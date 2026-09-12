package service

import (
	"context"
	"net/netip"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"sigs.k8s.io/knftables"

	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/api/network/network_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/types"
	"miren.dev/runtime/pkg/idgen"
	"miren.dev/runtime/pkg/set"
	"miren.dev/runtime/pkg/testutils"
)

func nftChains(t *testing.T, ctx context.Context, sc *ServiceController) set.Set[string] {
	t.Helper()
	chains, err := sc.nft.List(ctx, "chains")
	require.NoError(t, err)
	out := set.New[string]()
	for _, c := range chains {
		out.Add(c)
	}
	return out
}

func nftRuleCount(t *testing.T, ctx context.Context, sc *ServiceController, chain string) int {
	t.Helper()
	rules, err := sc.nft.ListRules(ctx, chain)
	require.NoError(t, err)
	return len(rules)
}

func putEndpoints(t *testing.T, ctx context.Context, eac *entityserver_v1alpha.EntityAccessClient, eps *network_v1alpha.Endpoints) {
	t.Helper()
	var rpcE entityserver_v1alpha.Entity
	rpcE.SetAttrs(entity.New(
		entity.Keyword(entity.Ident, eps.ID.String()),
		eps.Encode).Attrs())
	_, err := eac.Put(ctx, &rpcE)
	require.NoError(t, err)
}

func nftMapElementKeys(t *testing.T, ctx context.Context, sc *ServiceController, mapName string) set.Set[string] {
	t.Helper()
	els, err := sc.nft.ListElements(ctx, "map", mapName)
	if err != nil && knftables.IsNotFound(err) {
		return set.New[string]()
	}
	require.NoError(t, err)
	out := set.New[string]()
	for _, el := range els {
		out.Add(canonicalKey(el.Key))
	}
	return out
}

func putService(t *testing.T, ctx context.Context, eac *entityserver_v1alpha.EntityAccessClient, svc *network_v1alpha.Service) {
	t.Helper()
	var rpcE entityserver_v1alpha.Entity
	rpcE.SetAttrs(entity.New(
		entity.Keyword(entity.Ident, svc.ID.String()),
		svc.Encode).Attrs())
	_, err := eac.Put(ctx, &rpcE)
	require.NoError(t, err)
}

