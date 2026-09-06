package service

import (
	"context"
	"fmt"
	"net/netip"
	"slices"
	"strconv"
	"strings"

	"sigs.k8s.io/knftables"

	"miren.dev/runtime/api/network/network_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/set"
)

// chainKind classifies a chain by ownership. The GC pass treats each kind
// differently: static chains are never deleted, content-addressed chains are
// candidates for cleanup if no Service entity claims them, and unknown chains
// are left alone (they belong to someone outside the controller's domain).
type chainKind int

const (
	chainKindUnknown chainKind = iota
	chainKindStatic
	chainKindService
	chainKindEndpoint
	chainKindNodePort
)

// classifyChain returns the kind of a chain by name. Chain names are
// content-addressed prefixes the controller writes itself, so prefix-matching
// is sufficient and stable.
func classifyChain(name string) chainKind {
	switch {
	case strings.HasPrefix(name, "service_"):
		return chainKindService
	case strings.HasPrefix(name, "endpoint_"):
		return chainKindEndpoint
	case strings.HasPrefix(name, "nodeport_"):
		return chainKindNodePort
	case isStaticChain(name):
		return chainKindStatic
	default:
		return chainKindUnknown
	}
}

func isStaticChain(name string) bool {
	switch name {
	case chainServices, chainNATPrerouting, chainNATOutput,
		chainNATPostrouting, chainMarkForMasq, chainMasq:
		return true
	}
	return false
}

// managedMaps lists the verdict maps the controller writes elements into.
// Elements outside these maps are not the controller's concern.
var managedMaps = []string{mapServiceIP4s, mapServiceIP6s, mapServiceNodePort}

// targetState captures what the kernel should look like, derived from the
// current Service and Endpoints entities. Chain names plus expected map
// element keys (canonicalized).
//
// serviceSpecs, nodePortSpecs, and endpointSpecs carry enough info for the
// Periodic reconcile pass to rebuild every live chain's body unconditionally.
// Knowing the chain name alone (in `chains`) is sufficient for orphan detection
// but not enough to recompute the chain's rules.
type targetState struct {
	chains   set.Set[string]
	elements map[string]set.Set[string] // mapName -> canonicalKey set

	serviceSpecs  []serviceChainSpec
	nodePortSpecs []nodePortChainSpec
	endpointSpecs map[string]endpointChainSpec // chain name -> spec, deduplicated
}

type serviceChainSpec struct {
	ip        netip.Addr
	port      uint16
	proto     string
	endpoints []string // endpoint chain names, sorted
}

type nodePortChainSpec struct {
	nport     int
	proto     string
	endpoints []string // endpoint chain names, sorted
}

type endpointChainSpec struct {
	ip    netip.Addr
	port  uint16
	proto string
}

// kernelState captures what the kernel actually has, observed via knftables.
// elements stores the original Element so we can re-emit a Delete with the
// correct Key (and Value, in case knftables ever requires it for delete).
type kernelState struct {
	chains   map[string]chainKind
	elements map[string]map[string]*knftables.Element // mapName -> canonicalKey -> Element
}

// canonicalKey turns a multi-part element key into a deterministic string for
// set membership checks. The null separator can't appear in any IP, proto, or
// port representation we use.
func canonicalKey(parts []string) string {
	return strings.Join(parts, "\x00")
}

// Periodic runs cluster-level GC: lists all live Service and Endpoints
// entities, snapshots the kernel state, and prunes anything in the kernel
// that no current entity claims. Designed to run on the controller's
// periodic tick (registered via SetPeriodic in runner.go).
func (s *ServiceController) Periodic(ctx context.Context) error {
	target, err := s.computeTargetState(ctx)
	if err != nil {
		return fmt.Errorf("compute target state: %w", err)
	}

	actual, err := s.snapshotKernelState(ctx)
	if err != nil {
		return fmt.Errorf("snapshot kernel state: %w", err)
	}

	return s.applyGC(ctx, target, actual)
}

