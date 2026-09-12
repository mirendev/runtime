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

// TestChainBodyDependsOnNoNamedObjects applies a chain body against real
// nftables in a table where no named counter exists at all.
//
// This is the production failure. The body used to open with
// `counter name "services"`, the nftables objref expression, which needs
// CONFIG_NFT_OBJREF. On a kernel built without it nft rejects that rule with
// ENOENT pointing at the name -- indistinguishable from "the counter is
// missing" -- and rolls back the whole batch, so the chain stayed empty while
// the verdict map went on routing to it. Every service IP on such a host was a
// black hole, permanently, because every reconcile failed identically.
//
// This test cannot reproduce the missing kernel option, since the kernel under
// iso has it. What it does pin is the property that makes the option
// irrelevant: the body references no named object, so there is nothing for a
// kernel to fail to look up.
func TestChainBodyDependsOnNoNamedObjects(t *testing.T) {
	r := require.New(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	testDeps, cleanup := testutils.NewTestDeps()
	defer cleanup()

	sc, err := newServiceController(testDeps)
	r.NoError(err)
	r.NoError(sc.Init(ctx))

	counters, err := sc.nft.ListCounters(ctx)
	r.NoError(err)
	r.Empty(counters, "Init must not create named counters; nothing may depend on objref")

	chain := "service_probe_" + idgen.GenNS("c")
	t.Cleanup(func() {
		tx := sc.nft.NewTransaction()
		tx.Add(&knftables.Table{})
		tx.Delete(&knftables.Chain{Name: chain})
		_ = sc.nft.Run(context.Background(), tx)
	})

	tx := sc.nft.NewTransaction()
	tx.Add(&knftables.Table{})
	tx.Add(&knftables.Chain{Name: chain})
	sc.writeChainBody(tx, chain, nil)

	r.NotContains(tx.String(), "counter name",
		"a named-counter reference needs CONFIG_NFT_OBJREF, which some kernels lack")

	r.NoError(sc.nft.Run(ctx, tx))
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
