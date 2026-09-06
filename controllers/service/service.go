package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/mr-tron/base58"
	"golang.org/x/crypto/blake2b"
	"sigs.k8s.io/knftables"

	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/api/network/network_v1alpha"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/controller"
	"miren.dev/runtime/pkg/entity"
)

// tableName is the single nft table this controller owns.
const tableName = "miren"

// Static chain names. These belong to the controller's "infrastructure" — the
// dispatcher, the masq machinery, the base chains attached to NAT hooks. They
// are installed once by Init and never garbage-collected.
const (
	chainServices       = "services"
	chainNATPrerouting  = "nat-prerouting"
	chainNATOutput      = "nat-output"
	chainNATPostrouting = "nat-postrouting"
	chainMarkForMasq    = "mark-for-masq"
	chainMasq           = "masq"
)

// Static map names. Keys are concatenated tuples that the dispatcher chain
// uses to route packets to the right per-service or per-nodeport chain.
const (
	mapServiceIP4s     = "service_ip4s"
	mapServiceIP6s     = "service_ip6s"
	mapServiceNodePort = "service_nodeports"
)

// ServiceControllerDeps holds required dependencies for ServiceController.
type ServiceControllerDeps struct {
	Log             *slog.Logger
	EAC             *entityserver_v1alpha.EntityAccessClient
	IPv4Routable    netip.Prefix
	ServicePrefixes []netip.Prefix
	DisableLocalNet bool
}

type ServiceController struct {
	Log             *slog.Logger
	EAC             *entityserver_v1alpha.EntityAccessClient
	IPv4Routable    netip.Prefix
	ServicePrefixes []netip.Prefix

	DisableLocalNet bool

	routablePrefixes []netip.Prefix

	nft knftables.Interface

	mu sync.Mutex
	// chainEndpoints caches the last-installed endpoint chain list for each
	// service-IP and nodeport chain so we can skip the flush+rebuild when the
	// composition hasn't changed. Protected by mu.
	chainEndpoints map[string][]string
}

// NewServiceController creates a new ServiceController with validated dependencies.
func NewServiceController(cfg ServiceControllerDeps) (*ServiceController, error) {
	if cfg.Log == nil {
		return nil, fmt.Errorf("service: Log is required")
	}
	if cfg.EAC == nil {
		return nil, fmt.Errorf("service: entity access client is required")
	}
	if !cfg.IPv4Routable.IsValid() {
		return nil, fmt.Errorf("service: IPv4Routable must be a valid prefix")
	}

	nft, err := knftables.New(knftables.InetFamily, tableName)
	if err != nil {
		return nil, fmt.Errorf("service: nft init: %w", err)
	}

	return &ServiceController{
		Log:             cfg.Log,
		EAC:             cfg.EAC,
		IPv4Routable:    cfg.IPv4Routable,
		ServicePrefixes: cfg.ServicePrefixes,
		DisableLocalNet: cfg.DisableLocalNet,
		nft:             nft,
	}, nil
}

