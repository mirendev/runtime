package ipalloc

import (
	"context"
	"crypto/rand"
	"fmt"
	"log/slog"
	"math"
	"math/big"
	mrand "math/rand"
	"net/netip"
	"slices"
	"sync"

	"golang.org/x/crypto/blake2b"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/api/network/network_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/multierror"
	"miren.dev/runtime/pkg/rpc/stream"
)

type Allocator struct {
	mu sync.Mutex

	log         *slog.Logger
	subnets     []netip.Prefix
	allocations map[netip.Addr]string
}

func NewAllocator(log *slog.Logger, subnets []netip.Prefix) *Allocator {
	return &Allocator{
		log:         log,
		subnets:     slices.Clone(subnets),
		allocations: make(map[netip.Addr]string),
	}
}

func (a *Allocator) Refresh(ctx context.Context, eac *entityserver_v1alpha.EntityAccessClient) error {
	res, err := eac.List(ctx, entity.Ref(entity.EntityKind, network_v1alpha.KindService))
	if err != nil {
		return err
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	var rerr error

	for _, ent := range res.Values() {
		var serv network_v1alpha.Service
		serv.Decode(ent.Entity())

		for _, ip := range serv.Ip {
			addr, err := netip.ParseAddr(ip)
			if err != nil {
				rerr = multierror.Append(rerr, err)
			} else {
				a.allocations[addr] = ent.Id()
			}
		}
	}

	return nil
}

func (a *Allocator) Allocate(ctx context.Context, id entity.Id) ([]netip.Addr, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	var ret []netip.Addr

	// Try using an ip based on the hash of the id, which has the benefit of producing
	// mostly stable ips which is good for debugging.

	rr := newRandReader()

	for _, subnet := range a.subnets {
		cr := hashedRandReader(id)

	inner:
		for {
			ip, err := generateRandomIPInSubnet(cr, subnet)
			if err != nil {
				return nil, err
			}

			if _, ok := a.allocations[ip]; !ok {
				a.allocations[ip] = id.String()
				ret = append(ret, ip)
				break inner
			}

			// After the first iteration, we can use the random reader to generate
			// random ips.
			cr = rr
		}
	}

	return ret, nil
}

func newRandReader() *mrand.Rand {
	limit := new(big.Int).SetUint64(math.MaxInt64)

	n, err := rand.Int(rand.Reader, limit)
	if err != nil {
		panic(err)
	}

	return mrand.New(mrand.NewSource(n.Int64()))
}

func hashedRandReader(id entity.Id) *mrand.Rand {
	h := blake2b.Sum256([]byte(id))
	seed := new(big.Int).SetBytes(h[:7])
	return mrand.New(mrand.NewSource(seed.Int64()))
}

// generateRandomIPInSubnet generates a random IP address within the given subnet
func generateRandomIPInSubnet(r *mrand.Rand, prefix netip.Prefix) (netip.Addr, error) {
	// Get the start address of the subnet
	startAddr := prefix.Addr()

	// Calculate the number of addresses in the subnet
	bits := prefix.Bits()
	addrCount := new(big.Int).Lsh(big.NewInt(1), uint(startAddr.BitLen()-bits)) // For IPv4, it's 32-prefix_length
	addrCount.Sub(addrCount, big.NewInt(1))                                     // Subtract 1 to stay within range

	// Set up the random source with a time-based seed
	//source := mrand.NewSource(time.Now().UnixNano())
	//r := mrand.New(source)

	// Generate a random offset within the subnet
	randomOffset := new(big.Int).Rand(r, addrCount)

	// Apply the offset to the start address
	ipBytes := startAddr.AsSlice()
	offsetBytes := randomOffset.Bytes()

	// Ensure offsetBytes is properly padded to match ipBytes length
	paddedOffset := make([]byte, len(ipBytes))
	copy(paddedOffset[len(paddedOffset)-len(offsetBytes):], offsetBytes)

	// Apply the offset
	for i := 0; i < len(ipBytes); i++ {
		ipBytes[i] += paddedOffset[i]
	}

	// Convert back to a netip.Addr
	result, ok := netip.AddrFromSlice(ipBytes)
	if !ok {
		return netip.Addr{}, fmt.Errorf("failed to create IP address from bytes")
	}

	return result, nil
}

func (a *Allocator) Watch(ctx context.Context, eac *entityserver_v1alpha.EntityAccessClient) error {
	a.log.Info("watching for service changes")

	index := entity.Ref(entity.EntityKind, network_v1alpha.KindService)

	_, err := eac.WatchIndex(ctx, index, stream.Callback(func(op *entityserver_v1alpha.EntityOp) error {
		if op == nil {
			return nil
		}

		switch op.Operation() {
		case 1, 2:
			// fine
		default:
			return nil
		}

		err := a.assignService(ctx, op.Entity().Entity(), eac)
		if err != nil {
			a.log.Error("failed to assign sandbox", "error", err, "sandbox", op.Entity().Id())
		}

		return nil
	}))
	return err
}

type service struct {
	network_v1alpha.Service
	*entity.Entity
}

func (a *Allocator) assignService(ctx context.Context, ent *entity.Entity, eac *entityserver_v1alpha.EntityAccessClient) error {
	var srv service
	srv.Entity = ent
	srv.Decode(srv.Entity)

	if len(srv.Ip) > 0 {
		return nil
	}

	ips, err := a.Allocate(ctx, srv.Entity.ID)
	if err != nil {
		return err
	}

	for _, ip := range ips {
		srv.Ip = append(srv.Ip, ip.String())
	}

	var rpcE entityserver_v1alpha.Entity
	rpcE.SetId(string(srv.Entity.ID))
	rpcE.SetAttrs(srv.Encode())

	if _, err := eac.Put(ctx, &rpcE); err != nil {
		a.log.Error("failed to assign service ips", "error", err, "service", srv.Entity.ID)
		return err
	}

	return nil
}