func (s *ServiceController) computeTargetState(ctx context.Context) (*targetState, error) {
	target := &targetState{
		chains:        set.New[string](),
		elements:      make(map[string]set.Set[string]),
		endpointSpecs: make(map[string]endpointChainSpec),
	}
	for _, m := range managedMaps {
		target.elements[m] = set.New[string]()
	}

	services, err := s.EAC.List(ctx, entity.Ref(entity.EntityKind, network_v1alpha.KindService))
	if err != nil {
		return nil, fmt.Errorf("list services: %w", err)
	}

	// One global Endpoints list grouped by service ID, instead of a list
	// call per service. Cheaper as the cluster grows.
	endpointsByService := make(map[entity.Id][]network_v1alpha.Endpoints)
	eps, err := s.EAC.List(ctx, entity.Ref(entity.EntityKind, network_v1alpha.KindEndpoints))
	if err != nil {
		return nil, fmt.Errorf("list endpoints: %w", err)
	}
	for _, e := range eps.Values() {
		var ep network_v1alpha.Endpoints
		ep.Decode(e.Entity())
		endpointsByService[ep.Service] = append(endpointsByService[ep.Service], ep)
	}

	for _, e := range services.Values() {
		var srv network_v1alpha.Service
		srv.Decode(e.Entity())

		for _, tp := range srv.Port {
			proto := nftProto(tp.Protocol)
			targetPort := tp.TargetPort
			if targetPort == 0 {
				targetPort = tp.Port
			}

			// Resolve endpoint chain names once per (port, proto). Service-IP
			// and NodePort dispatchers share the same backend set, so each
			// (ip, port, proto) tuple maps to a single endpoint chain by hash.
			var epChains []string
			for _, ep := range endpointsByService[srv.ID] {
				for _, e := range ep.Endpoint {
					ip, err := netip.ParseAddr(e.Ip)
					if err != nil {
						return nil, fmt.Errorf("parse endpoint IP %q: %w", e.Ip, err)
					}
					epChain := s.endpointChain(ip, uint16(targetPort), proto)
					target.chains.Add(epChain)
					epChains = append(epChains, epChain)
					target.endpointSpecs[epChain] = endpointChainSpec{
						ip: ip, port: uint16(targetPort), proto: proto,
					}
				}
			}
			slices.Sort(epChains)
			epChains = slices.Compact(epChains)

			for _, sip := range srv.Ip {
				ip, err := netip.ParseAddr(sip)
				if err != nil {
					return nil, fmt.Errorf("parse service IP %q: %w", sip, err)
				}
				target.chains.Add(s.serviceChain(ip, uint16(tp.Port), proto))
				target.elements[mapForServiceIP(ip)].Add(canonicalKey(
					[]string{ip.String(), proto, strconv.FormatInt(tp.Port, 10)},
				))
				target.serviceSpecs = append(target.serviceSpecs, serviceChainSpec{
					ip: ip, port: uint16(tp.Port), proto: proto,
					endpoints: epChains,
				})
			}

			if tp.NodePort != 0 {
				target.chains.Add(s.nodeportChain(int(tp.NodePort), proto))
				target.elements[mapServiceNodePort].Add(canonicalKey(
					[]string{proto, strconv.FormatInt(tp.NodePort, 10)},
				))
				target.nodePortSpecs = append(target.nodePortSpecs, nodePortChainSpec{
					nport: int(tp.NodePort), proto: proto,
					endpoints: epChains,
				})
			}
		}
	}

	return target, nil
}

func (s *ServiceController) snapshotKernelState(ctx context.Context) (*kernelState, error) {
	state := &kernelState{
		chains:   make(map[string]chainKind),
		elements: make(map[string]map[string]*knftables.Element),
	}

	chains, err := s.nft.List(ctx, "chains")
	if err != nil {
		return nil, fmt.Errorf("list chains: %w", err)
	}
	for _, c := range chains {
		state.chains[c] = classifyChain(c)
	}

	for _, m := range managedMaps {
		state.elements[m] = make(map[string]*knftables.Element)
		els, err := s.nft.ListElements(ctx, "map", m)
		if err != nil {
			// A map missing from the kernel is fine — we just have no
			// elements to consider. Other errors should bubble up.
			if knftables.IsNotFound(err) {
				continue
			}
			return nil, fmt.Errorf("list elements of %s: %w", m, err)
		}
		for _, el := range els {
			state.elements[m][canonicalKey(el.Key)] = el
		}
	}

	return state, nil
}