func (s *ServiceController) UpdateEndpoints(ctx context.Context, event controller.Event) ([]entity.Attr, error) {
	var eps network_v1alpha.Endpoints
	eps.Decode(event.Entity)

	gr, err := s.EAC.Get(ctx, eps.Service.String())
	if err != nil {
		if errors.Is(err, cond.ErrNotFound{}) {
			// The parent Service is gone too (cascading delete or the
			// launcher tearing the app down). Any stranded chains get
			// pruned by the next GC tick.
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get service: %w", err)
	}

	var srv network_v1alpha.Service
	srv.Decode(gr.Entity().Entity())

	s.Log.Info("Endpoint event, triggering service update",
		"service", srv.ID, "type", event.Type)

	return nil, s.Create(ctx, &srv, &entity.Meta{Entity: gr.Entity().Entity()})
}

// nftProto converts a network_v1alpha.PortProtocol to the nftables protocol string.
// Defaults to "tcp" for empty or unrecognized values.
func nftProto(p network_v1alpha.PortProtocol) string {
	if p == network_v1alpha.UDP {
		return "udp"
	}
	return "tcp"
}

func (s *ServiceController) serviceChain(ip netip.Addr, port uint16, proto string) string {
	x := blake2b.Sum256(fmt.Appendf(nil, "%s:%s:%d", ip.String(), proto, port))
	return fmt.Sprintf("service_%s", base58.Encode(x[:]))
}

func (s *ServiceController) endpointChain(ip netip.Addr, port uint16, proto string) string {
	x := blake2b.Sum256(fmt.Appendf(nil, "%s:%s:%d", ip.String(), proto, port))
	return fmt.Sprintf("endpoint_%s", base58.Encode(x[:]))
}

// nodeportChain names the per-NodePort chain. NodePort dispatch is a property
// of the cluster-facing port, so the key is (proto, nport) — independent of
// any service IP, which a service may not yet have when this chain installs.
func (s *ServiceController) nodeportChain(nport int, proto string) string {
	x := blake2b.Sum256(fmt.Appendf(nil, "%s:%d", proto, nport))
	return fmt.Sprintf("nodeport_%s", base58.Encode(x[:]))
}

// mapForServiceIP returns the verdict map name for the dispatcher's service-IP
// path. Picks v4 vs v6 from the address family.
func mapForServiceIP(ip netip.Addr) string {
	if ip.Is6() {
		return mapServiceIP6s
	}
	return mapServiceIP4s
}

func (s *ServiceController) Init(ctx context.Context) error {
	s.chainEndpoints = make(map[string][]string)
	s.routablePrefixes = append([]netip.Prefix{s.IPv4Routable}, s.ServicePrefixes...)

	s.Log.Info("Initializing service controller")

	if !s.DisableLocalNet {
		// Allow DNAT to deliver to localhost-bound endpoints. Best-effort; if
		// /proc isn't writable we just keep going.
		_ = os.WriteFile("/proc/sys/net/ipv4/conf/all/route_localnet", []byte("1"), 0644)
	}

	tx := s.nft.NewTransaction()

	// Idempotent table create.
	tx.Add(&knftables.Table{})

	// Verdict maps the dispatcher uses to route packets.
	tx.Add(&knftables.Map{
		Name: mapServiceIP4s,
		Type: "ipv4_addr . inet_proto . inet_service : verdict",
	})
	tx.Add(&knftables.Map{
		Name: mapServiceIP6s,
		Type: "ipv6_addr . inet_proto . inet_service : verdict",
	})
	tx.Add(&knftables.Map{
		Name: mapServiceNodePort,
		Type: "inet_proto . inet_service : verdict",
	})

	// Named counters that the per-service and per-nodeport chains reference
	// for traffic visibility. Created here so chain bodies can `counter name`
	// them without races.
	tx.Add(&knftables.Counter{Name: "services"})
	tx.Add(&knftables.Counter{Name: "nodeports"})

	// Static chains. We Add to ensure existence and Flush to wipe stale rules
	// from prior controller versions, then re-add the canonical rule set.
	// The duplicate `jump services` rules the bug report flagged came from old
	// code that re-added without flushing; this loop fixes that for free.
	tx.Add(&knftables.Chain{Name: chainServices})
	tx.Flush(&knftables.Chain{Name: chainServices})
	tx.Add(&knftables.Rule{Chain: chainServices, Rule: "ip daddr . meta l4proto . th dport vmap @" + mapServiceIP4s})
	tx.Add(&knftables.Rule{Chain: chainServices, Rule: "ip6 daddr . meta l4proto . th dport vmap @" + mapServiceIP6s})
	tx.Add(&knftables.Rule{Chain: chainServices, Rule: "meta l4proto . th dport vmap @" + mapServiceNodePort})

	tx.Add(&knftables.Chain{Name: chainMarkForMasq})
	tx.Flush(&knftables.Chain{Name: chainMarkForMasq})
	tx.Add(&knftables.Rule{Chain: chainMarkForMasq, Rule: "mark set mark or 0x2000"})

	tx.Add(&knftables.Chain{Name: chainMasq})
	tx.Flush(&knftables.Chain{Name: chainMasq})
	tx.Add(&knftables.Rule{Chain: chainMasq, Rule: "mark and 0x2000 == 0 return"})
	tx.Add(&knftables.Rule{Chain: chainMasq, Rule: "mark set mark xor 0x2000"})
	tx.Add(&knftables.Rule{Chain: chainMasq, Rule: "masquerade fully-random"})

	// Base chains attached to NAT hooks.
	tx.Add(&knftables.Chain{
		Name:     chainNATPrerouting,
		Type:     knftables.PtrTo(knftables.NATType),
		Hook:     knftables.PtrTo(knftables.PreroutingHook),
		Priority: knftables.PtrTo(knftables.BaseChainPriority("-100")),
	})
	tx.Flush(&knftables.Chain{Name: chainNATPrerouting})
	tx.Add(&knftables.Rule{Chain: chainNATPrerouting, Rule: "jump " + chainServices})

	tx.Add(&knftables.Chain{
		Name:     chainNATOutput,
		Type:     knftables.PtrTo(knftables.NATType),
		Hook:     knftables.PtrTo(knftables.OutputHook),
		Priority: knftables.PtrTo(knftables.BaseChainPriority("-100")),
	})
	tx.Flush(&knftables.Chain{Name: chainNATOutput})
	tx.Add(&knftables.Rule{Chain: chainNATOutput, Rule: "jump " + chainServices})

	tx.Add(&knftables.Chain{
		Name:     chainNATPostrouting,
		Type:     knftables.PtrTo(knftables.NATType),
		Hook:     knftables.PtrTo(knftables.PostroutingHook),
		Priority: knftables.PtrTo(knftables.BaseChainPriority("100")),
	})
	tx.Flush(&knftables.Chain{Name: chainNATPostrouting})
	tx.Add(&knftables.Rule{Chain: chainNATPostrouting, Rule: "jump " + chainMasq})

	if err := s.nft.Run(ctx, tx); err != nil {
		return fmt.Errorf("initialize nftables: %w", err)
	}
	return nil
}

// addEndpointChain registers a per-backend chain that DNATs to one sandbox.
// Endpoint chains have static contents (mark-for-masq for hairpin, then DNAT)
// and are content-addressed by (backend ip, target port, proto), so two
// services pointing at the same backend share the same chain.
//
// Returns the chain name. Always emits an idempotent Add+Flush+rules so
// concurrent Create() calls don't end up with stale rule contents from a prior
// controller version.
func (s *ServiceController) addEndpointChain(tx *knftables.Transaction, ip netip.Addr, port uint16, proto string) string {
	chain := s.endpointChain(ip, port, proto)
	tx.Add(&knftables.Chain{Name: chain})
	tx.Flush(&knftables.Chain{Name: chain})
	if ip.Is4() {
		tx.Add(&knftables.Rule{Chain: chain, Rule: knftables.Concat("ip saddr", ip, "jump", chainMarkForMasq)})
		tx.Add(&knftables.Rule{Chain: chain, Rule: knftables.Concat("meta l4proto", proto, "counter dnat ip to", ip, ":", port)})
	} else {
		tx.Add(&knftables.Rule{Chain: chain, Rule: knftables.Concat("ip6 saddr", ip, "jump", chainMarkForMasq)})
		tx.Add(&knftables.Rule{Chain: chain, Rule: knftables.Concat("meta l4proto", proto, "counter dnat ip6 to", ip, ":", port)})
	}
	return chain
}

// addServiceChain registers a per-(serviceIP, port, proto) chain plus the map
// element that points the dispatcher at it. Body is filled in by setEndpoints.
func (s *ServiceController) addServiceChain(tx *knftables.Transaction, ip netip.Addr, port int, proto string) {
	chain := s.serviceChain(ip, uint16(port), proto)
	tx.Add(&knftables.Chain{Name: chain})
	tx.Add(&knftables.Element{
		Map:   mapForServiceIP(ip),
		Key:   []string{ip.String(), proto, strconv.Itoa(port)},
		Value: []string{"goto " + chain},
	})
}

// setEndpoints fills in the body of a service-IP or nodeport chain. Skips the
// flush+rebuild when the endpoint set hasn't changed from the cache to avoid
// resetting the named counter on each event-driven reconcile. For the
// unconditional-rebuild path used by Periodic, see writeChainBody.
func (s *ServiceController) setEndpoints(tx *knftables.Transaction, chain, counterName string, endpoints []string) {
	sorted := append([]string(nil), endpoints...)
	slices.Sort(sorted)

	s.mu.Lock()
	cur, ok := s.chainEndpoints[chain]
	if ok && slices.Equal(cur, sorted) {
		s.mu.Unlock()
		return
	}
	s.chainEndpoints[chain] = sorted
	s.mu.Unlock()

	s.writeChainBody(tx, chain, counterName, sorted)
}

// invalidateChainCache drops every cached chain body. The cache is only an
// optimization -- it exists to skip a flush+rebuild when nothing changed -- so
// dropping it costs one extra rebuild and restores the invariant that a cache
// entry means nft accepted that body.
func (s *ServiceController) invalidateChainCache() {
	s.mu.Lock()
	clear(s.chainEndpoints)
	s.mu.Unlock()
}

// writeChainBody flushes a service-IP or nodeport chain and re-emits its full
// rule set: counter, per-prefix mark-for-masq jumps, and a numgen-random vmap
// across endpoint chains. When endpoints is empty the chain is rebuilt with
// just `counter + drop` so traffic to a service with no backends is dropped
// rather than DNAT'd to a stale address. Bypasses the chainEndpoints cache;
// callers that want the cached fast path should go through setEndpoints.
func (s *ServiceController) writeChainBody(tx *knftables.Transaction, chain, counterName string, endpoints []string) {
	// Declare the counter in the same transaction that references it. Init
	// declares it too, but a chain body can be written against kernel state
	// where that never took effect -- Init runs once at startup, while anything
	// that flushes the ruleset underneath us drops the counter and leaves the
	// table to be rebuilt by these paths. nft answers a rule naming an absent
	// counter with ENOENT and rolls back the whole batch, so the chain keeps
	// whatever it had. `add` is idempotent and does not reset an existing
	// counter's totals, so declaring it here is free.
	tx.Add(&knftables.Counter{Name: counterName})

	tx.Flush(&knftables.Chain{Name: chain})
	tx.Add(&knftables.Rule{Chain: chain, Rule: knftables.Concat("counter name", `"`+counterName+`"`)})

	if len(endpoints) == 0 {
		// `drop` rather than `reject` because the calling path includes
		// nat-prerouting, where `reject` isn't permitted. Result is a connect
		// timeout instead of ECONNREFUSED, but still preferable to forwarding
		// traffic to a stale endpoint address.
		tx.Add(&knftables.Rule{Chain: chain, Rule: "drop"})
		return
	}

	for _, rp := range s.routablePrefixes {
		if rp.Addr().Is4() {
			tx.Add(&knftables.Rule{Chain: chain, Rule: knftables.Concat("ip saddr !=", rp, "counter jump", chainMarkForMasq)})
		} else {
			tx.Add(&knftables.Rule{Chain: chain, Rule: knftables.Concat("ip6 saddr !=", rp, "counter jump", chainMarkForMasq)})
		}
	}

	vmap := make([]string, len(endpoints))
	for i, ep := range endpoints {
		vmap[i] = fmt.Sprintf("%d : goto %s", i, ep)
	}
	tx.Add(&knftables.Rule{
		Chain: chain,
		Rule:  fmt.Sprintf("numgen random mod %d vmap { %s }", len(endpoints), strings.Join(vmap, ", ")),
	})
}

// addNodePort registers a per-NodePort chain that DNATs traffic landing on
// this node's NodePort to one of the service's endpoint sandbox IPs, picked
// at random. Mirrors addServiceChain + setEndpoints but stands alone so that
// NodePort works on every node regardless of whether the service has an
// allocated cluster IP yet. When endpoints is empty, setEndpoints writes a
// `drop` body so the NodePort gets the same fail-shut treatment as cluster IPs.
func (s *ServiceController) addNodePort(tx *knftables.Transaction, nport int, proto string, endpoints []string) {
	chain := s.nodeportChain(nport, proto)
	tx.Add(&knftables.Chain{Name: chain})
	tx.Add(&knftables.Element{
		Map:   mapServiceNodePort,
		Key:   []string{proto, strconv.Itoa(nport)},
		Value: []string{"goto " + chain},
	})
	s.setEndpoints(tx, chain, "nodeports", endpoints)
}

func (s *ServiceController) Create(ctx context.Context, srv *network_v1alpha.Service, meta *entity.Meta) error {
	// Named fields rather than the whole struct: %+v on a Service renders its
	// address list, match rules and full port table inline, which came to ~300
	// bytes a line for something that is mostly reporting an identifier.
	s.Log.Info("Creating service",
		"service", srv.ID,
		"addrs", len(srv.Ip),
		"ports", len(srv.Port))

	lr, err := s.EAC.List(ctx, entity.Ref(network_v1alpha.EndpointsServiceId, srv.ID))
	if err != nil {
		return fmt.Errorf("failed to list endpoints: %w", err)
	}

	tx := s.nft.NewTransaction()

	// Build endpoint chains once, keyed by the public-facing port. Both the
	// service-IP path and the NodePort path consume the same set so that a
	// service with a NodePort but no allocated cluster IP still gets DNAT
	// installed on every node.
	type portKey struct {
		Port  int64
		Proto string
	}
	epChainsByPort := make(map[portKey][]string, len(srv.Port))
	for _, tp := range srv.Port {
		target := tp.TargetPort
		if target == 0 {
			target = tp.Port
		}
		proto := nftProto(tp.Protocol)
		key := portKey{Port: tp.Port, Proto: proto}

		for _, ent := range lr.Values() {
			var eps network_v1alpha.Endpoints
			eps.Decode(ent.Entity())

			for _, ep := range eps.Endpoint {
				destIP, err := netip.ParseAddr(ep.Ip)
				if err != nil {
					return fmt.Errorf("failed to parse endpoint IP address: %v", err)
				}
				chain := s.addEndpointChain(tx, destIP, uint16(target), proto)
				epChainsByPort[key] = append(epChainsByPort[key], chain)
			}
		}
	}

	// Service-IP path: for cluster-internal traffic to <serviceIP>:<port>.
	// Skipped when the service has no allocated IP.
	for _, sip := range srv.Ip {
		ip, err := netip.ParseAddr(sip)
		if err != nil {
			return fmt.Errorf("failed to parse service IP address: %w", err)
		}
		for _, tp := range srv.Port {
			proto := nftProto(tp.Protocol)
			key := portKey{Port: tp.Port, Proto: proto}

			s.addServiceChain(tx, ip, int(tp.Port), proto)
			s.setEndpoints(tx, s.serviceChain(ip, uint16(tp.Port), proto), "services", epChainsByPort[key])
		}
	}

	// NodePort path: every node that runs the controller installs this chain,
	// independent of where the service's sandboxes live, so external traffic
	// to <any-node>:<nport> reaches the service.
	for _, tp := range srv.Port {
		if tp.NodePort == 0 {
			continue
		}
		proto := nftProto(tp.Protocol)
		key := portKey{Port: tp.Port, Proto: proto}
		s.addNodePort(tx, int(tp.NodePort), proto, epChainsByPort[key])
	}

	if tx.NumOperations() == 0 {
		return nil
	}
	if err := s.nft.Run(ctx, tx); err != nil {
		// setEndpoints records what it wrote before the batch is applied, so a
		// failed apply leaves the cache claiming rules that nft never took.
		// The next pass would then skip the rebuild and the chain would stay
		// as it is -- created by the idempotent addServiceChain, but empty, so
		// the map sends traffic into a chain that matches nothing. Drop the
		// cache so the next pass rebuilds from scratch.
		s.invalidateChainCache()
		return fmt.Errorf("apply nftables changes: %w", err)
	}
	return nil
}

func (s *ServiceController) Delete(ctx context.Context, id entity.Id, obj *network_v1alpha.Service) error {
	// No-op. Cleanup happens in the periodic GC pass which diffs the entity
	// store against the kernel and prunes anything no longer claimed.
	//
	// Per-service deletion is awkward to do correctly here anyway. Endpoint
	// chains are content-addressed by (backendIP, targetPort, proto), so two
	// services pointing at the same backend share a chain; tearing it down
	// because one of them went away would break the other. The GC pass
	// computes the union of expected chains across all live services in one
	// shot and only prunes what's actually orphan, which dodges that whole
	// class of mistake. As a bonus it also sweeps chains leaked by older
	// controller versions that no current entity claims.
	return nil
}
