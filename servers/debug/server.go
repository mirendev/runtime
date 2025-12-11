package debug

import (
	"context"
	"fmt"
	"log/slog"
	"net/netip"
	"strings"

	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/debug/debug_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/netdb"
	"miren.dev/runtime/pkg/rpc/standard"
)

type Server struct {
	Log   *slog.Logger
	NetDB *netdb.NetDB
	EAC   *entityserver_v1alpha.EntityAccessClient
}

var _ debug_v1alpha.IPAlloc = &Server{}

func NewServer(log *slog.Logger, ndb *netdb.NetDB, eac *entityserver_v1alpha.EntityAccessClient) *Server {
	return &Server{
		Log:   log.With("module", "debug"),
		NetDB: ndb,
		EAC:   eac,
	}
}

func (s *Server) ListLeases(ctx context.Context, state *debug_v1alpha.IPAllocListLeases) error {
	args := state.Args()
	results := state.Results()

	filter := netdb.LeaseFilter{
		ReservedOnly: args.ReservedOnly(),
		ReleasedOnly: args.ReleasedOnly(),
	}
	if args.HasSubnet() {
		filter.Subnet = args.Subnet()
	}

	leases, err := s.NetDB.ListLeases(filter)
	if err != nil {
		return err
	}

	rpcLeases := make([]*debug_v1alpha.IPLease, len(leases))
	for i, lease := range leases {
		rpcLease := &debug_v1alpha.IPLease{}
		rpcLease.SetIp(lease.IP)
		rpcLease.SetSubnet(lease.Subnet)
		rpcLease.SetReserved(lease.Reserved)
		if lease.ReleasedAt != nil {
			rpcLease.SetReleasedAt(standard.ToTimestamp(*lease.ReleasedAt))
		}
		rpcLeases[i] = rpcLease
	}

	results.SetLeases(rpcLeases)
	return nil
}

func (s *Server) Status(ctx context.Context, state *debug_v1alpha.IPAllocStatus) error {
	results := state.Results()

	statuses, err := s.NetDB.GetSubnetStatus()
	if err != nil {
		return err
	}

	rpcStatuses := make([]*debug_v1alpha.SubnetStatus, len(statuses))
	for i, status := range statuses {
		rpcStatus := &debug_v1alpha.SubnetStatus{}
		rpcStatus.SetSubnet(status.Subnet)
		rpcStatus.SetTotal(int32(status.Total))
		rpcStatus.SetReserved(int32(status.Reserved))
		rpcStatus.SetReleased(int32(status.Released))
		rpcStatus.SetCapacity(int32(status.Capacity))
		rpcStatuses[i] = rpcStatus
	}

	results.SetSubnets(rpcStatuses)
	return nil
}

func (s *Server) ReleaseIP(ctx context.Context, state *debug_v1alpha.IPAllocReleaseIP) error {
	args := state.Args()
	results := state.Results()

	released, err := s.NetDB.ForceReleaseIP(args.Ip())
	if err != nil {
		return err
	}

	results.SetReleased(released)
	return nil
}

func (s *Server) ReleaseSubnet(ctx context.Context, state *debug_v1alpha.IPAllocReleaseSubnet) error {
	args := state.Args()
	results := state.Results()

	count, err := s.NetDB.ForceReleaseSubnet(args.Subnet())
	if err != nil {
		return err
	}

	results.SetCount(int32(count))
	return nil
}

func (s *Server) ReleaseAll(ctx context.Context, state *debug_v1alpha.IPAllocReleaseAll) error {
	results := state.Results()

	count, err := s.NetDB.ForceReleaseAll()
	if err != nil {
		return err
	}

	results.SetCount(int32(count))
	return nil
}

func (s *Server) Gc(ctx context.Context, state *debug_v1alpha.IPAllocGc) error {
	args := state.Args()
	results := state.Results()

	// Get all sandbox IPs from entity store
	sandboxRef := entity.Ref(entity.EntityKind, compute_v1alpha.KindSandbox)
	sandboxes, err := s.EAC.List(ctx, sandboxRef)
	if err != nil {
		return fmt.Errorf("failed to list sandboxes: %w", err)
	}

	// Build set of IPs that are in use by sandboxes
	liveIPs := make(map[string]bool)
	for _, sbEnt := range sandboxes.Values() {
		var sb compute_v1alpha.Sandbox
		sb.Decode(sbEnt.Entity())

		for _, net := range sb.Network {
			addr := net.Address
			if strings.Contains(addr, "/") {
				if prefix, err := netip.ParsePrefix(addr); err == nil {
					addr = prefix.Addr().String()
				}
			}
			liveIPs[addr] = true
		}
	}

	// Query reserved IPs from netdb
	var subnet string
	if args.HasSubnet() {
		subnet = args.Subnet()
	}

	reservedIPs, err := s.NetDB.GetReservedIPs(subnet)
	if err != nil {
		return err
	}

	var orphanedIPs []string
	for _, ip := range reservedIPs {
		if !liveIPs[ip] {
			orphanedIPs = append(orphanedIPs, ip)
		}
	}

	results.SetOrphanedIps(orphanedIPs)

	if args.DryRun() || len(orphanedIPs) == 0 {
		results.SetReleasedCount(0)
		return nil
	}

	if err := s.NetDB.ForceReleaseIPs(orphanedIPs); err != nil {
		return err
	}

	results.SetReleasedCount(int32(len(orphanedIPs)))
	return nil
}