func (s *ServiceController) applyGC(ctx context.Context, target *targetState, actual *kernelState) error {
	tx := s.nft.NewTransaction()

	// Drop stale dispatcher map elements first so that the reconcile-add path
	// below doesn't trip "File exists" when a live key needs a fresh value
	// (e.g. across a chain-hash-function bump: same proto+port, but the goto
	// target chain name shifted).
	staleElements := 0
	for mapName, expected := range target.elements {
		for canonKey, el := range actual.elements[mapName] {
			// Element is stale if the entity store doesn't claim it, or
			// if its goto target chain is gone (which can happen when
			// a service was rewritten with new chain hashes).
			stale := !expected.Contains(canonKey) || !target.chains.Contains(gotoTarget(el))
			if !stale {
				continue
			}
			tx.Delete(&knftables.Element{
				Map:   mapName,
				Key:   el.Key,
				Value: el.Value,
			})
			staleElements++
		}
	}

	// Reconcile live chain bodies. Periodic runs even when no entity changed,
	// so this is the self-heal path: if a live parent chain ended up with a
	// stale vmap (UpdateEndpoints event missed, setEndpoints called with an
	// empty list pre-fix, pre-#795 stacked rules, etc.), flush+rebuild here
	// corrects it, and the orphan endpoint chains it was pinning become
	// deletable in the same tx.
	s.reconcileLiveChains(tx, target)

	// Orphan chains in three phases. nft processes batch deletes sequentially
	// even inside an atomic transaction, so we have to delete in reverse-
	// dependency order to avoid EBUSY on chains that still have referrers:
	//   nodeport_* may contain `goto service_*` (pre-#795 shape) → delete first
	//   service_*  contains `goto endpoint_*` via vmap                → next
	//   endpoint_* are leaves                                          → last
	var orphanNodePorts, orphanServices, orphanLeaves []string
	for chain, kind := range actual.chains {
		switch kind {
		case chainKindNodePort:
			if !target.chains.Contains(chain) {
				orphanNodePorts = append(orphanNodePorts, chain)
			}
		case chainKindService:
			if !target.chains.Contains(chain) {
				orphanServices = append(orphanServices, chain)
			}
		case chainKindEndpoint:
			if !target.chains.Contains(chain) {
				orphanLeaves = append(orphanLeaves, chain)
			}
		case chainKindStatic, chainKindUnknown:
			// Leave alone.
		}
	}
	orphanParentCount := len(orphanNodePorts) + len(orphanServices)

	for _, chain := range orphanNodePorts {
		tx.Delete(&knftables.Chain{Name: chain})
	}
	for _, chain := range orphanServices {
		tx.Delete(&knftables.Chain{Name: chain})
	}
	for _, chain := range orphanLeaves {
		tx.Delete(&knftables.Chain{Name: chain})
	}

	if tx.NumOperations() == 0 {
		return nil
	}

	s.Log.Info("GC pass reconciling nft state",
		"reconciled_chains", len(target.serviceSpecs)+len(target.nodePortSpecs)+len(target.endpointSpecs),
		"stale_elements", staleElements,
		"orphan_chains", orphanParentCount+len(orphanLeaves),
	)

	if err := s.nft.Run(ctx, tx); err != nil {
		// Same reasoning as Create: the cache must not outlive a batch nft
		// rejected, or the next pass skips the rebuild it needs.
		s.invalidateChainCache()
		return fmt.Errorf("apply GC batch: %w", err)
	}

	deleted := append(append(orphanNodePorts, orphanServices...), orphanLeaves...)
	s.syncCachesAfterGC(target, deleted)
	return nil
}

// reconcileLiveChains emits Add+Flush+rules for every endpoint chain, service
// chain, and nodeport chain that the entity store currently claims. It does
// not consult the chainEndpoints cache; that's the whole point — Periodic
// runs as a backstop, so whatever the kernel currently looks like, this
// rebuilds it to match the entity store.
func (s *ServiceController) reconcileLiveChains(tx *knftables.Transaction, target *targetState) {
	for _, ep := range target.endpointSpecs {
		s.addEndpointChain(tx, ep.ip, ep.port, ep.proto)
	}
	for _, sp := range target.serviceSpecs {
		s.addServiceChain(tx, sp.ip, int(sp.port), sp.proto)
		s.writeChainBody(tx, s.serviceChain(sp.ip, sp.port, sp.proto), "services", sp.endpoints)
	}
	for _, sp := range target.nodePortSpecs {
		chain := s.nodeportChain(sp.nport, sp.proto)
		tx.Add(&knftables.Chain{Name: chain})
		tx.Add(&knftables.Element{
			Map:   mapServiceNodePort,
			Key:   []string{sp.proto, strconv.Itoa(sp.nport)},
			Value: []string{"goto " + chain},
		})
		s.writeChainBody(tx, chain, "nodeports", sp.endpoints)
	}
}

// gotoTarget extracts the target chain from a verdict-map element's value.
// knftables represents verdicts as a single-element string like "goto X".
// Any other shape is treated as "no target" (which makes the GC conservative
// — won't classify the element as orphan because of a missing target).
func gotoTarget(el *knftables.Element) string {
	const prefix = "goto "
	if len(el.Value) != 1 || !strings.HasPrefix(el.Value[0], prefix) {
		return ""
	}
	return el.Value[0][len(prefix):]
}

// syncCachesAfterGC keeps the chainEndpoints cache aligned with what we just
// wrote to the kernel. Reconciled chains get their current endpoint set
// recorded (so the next event-driven setEndpoints can no-op when truly
// unchanged); deleted chains are dropped from the cache (so a future Create
// doesn't skip the rebuild thinking nothing changed).
func (s *ServiceController) syncCachesAfterGC(target *targetState, deletedChains []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, sp := range target.serviceSpecs {
		chain := s.serviceChain(sp.ip, sp.port, sp.proto)
		s.chainEndpoints[chain] = append([]string(nil), sp.endpoints...)
	}
	for _, sp := range target.nodePortSpecs {
		chain := s.nodeportChain(sp.nport, sp.proto)
		s.chainEndpoints[chain] = append([]string(nil), sp.endpoints...)
	}
	for _, c := range deletedChains {
		delete(s.chainEndpoints, c)
	}
}
