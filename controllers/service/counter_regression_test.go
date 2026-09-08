package service

import (
	"context"
	"errors"
	"log/slog"
	"net/netip"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"sigs.k8s.io/knftables"

	"miren.dev/runtime/pkg/set"
)

func newTestController(nft knftables.Interface) *ServiceController {
	return &ServiceController{
		Log:              slog.New(slog.DiscardHandler),
		nft:              nft,
		chainEndpoints:   map[string][]string{},
		routablePrefixes: []netip.Prefix{netip.MustParsePrefix("10.8.0.0/16")},
	}
}

// rejectingNFT fails the next Run, the way nft rejects a batch it cannot
// process, and passes everything else through to the fake.
type rejectingNFT struct {
	knftables.Interface
	rejectNext bool
}

func (r *rejectingNFT) Run(ctx context.Context, tx *knftables.Transaction) error {
	if r.rejectNext {
		r.rejectNext = false
		return errors.New("Could not process rule: No such file or directory")
	}
	return r.Interface.Run(ctx, tx)
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

// TestRejectedGCBatchLeavesCacheAlone is the reason an empty service chain used
// to survive forever rather than being repaired on the next pass. The cache
// exists to skip a flush+rebuild when nothing changed, so an entry for a body
// nft never accepted makes the next pass skip the rebuild it needs, leaving the
// chain empty while the verdict map happily routes traffic into it.
//
// Drives applyGC's rejection path rather than the cache helper directly, so
// deleting the ordering it depends on fails the test.
func TestRejectedGCBatchLeavesCacheAlone(t *testing.T) {
	ctx := context.Background()
	nft := &rejectingNFT{Interface: knftables.NewFake(knftables.InetFamily, tableName)}
	s := newTestController(nft)

	require.NoError(t, s.nft.Run(ctx, baseTable(s)))

	target, actual, chain := oneServiceTarget(s, netip.MustParseAddr("10.10.0.7"))

	nft.rejectNext = true
	require.Error(t, s.applyGC(ctx, target, actual), "the GC batch must be rejected for this test to mean anything")

	s.mu.Lock()
	_, cached := s.chainEndpoints[chain]
	s.mu.Unlock()
	require.False(t, cached, "a rejected batch must not leave the cache claiming a body nft never took")

	// Second pass: same endpoints. The rebuild must happen again, because the
	// first one never reached the kernel.
	require.NoError(t, s.applyGC(ctx, target, actual))
	require.NotEmpty(t, mustRules(t, ctx, s, chain), "after a rejected batch the next pass must rebuild the chain, not skip it")
}

// TestRejectedGCBatchKeepsExistingCache is the other half: a rejected batch
// changed nothing, so entries describing bodies that are still in the kernel
// have to survive. Clearing the whole cache instead would be safe but would
// throw away the optimization on every transient error.
func TestRejectedGCBatchKeepsExistingCache(t *testing.T) {
	ctx := context.Background()
	nft := &rejectingNFT{Interface: knftables.NewFake(knftables.InetFamily, tableName)}
	s := newTestController(nft)

	require.NoError(t, s.nft.Run(ctx, baseTable(s)))

	target, actual, chain := oneServiceTarget(s, netip.MustParseAddr("10.10.0.8"))

	require.NoError(t, s.applyGC(ctx, target, actual))
	s.mu.Lock()
	_, cached := s.chainEndpoints[chain]
	s.mu.Unlock()
	require.True(t, cached, "a batch nft accepted must be cached")

	nft.rejectNext = true
	require.Error(t, s.applyGC(ctx, target, actual))

	s.mu.Lock()
	_, stillCached := s.chainEndpoints[chain]
	s.mu.Unlock()
	require.True(t, stillCached, "a rejected batch changed nothing, so the cache must keep describing what is still there")
}

// TestUnchangedEndpointsStillSkipRebuild guards the optimization the cache is
// there for: with no failure in between, an unchanged endpoint set must not
// rewrite the chain.
func TestUnchangedEndpointsStillSkipRebuild(t *testing.T) {
	s := newTestController(knftables.NewFake(knftables.InetFamily, tableName))
	const chain = "service_test"

	pending := map[string][]string{}
	tx1 := s.nft.NewTransaction()
	s.setEndpoints(tx1, pending, chain, "services", []string{"endpoint_a"})
	require.NotEmpty(t, tx1.String())
	s.commitChainCache(pending)

	tx2 := s.nft.NewTransaction()
	s.setEndpoints(tx2, map[string][]string{}, chain, "services", []string{"endpoint_a"})
	require.Equal(t, 0, tx2.NumOperations(), "an unchanged endpoint set must skip the rebuild")
}

func mustRules(t *testing.T, ctx context.Context, s *ServiceController, chain string) []*knftables.Rule {
	t.Helper()
	rules, err := s.nft.ListRules(ctx, chain)
	require.NoError(t, err)
	return rules
}

// baseTable is the slice of Init the GC path needs: the table, the verdict map
// addServiceChain points at, and the chain its bodies jump to.
func baseTable(s *ServiceController) *knftables.Transaction {
	tx := s.nft.NewTransaction()
	tx.Add(&knftables.Table{})
	tx.Add(&knftables.Map{
		Name: mapServiceIP4s,
		Type: "ipv4_addr . inet_proto . inet_service : verdict",
	})
	tx.Add(&knftables.Chain{Name: chainMarkForMasq})
	return tx
}

// oneServiceTarget builds the GC inputs for a single service IP backed by one
// endpoint, shaped the way computeTargetState would. Returns the service chain
// name the pass will write.
func oneServiceTarget(s *ServiceController, ip netip.Addr) (*targetState, *kernelState, string) {
	epIP := netip.MustParseAddr("10.8.0.5")
	epChain := s.endpointChain(epIP, 80, "tcp")

	target := &targetState{
		chains:   set.New[string](),
		elements: map[string]set.Set[string]{},
		serviceSpecs: []serviceChainSpec{
			{ip: ip, port: 80, proto: "tcp", endpoints: []string{epChain}},
		},
		endpointSpecs: map[string]endpointChainSpec{
			epChain: {ip: epIP, port: 80, proto: "tcp"},
		},
	}
	actual := &kernelState{
		chains:   map[string]chainKind{},
		elements: map[string]map[string]*knftables.Element{},
	}
	return target, actual, s.serviceChain(ip, 80, "tcp")
}