func TestServicePeriodic(t *testing.T) {
	svcName := func() string { return idgen.GenNS("svc") }

	t.Run("prunes orphan service chain and map element after entity deletion", func(t *testing.T) {
		r := require.New(t)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		testDeps, cleanup := testutils.NewTestDeps()
		defer cleanup()

		sc, err := newServiceController(testDeps)
		r.NoError(err)
		r.NoError(sc.Init(ctx))

		svcID := entity.Id(svcName())
		// Use a unique-per-test IP so chain hashes don't collide with state
		// from earlier tests in the same iso environment.
		svcIP := "10.99.13.7"
		svc := &network_v1alpha.Service{
			ID:    svcID,
			Ip:    []string{svcIP},
			Match: types.Labels{types.Label{Key: "app", Value: "gc-test"}},
			Port:  []network_v1alpha.Port{{Name: "http", Port: 80}},
		}
		putService(t, ctx, testDeps.EAC, svc)

		err = sc.Create(ctx, svc, &entity.Meta{Entity: entity.New(svc.Encode), Revision: 1})
		r.NoError(err)

		ip := netip.MustParseAddr(svcIP)
		chainName := sc.serviceChain(ip, 80, "tcp")
		elemKey := canonicalKey([]string{svcIP, "tcp", "80"})

		r.True(nftChains(t, ctx, sc).Contains(chainName), "service chain should exist after Create")
		r.True(nftMapElementKeys(t, ctx, sc, mapServiceIP4s).Contains(elemKey), "service_ip4s element should exist")

		_, err = testDeps.EAC.Delete(ctx, svcID.String())
		r.NoError(err)

		r.NoError(sc.Periodic(ctx))

		r.False(nftChains(t, ctx, sc).Contains(chainName), "orphan service chain should be pruned")
		r.False(nftMapElementKeys(t, ctx, sc, mapServiceIP4s).Contains(elemKey), "orphan map element should be pruned")
	})

	t.Run("prunes orphan nodeport chain and map element", func(t *testing.T) {
		r := require.New(t)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		testDeps, cleanup := testutils.NewTestDeps()
		defer cleanup()

		sc, err := newServiceController(testDeps)
		r.NoError(err)
		r.NoError(sc.Init(ctx))

		svcID := entity.Id(svcName())
		// Pick a NodePort unlikely to collide with anything else in the iso env.
		const nport = 31337
		svc := &network_v1alpha.Service{
			ID:    svcID,
			Match: types.Labels{types.Label{Key: "app", Value: "gc-test"}},
			Port:  []network_v1alpha.Port{{Name: "tcp", Port: 7777, NodePort: nport}},
		}
		putService(t, ctx, testDeps.EAC, svc)

		// One endpoint so addNodePort actually emits the chain (it skips on
		// empty endpoints).
		eps := &network_v1alpha.Endpoints{
			ID:       entity.Id("endpoints-" + svcID.String()),
			Service:  svcID,
			Endpoint: []network_v1alpha.Endpoint{{Ip: "10.8.99.5", Port: 7777}},
		}
		var epRPC entityserver_v1alpha.Entity
		epRPC.SetAttrs(entity.New(
			entity.Keyword(entity.Ident, eps.ID.String()),
			eps.Encode).Attrs())
		_, err = testDeps.EAC.Put(ctx, &epRPC)
		r.NoError(err)

		err = sc.Create(ctx, svc, &entity.Meta{Entity: entity.New(svc.Encode), Revision: 1})
		r.NoError(err)

		npChain := sc.nodeportChain(nport, "tcp")
		elemKey := canonicalKey([]string{"tcp", "31337"})

		r.True(nftChains(t, ctx, sc).Contains(npChain), "nodeport chain should exist after Create")
		r.True(nftMapElementKeys(t, ctx, sc, mapServiceNodePort).Contains(elemKey), "service_nodeports element should exist")

		_, err = testDeps.EAC.Delete(ctx, svcID.String())
		r.NoError(err)
		_, err = testDeps.EAC.Delete(ctx, eps.ID.String())
		r.NoError(err)

		r.NoError(sc.Periodic(ctx))

		r.False(nftChains(t, ctx, sc).Contains(npChain), "orphan nodeport chain should be pruned")
		r.False(nftMapElementKeys(t, ctx, sc, mapServiceNodePort).Contains(elemKey), "orphan nodeport map element should be pruned")
	})

	t.Run("preserves endpoint chain shared by another service", func(t *testing.T) {
		r := require.New(t)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		testDeps, cleanup := testutils.NewTestDeps()
		defer cleanup()

		sc, err := newServiceController(testDeps)
		r.NoError(err)
		r.NoError(sc.Init(ctx))

		// Two services both routing to the same backend (sandboxIP, port).
		// They will produce the same endpoint chain hash.
		const sandboxIP = "10.8.42.5"
		const targetPort = 8080

		svcAID := entity.Id(svcName())
		svcA := &network_v1alpha.Service{
			ID:   svcAID,
			Ip:   []string{"10.99.42.1"},
			Port: []network_v1alpha.Port{{Name: "front", Port: 80, TargetPort: targetPort}},
		}
		putService(t, ctx, testDeps.EAC, svcA)

		svcBID := entity.Id(svcName())
		svcB := &network_v1alpha.Service{
			ID:   svcBID,
			Ip:   []string{"10.99.42.2"},
			Port: []network_v1alpha.Port{{Name: "internal", Port: targetPort, TargetPort: targetPort}},
		}
		putService(t, ctx, testDeps.EAC, svcB)

		for _, sid := range []entity.Id{svcAID, svcBID} {
			eps := &network_v1alpha.Endpoints{
				ID:       entity.Id("endpoints-" + sid.String()),
				Service:  sid,
				Endpoint: []network_v1alpha.Endpoint{{Ip: sandboxIP, Port: targetPort}},
			}
			var epRPC entityserver_v1alpha.Entity
			epRPC.SetAttrs(entity.New(
				entity.Keyword(entity.Ident, eps.ID.String()),
				eps.Encode).Attrs())
			_, err = testDeps.EAC.Put(ctx, &epRPC)
			r.NoError(err)
		}

		r.NoError(sc.Create(ctx, svcA, &entity.Meta{Entity: entity.New(svcA.Encode), Revision: 1}))
		r.NoError(sc.Create(ctx, svcB, &entity.Meta{Entity: entity.New(svcB.Encode), Revision: 1}))

		epChain := sc.endpointChain(netip.MustParseAddr(sandboxIP), targetPort, "tcp")
		r.True(nftChains(t, ctx, sc).Contains(epChain), "shared endpoint chain should exist after both Creates")

		// Delete svcA only. svcB still references the same endpoint chain.
		_, err = testDeps.EAC.Delete(ctx, svcAID.String())
		r.NoError(err)
		_, err = testDeps.EAC.Delete(ctx, ("endpoints-" + svcAID.String()))
		r.NoError(err)

		r.NoError(sc.Periodic(ctx))

		r.True(nftChains(t, ctx, sc).Contains(epChain), "endpoint chain still in use by svcB should not be pruned")
	})

	t.Run("rebuilds parent chain body to drop traffic when endpoints drop to empty", func(t *testing.T) {
		r := require.New(t)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		testDeps, cleanup := testutils.NewTestDeps()
		defer cleanup()

		sc, err := newServiceController(testDeps)
		r.NoError(err)
		r.NoError(sc.Init(ctx))

		const sandboxIP = "10.8.123.7"
		const svcIP = "10.99.123.1"
		const port = 6669

		svcID := entity.Id(svcName())
		svc := &network_v1alpha.Service{
			ID:   svcID,
			Ip:   []string{svcIP},
			Port: []network_v1alpha.Port{{Name: "irc", Port: port}},
		}
		putService(t, ctx, testDeps.EAC, svc)

		epsID := entity.Id("endpoints-" + svcID.String())
		eps := &network_v1alpha.Endpoints{
			ID:       epsID,
			Service:  svcID,
			Endpoint: []network_v1alpha.Endpoint{{Ip: sandboxIP, Port: port}},
		}
		putEndpoints(t, ctx, testDeps.EAC, eps)

		r.NoError(sc.Create(ctx, svc, &entity.Meta{Entity: entity.New(svc.Encode), Revision: 1}))

		parentChain := sc.serviceChain(netip.MustParseAddr(svcIP), port, "tcp")
		epChain := sc.endpointChain(netip.MustParseAddr(sandboxIP), port, "tcp")

		r.True(nftChains(t, ctx, sc).Contains(parentChain))
		r.True(nftChains(t, ctx, sc).Contains(epChain))

		// Drop the Endpoints entity. Pre-fix, setEndpoints({}) would early-return
		// here on the next event, leaving the parent's vmap pointing at the
		// now-orphan endpoint chain and the GC pass aborting with EBUSY every
		// 5 minutes. We never fire that event in the test — we go straight to
		// Periodic, which exercises the self-heal path.
		_, err = testDeps.EAC.Delete(ctx, epsID.String())
		r.NoError(err)

		r.NoError(sc.Periodic(ctx))

		r.True(nftChains(t, ctx, sc).Contains(parentChain), "parent chain should still exist")
		r.False(nftChains(t, ctx, sc).Contains(epChain), "orphan endpoint chain should be deleted now that the parent no longer pins it")

		// Parent body when empty: counter + drop = 2 rules.
		r.Equal(2, nftRuleCount(t, ctx, sc, parentChain), "empty-endpoint parent should be counter + drop")
	})

	t.Run("rebuilds parent chain vmap when endpoint IP rotates", func(t *testing.T) {
		r := require.New(t)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		testDeps, cleanup := testutils.NewTestDeps()
		defer cleanup()

		sc, err := newServiceController(testDeps)
		r.NoError(err)
		r.NoError(sc.Init(ctx))

		const oldIP = "10.8.54.24"
		const newIP = "10.8.29.42"
		const svcIP = "10.99.124.1"
		const port = 6669

		svcID := entity.Id(svcName())
		svc := &network_v1alpha.Service{
			ID:   svcID,
			Ip:   []string{svcIP},
			Port: []network_v1alpha.Port{{Name: "irc", Port: port}},
		}
		putService(t, ctx, testDeps.EAC, svc)

		oldEpsID := entity.Id("endpoints-old-" + svcID.String())
		putEndpoints(t, ctx, testDeps.EAC, &network_v1alpha.Endpoints{
			ID:       oldEpsID,
			Service:  svcID,
			Endpoint: []network_v1alpha.Endpoint{{Ip: oldIP, Port: port}},
		})

		r.NoError(sc.Create(ctx, svc, &entity.Meta{Entity: entity.New(svc.Encode), Revision: 1}))

		parentChain := sc.serviceChain(netip.MustParseAddr(svcIP), port, "tcp")
		oldEpChain := sc.endpointChain(netip.MustParseAddr(oldIP), port, "tcp")
		newEpChain := sc.endpointChain(netip.MustParseAddr(newIP), port, "tcp")

		r.True(nftChains(t, ctx, sc).Contains(oldEpChain))
		r.False(nftChains(t, ctx, sc).Contains(newEpChain))

		// Rotate the endpoint IP without firing UpdateEndpoints. Old Endpoints
		// entity is gone (its sandbox died), new Endpoints entity took its
		// place. Periodic should rebuild the parent vmap to the new chain and
		// drop the orphan.
		_, err = testDeps.EAC.Delete(ctx, oldEpsID.String())
		r.NoError(err)
		putEndpoints(t, ctx, testDeps.EAC, &network_v1alpha.Endpoints{
			ID:       entity.Id("endpoints-new-" + svcID.String()),
			Service:  svcID,
			Endpoint: []network_v1alpha.Endpoint{{Ip: newIP, Port: port}},
		})

		r.NoError(sc.Periodic(ctx))

		r.True(nftChains(t, ctx, sc).Contains(parentChain), "parent chain still exists")
		r.True(nftChains(t, ctx, sc).Contains(newEpChain), "new endpoint chain created")
		r.False(nftChains(t, ctx, sc).Contains(oldEpChain), "stale endpoint chain pruned")

		// Non-empty parent: counter + one masq jump per routablePrefix + vmap.
		// TestDeps installs 3 routable prefixes (subnet, 10.10.0.0/16, fd47:.../64).
		r.Equal(5, nftRuleCount(t, ctx, sc, parentChain), "parent chain should be rebuilt to current shape")
	})

	t.Run("rebuilds parent chain when extra rules have accumulated", func(t *testing.T) {
		r := require.New(t)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		testDeps, cleanup := testutils.NewTestDeps()
		defer cleanup()

		sc, err := newServiceController(testDeps)
		r.NoError(err)
		r.NoError(sc.Init(ctx))

		const sandboxIP = "10.8.125.7"
		const svcIP = "10.99.125.1"
		const port = 6669

		svcID := entity.Id(svcName())
		svc := &network_v1alpha.Service{
			ID:   svcID,
			Ip:   []string{svcIP},
			Port: []network_v1alpha.Port{{Name: "irc", Port: port}},
		}
		putService(t, ctx, testDeps.EAC, svc)
		putEndpoints(t, ctx, testDeps.EAC, &network_v1alpha.Endpoints{
			ID:       entity.Id("endpoints-" + svcID.String()),
			Service:  svcID,
			Endpoint: []network_v1alpha.Endpoint{{Ip: sandboxIP, Port: port}},
		})

		r.NoError(sc.Create(ctx, svc, &entity.Meta{Entity: entity.New(svc.Encode), Revision: 1}))

		parentChain := sc.serviceChain(netip.MustParseAddr(svcIP), port, "tcp")
		r.Equal(5, nftRuleCount(t, ctx, sc, parentChain), "fresh parent should have 5 rules")

		// Inject the kind of stacked-duplicate state that the pre-#795
		// append-without-flush code would leave behind across redeploys.
		injectTx := sc.nft.NewTransaction()
		injectTx.Add(&knftables.Rule{Chain: parentChain, Rule: "counter"})
		injectTx.Add(&knftables.Rule{Chain: parentChain, Rule: "ip saddr 10.123.0.0/16 jump mark-for-masq"})
		injectTx.Add(&knftables.Rule{Chain: parentChain, Rule: "ip saddr 10.124.0.0/16 jump mark-for-masq"})
		r.NoError(sc.nft.Run(ctx, injectTx))
		r.Greater(nftRuleCount(t, ctx, sc, parentChain), 5, "injection should have added rules")

		r.NoError(sc.Periodic(ctx))

		r.Equal(5, nftRuleCount(t, ctx, sc, parentChain), "Periodic should flush and rebuild to the canonical 5-rule shape")
	})

	t.Run("does not touch chains outside managed prefixes", func(t *testing.T) {
		r := require.New(t)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		testDeps, cleanup := testutils.NewTestDeps()
		defer cleanup()

		sc, err := newServiceController(testDeps)
		r.NoError(err)
		r.NoError(sc.Init(ctx))

		// Inject an unrelated chain directly. It doesn't match any managed
		// prefix and isn't a static chain, so GC must leave it alone.
		const foreign = "ext_unrelated_chain"
		tx := sc.nft.NewTransaction()
		tx.Add(&knftables.Chain{Name: foreign})
		r.NoError(sc.nft.Run(ctx, tx))
		t.Cleanup(func() {
			cleanup := sc.nft.NewTransaction()
			cleanup.Delete(&knftables.Chain{Name: foreign})
			_ = sc.nft.Run(context.Background(), cleanup)
		})

		r.True(nftChains(t, ctx, sc).Contains(foreign), "foreign chain should be present before GC")

		r.NoError(sc.Periodic(ctx))

		r.True(nftChains(t, ctx, sc).Contains(foreign), "GC must not delete chains outside managed prefixes")
	})
}
