package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"sigs.k8s.io/knftables"

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
		tx := sc.nft.NewTransaction()
		tx.Add(&knftables.Table{})
		tx.Delete(&knftables.Chain{Name: chain})
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
