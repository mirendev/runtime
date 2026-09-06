package service

import (
	"log/slog"
	"net/netip"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"sigs.k8s.io/knftables"
)

func newTestController(nft knftables.Interface) *ServiceController {
	return &ServiceController{
		Log:              slog.New(slog.DiscardHandler),
		nft:              nft,
		chainEndpoints:   map[string][]string{},
		routablePrefixes: []netip.Prefix{netip.MustParsePrefix("10.8.0.0/16")},
	}
}

// TestChainBodyDeclaresItsCounter: nft rejects a rule naming a counter that does
// not exist, and rolls the whole batch back. The chain body must therefore
// declare the counter it references rather than assuming Init got there first.
func TestChainBodyDeclaresItsCounter(t *testing.T) {
	s := newTestController(knftables.NewFake(knftables.InetFamily, tableName))
	tx := s.nft.NewTransaction()
	tx.Add(&knftables.Table{})

	s.writeChainBody(tx, "service_test", "services", []string{"endpoint_a"})

	body := tx.String()

	declare := strings.Index(body, `add counter inet `+tableName+` services`)
	reference := strings.Index(body, `counter name "services"`)

	require.NotEqual(t, -1, declare, "the batch must declare the counter it names:\n%s", body)
	require.Less(t, declare, reference, "the counter must be declared before it is referenced:\n%s", body)
}

// TestFailedApplyDoesNotPoisonChainCache is the reason an empty service chain
// survives forever rather than being repaired on the next pass. setEndpoints
// records what it wrote before the batch is applied, so a rejected batch leaves
// the cache asserting rules nft never took; the next pass then sees "no change"
// and skips the rebuild, leaving the chain empty while the verdict map happily
// routes traffic into it.
func TestFailedApplyDoesNotPoisonChainCache(t *testing.T) {
	s := newTestController(knftables.NewFake(knftables.InetFamily, tableName))
	const chain = "service_test"

	// Pass one: writes the body, then the apply is rejected.
	tx1 := s.nft.NewTransaction()
	s.setEndpoints(tx1, chain, "services", []string{"endpoint_a"})
	require.Contains(t, mustString(t, tx1), chain, "the first pass must write the chain body")
	s.invalidateChainCache() // what Create and the GC pass now do on apply failure

	// Pass two: the same endpoints. The rebuild must happen again, because the
	// first one never reached the kernel.
	tx2 := s.nft.NewTransaction()
	s.setEndpoints(tx2, chain, "services", []string{"endpoint_a"})
	require.Contains(t, mustString(t, tx2), `counter name "services"`,
		"after a rejected batch the next pass must rebuild the chain, not skip it")
}

// TestUnchangedEndpointsStillSkipRebuild guards the optimization the cache is
// there for: with no failure in between, an unchanged endpoint set must not
// rewrite the chain.
func TestUnchangedEndpointsStillSkipRebuild(t *testing.T) {
	s := newTestController(knftables.NewFake(knftables.InetFamily, tableName))
	const chain = "service_test"

	tx1 := s.nft.NewTransaction()
	s.setEndpoints(tx1, chain, "services", []string{"endpoint_a"})
	require.NotEmpty(t, mustString(t, tx1))

	tx2 := s.nft.NewTransaction()
	s.setEndpoints(tx2, chain, "services", []string{"endpoint_a"})
	require.Equal(t, 0, tx2.NumOperations(), "an unchanged endpoint set must skip the rebuild")
}

func mustString(t *testing.T, tx *knftables.Transaction) string {
	t.Helper()
	return tx.String()
}
