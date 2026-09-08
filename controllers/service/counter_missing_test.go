package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"sigs.k8s.io/knftables"

	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/api/network/network_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/types"
	"miren.dev/runtime/pkg/idgen"
	"miren.dev/runtime/pkg/testutils"
)

// TestChainBodyLandsWhenCounterUndeclared is the production failure against real
// nftables. A chain body names a counter; nft answers ENOENT if that counter
// does not exist and rolls the whole batch back, leaving the chain empty while
// the verdict map still routes to it -- a service IP that is a black hole.
//
// Init declares the counters once at startup, so a body written against kernel
// state where that never took effect hits exactly this. The body must therefore
// carry its own declaration.
//
// Uses a counter name that has never been declared rather than deleting the
// real ones: the table is shared by every test in this package's iso
// environment, and nft refuses to delete a named counter while any rule still
// references it (EBUSY), so removing them is both hostile to neighbouring tests
// and not reliably possible.
func TestChainBodyLandsWhenCounterUndeclared(t *testing.T) {
	r := require.New(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	testDeps, cleanup := testutils.NewTestDeps()
	defer cleanup()

	sc, err := newServiceController(testDeps)
	r.NoError(err)
	r.NoError(sc.Init(ctx))

	chain := "service_probe_" + idgen.GenNS("c")
	counter := "probe_" + idgen.GenNS("k")

	counters, err := sc.nft.ListCounters(ctx)
	r.NoError(err)
	for _, c := range counters {
		r.NotEqual(counter, c.Name, "the probe counter must not already exist")
	}

	t.Cleanup(func() {
		// Chain first: nft refuses to delete a counter a rule still
		// references. Both leak into the package's shared table otherwise.
		tx := sc.nft.NewTransaction()
		tx.Add(&knftables.Table{})
		tx.Delete(&knftables.Chain{Name: chain})
		tx.Delete(&knftables.Counter{Name: counter})
		_ = sc.nft.Run(context.Background(), tx)
	})

	tx := sc.nft.NewTransaction()
	tx.Add(&knftables.Table{})
	tx.Add(&knftables.Chain{Name: chain})
	sc.writeChainBody(tx, chain, counter, nil)

	r.NoError(sc.nft.Run(ctx, tx),
		"a chain body naming an undeclared counter must still apply; nft rejects the whole batch otherwise")

	r.NotZero(nftRuleCount(t, ctx, sc, chain),
		"the chain must have rules: an empty one is a black hole, since the verdict map routes to it regardless")
}

// TestRejectedCreateBatchLeavesCacheAlone drives the production rejection path:
// Create builds a batch, nft rejects it, and the next reconcile with the same
// endpoints must rebuild rather than trust a cache entry for a body that never
// reached the kernel. Runs against real nftables so the rebuilt chain can be
// read back from the kernel.
func TestRejectedCreateBatchLeavesCacheAlone(t *testing.T) {
	r := require.New(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	testDeps, cleanup := testutils.NewTestDeps()
	defer cleanup()

	sc, err := newServiceController(testDeps)
	r.NoError(err)
	r.NoError(sc.Init(ctx))

	svcID := entity.Id(idgen.GenNS("svc"))
	svc := &network_v1alpha.Service{
		ID:    svcID,
		Match: types.Labels{types.Label{Key: "app", Value: "probe"}},
		Port:  []network_v1alpha.Port{{Name: "http", Port: 8080, NodePort: 31808}},
	}
	var rpcE entityserver_v1alpha.Entity
	rpcE.SetAttrs(entity.New(
		entity.Keyword(entity.Ident, svcID.String()),
		svc.Encode).Attrs())
	_, err = testDeps.EAC.Put(ctx, &rpcE)
	r.NoError(err)

	epID := entity.Id("endpoints-" + svcID.String())
	putEndpoints(t, ctx, testDeps.EAC, &network_v1alpha.Endpoints{
		ID:       epID,
		Service:  svcID,
		Endpoint: []network_v1alpha.Endpoint{{Ip: "10.8.104.21", Port: 8080}},
	})

	meta := &entity.Meta{Entity: entity.New(svc.Encode), Revision: 1}
	npChain := sc.nodeportChain(31808, "tcp")

	t.Cleanup(func() {
		tx := sc.nft.NewTransaction()
		tx.Add(&knftables.Table{})
		tx.Delete(&knftables.Chain{Name: npChain})
		_ = sc.nft.Run(context.Background(), tx)
	})

	nft := &rejectingNFT{Interface: sc.nft, rejectNext: true}
	sc.nft = nft

	r.Error(sc.Create(ctx, svc, meta), "the batch must be rejected for this test to mean anything")

	sc.mu.Lock()
	_, cached := sc.chainEndpoints[npChain]
	sc.mu.Unlock()
	r.False(cached, "a rejected batch must not leave the cache claiming a body nft never took")

	// Same endpoints again. The rebuild must happen, and the chain must end up
	// with rules rather than being a black hole behind a live verdict map.
	r.NoError(sc.Create(ctx, svc, meta))
	r.NotZero(nftRuleCount(t, ctx, sc, npChain),
		"after a rejected batch the next pass must rebuild the chain, not skip it")
}
