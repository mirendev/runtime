package coordinate

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/types"
	"miren.dev/runtime/pkg/lifecyclesync"
	"miren.dev/runtime/pkg/serverlifecycle"
)

// fakeFleet is the inventory and every runner's ledger in one place, so a
// runner finishing its operation can show up in the inventory the way a
// real one does by rejoining.
type fakeFleet struct {
	mu    sync.Mutex
	nodes map[string]*compute_v1alpha.Node
	// behavior per runner id
	failAt      map[string]serverlifecycle.Phase // fail (and roll back) when reaching this phase
	unmanaged   map[string]bool                  // no lifecycle service
	unreachable map[string]bool
	neverRejoin map[string]bool // operation succeeds but the node never comes back READY
	// dropsWhileRestarting makes Get fail once the runner op is restarting,
	// the way a real runner's RPC goes away, until the next dial.
	dropsWhileRestarting map[string]bool
	dropped              map[string]bool
	busy                 map[string]bool // Start answers ErrBusy: another operation holds the ledger
	dialed               []string
	// inventoryErrors makes the next N inventory reads fail, listing and
	// single lookups alike, the way an entity server that is not answering
	// would.
	inventoryErrors int
	ops             map[string]*serverlifecycle.Operation
	cordons         []string // scheduling changes, in order: "name:cordoned" / "name:schedulable"
	requests        []string // target version of each operation started on a runner
	dials           int
}

func newFakeFleet() *fakeFleet {
	return &fakeFleet{
		nodes: map[string]*compute_v1alpha.Node{}, failAt: map[string]serverlifecycle.Phase{},
		unmanaged: map[string]bool{}, unreachable: map[string]bool{}, neverRejoin: map[string]bool{},
		dropsWhileRestarting: map[string]bool{}, dropped: map[string]bool{}, busy: map[string]bool{},
		ops: map[string]*serverlifecycle.Operation{},
	}
}

func (f *fakeFleet) add(name, role, version string, status compute_v1alpha.NodeStatus) *compute_v1alpha.Node {
	f.mu.Lock()
	defer f.mu.Unlock()
	node := &compute_v1alpha.Node{
		ID: entity.Id("node/" + name), Name: name, RunnerId: "rid-" + name, Version: version, Status: status,
		ApiAddress: name + ":8444", Constraints: types.LabelSet("role", role),
	}
	f.nodes[node.RunnerId] = node
	return node
}

func (f *fakeFleet) Runners(context.Context) ([]*compute_v1alpha.Node, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.inventoryErrors > 0 {
		f.inventoryErrors--
		return nil, errors.New("entity server unavailable")
	}
	var out []*compute_v1alpha.Node
	for _, n := range f.nodes {
		if role, _ := n.Constraints.Get("role"); role == "coordinator" {
			continue
		}
		c := *n
		out = append(out, &c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (f *fakeFleet) Runner(_ context.Context, runnerID string) (*compute_v1alpha.Node, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.inventoryErrors > 0 {
		f.inventoryErrors--
		return nil, errors.New("entity server unavailable")
	}
	n, ok := f.nodes[runnerID]
	if !ok {
		return nil, nil
	}
	c := *n
	return &c, nil
}

func (f *fakeFleet) SetScheduling(_ context.Context, node *compute_v1alpha.Node, scheduling entity.Id) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := f.nodes[node.RunnerId]
	switch scheduling {
	case compute_v1alpha.NodeSchedulingCordonedId:
		n.Scheduling = compute_v1alpha.CORDONED
		f.cordons = append(f.cordons, n.Name+":cordoned")
	default:
		n.Scheduling = compute_v1alpha.SCHEDULABLE
		f.cordons = append(f.cordons, n.Name+":schedulable")
	}
	return nil
}

func (f *fakeFleet) dial(_ context.Context, address string) (RunnerLifecycleClient, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.dials++
	f.dialed = append(f.dialed, address)
	var node *compute_v1alpha.Node
	for _, n := range f.nodes {
		if n.ApiAddress == address {
			node = n
		}
	}
	if node == nil || f.unreachable[node.RunnerId] {
		return nil, errors.New("connection refused")
	}
	if f.unmanaged[node.RunnerId] {
		return nil, fmt.Errorf("%w: no such service", ErrRunnerUnmanaged)
	}
	return &fakeRunnerClient{fleet: f, node: node}, nil
}

// fakeRunnerClient advances the runner's operation one phase per Get, the
// way polling a real one would show it moving.
type fakeRunnerClient struct {
	fleet  *fakeFleet
	node   *compute_v1alpha.Node
	closed bool
}

var runnerPhases = []serverlifecycle.Phase{
	serverlifecycle.PhaseDownloading, serverlifecycle.PhaseInstalling, serverlifecycle.PhaseRestarting,
	serverlifecycle.PhaseVerifying, serverlifecycle.PhaseSucceeded,
}

// Start is the ledger's idempotent start: it records the operation the first
// time and returns the record after that, which is also how the walker
// polls. Each call after the first moves the fake runner one phase along.
func (f *fakeFleet) dialsTo(address string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, a := range f.dialed {
		if a == address {
			n++
		}
	}
	return n
}

func (c *fakeRunnerClient) Start(ctx context.Context, op *serverlifecycle.Operation) (*serverlifecycle.Operation, error) {
	c.fleet.mu.Lock()
	if c.fleet.busy[c.node.RunnerId] {
		c.fleet.mu.Unlock()
		return nil, fmt.Errorf("rpc: %w: 01OTHER (upgrade, installing); see 'miren runner operations list'", serverlifecycle.ErrBusy)
	}
	if _, ok := c.fleet.ops[op.ID]; !ok {
		remote := *op
		remote.Phase = serverlifecycle.PhasePending
		remote.PreviousVersion = c.node.Version
		c.fleet.ops[op.ID] = &remote
		c.fleet.requests = append(c.fleet.requests, op.TargetVersion)
		cp := remote
		c.fleet.mu.Unlock()
		return &cp, nil
	}
	c.fleet.mu.Unlock()
	return c.Get(ctx, op.ID)
}

func (c *fakeRunnerClient) Get(_ context.Context, id string) (*serverlifecycle.Operation, error) {
	c.fleet.mu.Lock()
	defer c.fleet.mu.Unlock()
	remote, ok := c.fleet.ops[id]
	if !ok {
		return nil, serverlifecycle.ErrNotFound
	}
	if c.fleet.dropsWhileRestarting[c.node.RunnerId] && remote.Phase == serverlifecycle.PhaseRestarting && !c.fleet.dropped[c.node.RunnerId] {
		c.fleet.dropped[c.node.RunnerId] = true
		c.closed = true
		return nil, errors.New("connection reset")
	}
	if c.closed {
		return nil, errors.New("use of closed connection")
	}
	if !remote.Done() {
		next := runnerPhases[0]
		for i, p := range runnerPhases {
			if remote.Phase == p && i+1 < len(runnerPhases) {
				next = runnerPhases[i+1]
			}
		}
		if failAt, ok := c.fleet.failAt[c.node.RunnerId]; ok && next == failAt {
			remote.Phase = serverlifecycle.PhaseRolledBack
			remote.Error = "runner not ready within 1s"
			remote.NewVersion = remote.PreviousVersion
		} else {
			remote.Phase = next
			if next == serverlifecycle.PhaseSucceeded {
				remote.NewVersion = "v2.0.0"
				if !c.fleet.neverRejoin[c.node.RunnerId] {
					c.node.Version = "v2.0.0"
					c.node.Status = compute_v1alpha.READY
				}
			}
		}
	}
	cp := *remote
	return &cp, nil
}

func (c *fakeRunnerClient) Close() { c.closed = true }

func newTestUpgrader(t *testing.T, fleet *fakeFleet) (*RunnerUpgrader, *serverlifecycle.Store) {
	t.Helper()
	store, err := serverlifecycle.NewStore(t.TempDir())
	require.NoError(t, err)
	// Short enough to keep the tests quick, long enough that a loaded CI
	// runner does not turn a phase walk into a timeout. Nothing below
	// asserts on a timeout actually expiring except through the fake's
	// own behavior (neverRejoin, unreachable), so these only bound waits.
	opts := DefaultRunnerUpgradeOptions()
	opts.PollInterval = time.Millisecond
	opts.ReadyTimeout = 300 * time.Millisecond
	opts.NotReadyGrace = 300 * time.Millisecond
	opts.StepTimeout = 5 * time.Second
	opts.AdoptRetry = time.Second
	opts.RetryFor = 2 * time.Second
	opts.RetryBackoff = 10 * time.Millisecond
	opts.RetryBackoffMax = 20 * time.Millisecond
	u := NewRunnerUpgrader(slog.Default(), store, lifecyclesync.NewWatcher(slog.Default(), store), "inst-2", fleet, fleet.dial, opts)
	return u, store
}

func handedOff(t *testing.T, store *serverlifecycle.Store) *serverlifecycle.Operation {
	t.Helper()
	op := serverlifecycle.NewOperation(serverlifecycle.ActionUpgrade, "test")
	op.TargetVersion = "latest"
	op.ResolvedVersion = "v2.0.0"
	op.NewVersion = "v2.0.0"
	op.PreviousVersion = "v1.0.0"
	op.Phase = serverlifecycle.PhaseUpgradingRunners
	require.NoError(t, store.Create(op))
	return op
}

func stepByName(op *serverlifecycle.Operation, name string) *serverlifecycle.NodeStep {
	for _, s := range op.Nodes {
		if s.Name == name {
			return s
		}
	}
	return nil
}

func TestWalkUpgradesEveryRunnerInOrder(t *testing.T) {
	fleet := newFakeFleet()
	fleet.add("coord", "coordinator", "v2.0.0", compute_v1alpha.READY)
	fleet.add("b-runner", "runner", "v1.0.0", compute_v1alpha.READY)
	fleet.add("a-runner", "runner", "v1.0.0", compute_v1alpha.READY)
	u, store := newTestUpgrader(t, fleet)
	op := handedOff(t, store)

	require.NoError(t, u.drive(t.Context(), op.ID))

	got, err := store.Get(op.ID)
	require.NoError(t, err)
	require.Equal(t, serverlifecycle.PhaseSucceeded, got.Phase, got.Error)
	require.Equal(t, "inst-2", got.DrivenBy)
	require.NotNil(t, got.FinishedAt)
	require.Len(t, got.Nodes, 2)
	require.Equal(t, "a-runner", got.Nodes[0].Name)
	require.Equal(t, "b-runner", got.Nodes[1].Name)
	for _, step := range got.Nodes {
		require.Equal(t, serverlifecycle.PhaseSucceeded, step.Phase, step.Error)
		require.Equal(t, "v1.0.0", step.PreviousVersion)
		require.Equal(t, "v2.0.0", step.NewVersion)
		require.NotEmpty(t, step.OperationID)
		require.False(t, step.Cordoned)
		require.NotNil(t, step.FinishedAt)
	}
	// Serial: a finishes (and is uncordoned) before b is touched.
	require.Equal(t, []string{"a-runner:cordoned", "a-runner:schedulable", "b-runner:cordoned", "b-runner:schedulable"}, fleet.cordons)
}

func TestWalkRedialsARunnerThatRestartsUnderIt(t *testing.T) {
	fleet := newFakeFleet()
	fleet.add("a-runner", "runner", "v1.0.0", compute_v1alpha.READY)
	fleet.dropsWhileRestarting["rid-a-runner"] = true
	u, store := newTestUpgrader(t, fleet)
	op := handedOff(t, store)

	require.NoError(t, u.drive(t.Context(), op.ID))

	got, err := store.Get(op.ID)
	require.NoError(t, err)
	require.Equal(t, serverlifecycle.PhaseSucceeded, got.Phase, got.Error)
	require.Equal(t, serverlifecycle.PhaseSucceeded, got.Nodes[0].Phase)
	require.GreaterOrEqual(t, fleet.dials, 2, "the dropped connection was replaced")
}

func TestWalkStopsWhenTheCanaryFails(t *testing.T) {
	fleet := newFakeFleet()
	fleet.add("a-runner", "runner", "v1.0.0", compute_v1alpha.READY)
	fleet.add("b-runner", "runner", "v1.0.0", compute_v1alpha.READY)
	fleet.add("c-runner", "runner", "v1.0.0", compute_v1alpha.READY)
	fleet.failAt["rid-a-runner"] = serverlifecycle.PhaseVerifying
	u, store := newTestUpgrader(t, fleet)
	op := handedOff(t, store)

	require.NoError(t, u.drive(t.Context(), op.ID))

	got, err := store.Get(op.ID)
	require.NoError(t, err)
	require.Equal(t, serverlifecycle.PhaseFailed, got.Phase)
	require.Equal(t, "runner a-runner: runner not ready within 1s", got.Error)
	a := stepByName(got, "a-runner")
	require.Equal(t, serverlifecycle.PhaseRolledBack, a.Phase)
	require.Equal(t, "v1.0.0", a.NewVersion)
	for _, name := range []string{"b-runner", "c-runner"} {
		step := stepByName(got, name)
		require.Equal(t, serverlifecycle.StepSkipped, step.Phase, name)
		require.Contains(t, step.Error, "a-runner failed before any runner was upgraded")
		require.Empty(t, step.OperationID)
	}
	// The rolled-back runner is uncordoned: it is running the old build, not nothing.
	require.Equal(t, compute_v1alpha.SCHEDULABLE, fleet.nodes["rid-a-runner"].Scheduling)
}

func TestWalkContinuesPastAFailureOnceARunnerIsUpgraded(t *testing.T) {
	fleet := newFakeFleet()
	fleet.add("a-runner", "runner", "v1.0.0", compute_v1alpha.READY)
	fleet.add("b-runner", "runner", "v1.0.0", compute_v1alpha.READY)
	fleet.add("c-runner", "runner", "v1.0.0", compute_v1alpha.READY)
	fleet.add("d-runner", "runner", "v1.0.0", compute_v1alpha.READY)
	fleet.failAt["rid-b-runner"] = serverlifecycle.PhaseVerifying
	fleet.unreachable["rid-d-runner"] = true
	u, store := newTestUpgrader(t, fleet)
	op := handedOff(t, store)

	require.NoError(t, u.drive(t.Context(), op.ID))

	got, err := store.Get(op.ID)
	require.NoError(t, err)
	require.Equal(t, serverlifecycle.PhaseFailed, got.Phase)
	require.Equal(t, serverlifecycle.PhaseSucceeded, stepByName(got, "a-runner").Phase)
	require.Equal(t, serverlifecycle.PhaseRolledBack, stepByName(got, "b-runner").Phase)
	require.Equal(t, serverlifecycle.PhaseSucceeded, stepByName(got, "c-runner").Phase, "c was still attempted")
	require.Equal(t, serverlifecycle.PhaseFailed, stepByName(got, "d-runner").Phase)
	require.Contains(t, got.Error, "2 runners failed: b-runner: runner not ready within 1s; d-runner: cannot reach runner")
	require.Equal(t, "v2.0.0", fleet.nodes["rid-c-runner"].Version)
}

// A retry after a partial walk: the runners already on the build count as
// the canary, so a persistent failure on one runner no longer stops the
// rest, and the retry moves everyone it can.
func TestRetryConvergesAroundAStuckRunner(t *testing.T) {
	fleet := newFakeFleet()
	fleet.add("a-runner", "runner", "v2.0.0", compute_v1alpha.READY)
	fleet.add("b-runner", "runner", "v1.0.0", compute_v1alpha.READY)
	fleet.add("c-runner", "runner", "v1.0.0", compute_v1alpha.READY)
	fleet.failAt["rid-b-runner"] = serverlifecycle.PhaseInstalling
	u, store := newTestUpgrader(t, fleet)
	op := handedOff(t, store)

	require.NoError(t, u.drive(t.Context(), op.ID))

	got, err := store.Get(op.ID)
	require.NoError(t, err)
	require.Equal(t, serverlifecycle.PhaseFailed, got.Phase)
	require.Equal(t, "already running v2.0.0", stepByName(got, "a-runner").Progress)
	require.Equal(t, serverlifecycle.PhaseRolledBack, stepByName(got, "b-runner").Phase)
	require.Equal(t, serverlifecycle.PhaseSucceeded, stepByName(got, "c-runner").Phase)
	require.Equal(t, 0, fleet.dialsTo("a-runner:8444"), "a runner on the build is not asked")
}

func TestWalkFailsFastOnABusyRunner(t *testing.T) {
	fleet := newFakeFleet()
	fleet.add("a-runner", "runner", "v1.0.0", compute_v1alpha.READY)
	fleet.busy["rid-a-runner"] = true
	u, store := newTestUpgrader(t, fleet)
	op := handedOff(t, store)

	start := time.Now()
	require.NoError(t, u.drive(t.Context(), op.ID))
	require.Less(t, time.Since(start), 100*time.Millisecond, "did not wait out the step timeout")

	got, err := store.Get(op.ID)
	require.NoError(t, err)
	require.Equal(t, serverlifecycle.PhaseFailed, got.Phase)
	require.Contains(t, got.Nodes[0].Error, "runner has another operation in progress")
	require.Equal(t, compute_v1alpha.SCHEDULABLE, fleet.nodes["rid-a-runner"].Scheduling)
}

func TestWalkSkipsWhatItCannotUpgrade(t *testing.T) {
	fleet := newFakeFleet()
	fleet.add("a-current", "runner", "v2.0.0", compute_v1alpha.READY)
	fleet.add("b-old", "runner", "v1.0.0", compute_v1alpha.READY)
	fleet.unmanaged["rid-b-old"] = true
	fleet.add("c-down", "runner", "v1.0.0", compute_v1alpha.NodeStatus("status.unhealthy"))
	fleet.add("d-fine", "runner", "v1.0.0", compute_v1alpha.READY)
	u, store := newTestUpgrader(t, fleet)
	op := handedOff(t, store)

	require.NoError(t, u.drive(t.Context(), op.ID))

	got, err := store.Get(op.ID)
	require.NoError(t, err)
	require.Equal(t, serverlifecycle.PhaseSucceeded, got.Phase, got.Error)
	a := stepByName(got, "a-current")
	require.Equal(t, serverlifecycle.PhaseSucceeded, a.Phase)
	require.Equal(t, "already running v2.0.0", a.Progress)
	require.Empty(t, a.OperationID)
	b := stepByName(got, "b-old")
	require.Equal(t, serverlifecycle.StepSkipped, b.Phase)
	require.Contains(t, b.Error, "predates managed upgrades")
	c := stepByName(got, "c-down")
	require.Equal(t, serverlifecycle.StepSkipped, c.Phase)
	require.Contains(t, c.Error, "not ready (unhealthy)")
	require.Equal(t, serverlifecycle.PhaseSucceeded, stepByName(got, "d-fine").Phase)
	// Nothing was cordoned that was not upgraded.
	require.Equal(t, []string{"d-fine:cordoned", "d-fine:schedulable"}, fleet.cordons)
}

func TestWalkWaitsForARunnerStillRejoining(t *testing.T) {
	fleet := newFakeFleet()
	node := fleet.add("a-runner", "runner", "v1.0.0", compute_v1alpha.NodeStatus(""))
	u, store := newTestUpgrader(t, fleet)
	op := handedOff(t, store)

	go func() {
		time.Sleep(50 * time.Millisecond)
		fleet.mu.Lock()
		node.Status = compute_v1alpha.READY
		fleet.mu.Unlock()
	}()
	require.NoError(t, u.drive(t.Context(), op.ID))

	got, err := store.Get(op.ID)
	require.NoError(t, err)
	require.Equal(t, serverlifecycle.PhaseSucceeded, got.Phase, got.Error)
	require.Equal(t, serverlifecycle.PhaseSucceeded, got.Nodes[0].Phase)
}

func TestWalkFailsARunnerThatNeverRejoins(t *testing.T) {
	fleet := newFakeFleet()
	fleet.add("a-runner", "runner", "v1.0.0", compute_v1alpha.READY)
	fleet.neverRejoin["rid-a-runner"] = true
	u, store := newTestUpgrader(t, fleet)
	op := handedOff(t, store)

	require.NoError(t, u.drive(t.Context(), op.ID))

	got, err := store.Get(op.ID)
	require.NoError(t, err)
	require.Equal(t, serverlifecycle.PhaseFailed, got.Phase)
	step := got.Nodes[0]
	require.Equal(t, serverlifecycle.PhaseFailed, step.Phase)
	require.Contains(t, step.Error, "upgraded to v2.0.0 but is ready on v1.0.0")
}

func TestWalkFailsAnUnreachableRunner(t *testing.T) {
	fleet := newFakeFleet()
	fleet.add("a-runner", "runner", "v1.0.0", compute_v1alpha.READY)
	fleet.unreachable["rid-a-runner"] = true
	u, store := newTestUpgrader(t, fleet)
	op := handedOff(t, store)

	require.NoError(t, u.drive(t.Context(), op.ID))

	got, err := store.Get(op.ID)
	require.NoError(t, err)
	require.Equal(t, serverlifecycle.PhaseFailed, got.Phase)
	require.Contains(t, got.Nodes[0].Error, "cannot reach runner at a-runner:8444")
}

func TestWalkResumesFromCheckpointedSteps(t *testing.T) {
	fleet := newFakeFleet()
	fleet.add("a-runner", "runner", "v2.0.0", compute_v1alpha.READY)
	fleet.add("b-runner", "runner", "v1.0.0", compute_v1alpha.READY)
	u, store := newTestUpgrader(t, fleet)
	op := handedOff(t, store)
	// A previous server instance walked a-runner and died while b-runner was
	// mid-operation, cordoned, with its runner op already recorded.
	op.DrivenBy = "inst-1"
	started := time.Now().UTC()
	op.Nodes = []*serverlifecycle.NodeStep{
		{Name: "a-runner", RunnerID: "rid-a-runner", Phase: serverlifecycle.PhaseSucceeded, OperationID: "01AAAAAAAAAAAAAAAAAAAAAAAA", StartedAt: &started, FinishedAt: &started},
		{Name: "b-runner", RunnerID: "rid-b-runner", Phase: serverlifecycle.PhaseInstalling, OperationID: "01BBBBBBBBBBBBBBBBBBBBBBBB", Cordoned: true, StartedAt: &started},
	}
	require.NoError(t, store.Update(op))
	fleet.nodes["rid-b-runner"].Scheduling = compute_v1alpha.CORDONED
	fleet.ops["01BBBBBBBBBBBBBBBBBBBBBBBB"] = &serverlifecycle.Operation{
		ID: "01BBBBBBBBBBBBBBBBBBBBBBBB", Action: serverlifecycle.ActionUpgrade, TargetVersion: "latest",
		Phase: serverlifecycle.PhaseInstalling, PreviousVersion: "v1.0.0",
	}

	require.NoError(t, u.drive(t.Context(), op.ID))

	got, err := store.Get(op.ID)
	require.NoError(t, err)
	require.Equal(t, serverlifecycle.PhaseSucceeded, got.Phase, got.Error)
	require.Equal(t, "inst-2", got.DrivenBy)
	require.Equal(t, "01AAAAAAAAAAAAAAAAAAAAAAAA", got.Nodes[0].OperationID)
	b := got.Nodes[1]
	require.Equal(t, "01BBBBBBBBBBBBBBBBBBBBBBBB", b.OperationID)
	require.Equal(t, serverlifecycle.PhaseSucceeded, b.Phase, b.Error)
	require.False(t, b.Cordoned)
	// Only the resumed step's uncordon; a-runner was not touched again.
	require.Equal(t, []string{"b-runner:schedulable"}, fleet.cordons)
}

func TestWalkWithNoRunnersSucceeds(t *testing.T) {
	fleet := newFakeFleet()
	fleet.add("coord", "coordinator", "v2.0.0", compute_v1alpha.READY)
	u, store := newTestUpgrader(t, fleet)
	op := handedOff(t, store)

	require.NoError(t, u.drive(t.Context(), op.ID))

	got, err := store.Get(op.ID)
	require.NoError(t, err)
	require.Equal(t, serverlifecycle.PhaseSucceeded, got.Phase)
	require.Equal(t, "inst-2", got.DrivenBy)
	require.Empty(t, got.Nodes)
	require.Equal(t, 0, fleet.dials)
}

func TestWalkLeavesFinishedOperationsAlone(t *testing.T) {
	fleet := newFakeFleet()
	fleet.add("a-runner", "runner", "v1.0.0", compute_v1alpha.READY)
	u, store := newTestUpgrader(t, fleet)
	op := serverlifecycle.NewOperation(serverlifecycle.ActionUpgrade, "test")
	op.TargetVersion = "latest"
	op.Phase = serverlifecycle.PhaseSucceeded
	require.NoError(t, store.Create(op))

	require.NoError(t, u.drive(t.Context(), op.ID))
	got, err := store.Get(op.ID)
	require.NoError(t, err)
	require.Empty(t, got.DrivenBy)
	require.Equal(t, 0, fleet.dials)
}

// The full loop: the watcher sees the executor's hand-off land on disk and
// the upgrader picks it up, including one the executor is still holding.
func TestRunAdoptsHandedOffOperationsAsTheyAppear(t *testing.T) {
	fleet := newFakeFleet()
	fleet.add("a-runner", "runner", "v1.0.0", compute_v1alpha.READY)
	u, store := newTestUpgrader(t, fleet)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go func() { _ = u.watcher.Run(ctx) }()
	done := make(chan struct{})
	go func() { _ = u.Run(ctx); close(done) }()

	// An executor writes the hand-off while still holding the lock, then
	// lets go a moment later.
	op := handedOff(t, store)
	unlock, err := store.LockOperation(op.ID)
	require.NoError(t, err)
	u.watcher.Kick()
	time.Sleep(20 * time.Millisecond)
	unlock()

	require.Eventually(t, func() bool {
		got, err := store.Get(op.ID)
		return err == nil && got.Phase == serverlifecycle.PhaseSucceeded
	}, 5*time.Second, 10*time.Millisecond)
	got, err := store.Get(op.ID)
	require.NoError(t, err)
	require.Equal(t, "inst-2", got.DrivenBy)
	require.Len(t, got.Nodes, 1)

	cancel()
	<-done
}

// A resumed step is not eligible for the shortcuts a fresh one gets: its
// runner may be restarting on our account, and a cordon we left must come
// off even when there is nothing left to do.
func TestResumedStepIsNotSkippedWhileItsRunnerRestarts(t *testing.T) {
	fleet := newFakeFleet()
	node := fleet.add("a-runner", "runner", "v1.0.0", compute_v1alpha.NodeStatus(""))
	node.Scheduling = compute_v1alpha.CORDONED
	u, store := newTestUpgrader(t, fleet)
	op := handedOff(t, store)
	op.DrivenBy = "inst-1"
	started := time.Now().UTC()
	op.Nodes = []*serverlifecycle.NodeStep{
		{Name: "a-runner", RunnerID: "rid-a-runner", Phase: serverlifecycle.PhaseRestarting, OperationID: "01AAAAAAAAAAAAAAAAAAAAAAAA", Cordoned: true, StartedAt: &started},
	}
	require.NoError(t, store.Update(op))
	fleet.ops["01AAAAAAAAAAAAAAAAAAAAAAAA"] = &serverlifecycle.Operation{
		ID: "01AAAAAAAAAAAAAAAAAAAAAAAA", Action: serverlifecycle.ActionUpgrade, TargetVersion: "v2.0.0",
		Phase: serverlifecycle.PhaseRestarting, PreviousVersion: "v1.0.0",
	}
	// Unreachable for longer than the not-ready grace, then back.
	fleet.unreachable["rid-a-runner"] = true
	go func() {
		time.Sleep(400 * time.Millisecond)
		fleet.mu.Lock()
		fleet.unreachable["rid-a-runner"] = false
		fleet.mu.Unlock()
	}()

	require.NoError(t, u.drive(t.Context(), op.ID))

	got, err := store.Get(op.ID)
	require.NoError(t, err)
	require.Equal(t, serverlifecycle.PhaseSucceeded, got.Phase, got.Error)
	step := got.Nodes[0]
	require.Equal(t, serverlifecycle.PhaseSucceeded, step.Phase, step.Error)
	require.Equal(t, "01AAAAAAAAAAAAAAAAAAAAAAAA", step.OperationID, "the same runner operation was followed")
	require.False(t, step.Cordoned)
	require.Equal(t, compute_v1alpha.SCHEDULABLE, fleet.nodes["rid-a-runner"].Scheduling)
}

func TestResumedStepUncordonsARunnerThatAlreadyFinished(t *testing.T) {
	fleet := newFakeFleet()
	node := fleet.add("a-runner", "runner", "v2.0.0", compute_v1alpha.READY)
	node.Scheduling = compute_v1alpha.CORDONED
	u, store := newTestUpgrader(t, fleet)
	op := handedOff(t, store)
	op.DrivenBy = "inst-1"
	started := time.Now().UTC()
	op.Nodes = []*serverlifecycle.NodeStep{
		{Name: "a-runner", RunnerID: "rid-a-runner", Phase: serverlifecycle.PhaseVerifying, OperationID: "01AAAAAAAAAAAAAAAAAAAAAAAA", Cordoned: true, StartedAt: &started},
	}
	require.NoError(t, store.Update(op))
	fleet.ops["01AAAAAAAAAAAAAAAAAAAAAAAA"] = &serverlifecycle.Operation{
		ID: "01AAAAAAAAAAAAAAAAAAAAAAAA", Action: serverlifecycle.ActionUpgrade, TargetVersion: "v2.0.0",
		Phase: serverlifecycle.PhaseSucceeded, PreviousVersion: "v1.0.0", NewVersion: "v2.0.0",
	}

	require.NoError(t, u.drive(t.Context(), op.ID))

	got, err := store.Get(op.ID)
	require.NoError(t, err)
	require.Equal(t, serverlifecycle.PhaseSucceeded, got.Phase, got.Error)
	require.Equal(t, serverlifecycle.PhaseSucceeded, got.Nodes[0].Phase)
	require.False(t, got.Nodes[0].Cordoned)
	require.Equal(t, compute_v1alpha.SCHEDULABLE, fleet.nodes["rid-a-runner"].Scheduling)
	require.Equal(t, []string{"a-runner:schedulable"}, fleet.cordons)
}

func TestWalkPinsRunnersToTheResolvedRelease(t *testing.T) {
	fleet := newFakeFleet()
	fleet.add("a-runner", "runner", "v1.0.0", compute_v1alpha.READY)
	u, store := newTestUpgrader(t, fleet)
	op := handedOff(t, store) // TargetVersion "latest", ResolvedVersion "v2.0.0"

	require.NoError(t, u.drive(t.Context(), op.ID))
	require.Equal(t, []string{"v2.0.0"}, fleet.requests, "the runner installs what the coordinator resolved, not the channel")

	// A main build has no downloadable resolved name; the channel goes through.
	fleet2 := newFakeFleet()
	fleet2.add("a-runner", "runner", "main:abc1234", compute_v1alpha.READY)
	u2, store2 := newTestUpgrader(t, fleet2)
	main := handedOff(t, store2)
	main.TargetVersion, main.ResolvedVersion, main.NewVersion = "main", "main:def5678", "main:def5678"
	require.NoError(t, store2.Update(main))
	require.NoError(t, u2.drive(t.Context(), main.ID))
	require.Equal(t, []string{"main"}, fleet2.requests)
}

func TestWalkFailsARunnerThatLandsOnADifferentBuild(t *testing.T) {
	fleet := newFakeFleet()
	fleet.add("a-runner", "runner", "v1.0.0", compute_v1alpha.READY)
	u, store := newTestUpgrader(t, fleet)
	op := handedOff(t, store)
	// The coordinator is on v2.0.0; the fake runner comes back on v2.0.0 too,
	// so pretend the coordinator's build is something else.
	op.NewVersion = "v2.0.1"
	require.NoError(t, store.Update(op))

	require.NoError(t, u.drive(t.Context(), op.ID))

	got, err := store.Get(op.ID)
	require.NoError(t, err)
	require.Equal(t, serverlifecycle.PhaseFailed, got.Phase)
	require.Contains(t, got.Nodes[0].Error, "upgraded to v2.0.1 but is ready on v2.0.0")
}

// An inventory that stops answering mid-walk is not a step outcome: the walk
// retries, resumes from its checkpoints, and finishes once it is back.
func TestWalkRetriesThroughATransientInventoryError(t *testing.T) {
	fleet := newFakeFleet()
	fleet.add("a-runner", "runner", "v1.0.0", compute_v1alpha.READY)
	fleet.add("b-runner", "runner", "v1.0.0", compute_v1alpha.READY)
	u, store := newTestUpgrader(t, fleet)
	op := handedOff(t, store)
	// The first attempt lists the runners, then the inventory goes away
	// for a few reads.
	fleet.inventoryErrors = 3

	require.NoError(t, u.driveWithRetry(t.Context(), op.ID))

	got, err := store.Get(op.ID)
	require.NoError(t, err)
	require.Equal(t, serverlifecycle.PhaseSucceeded, got.Phase, got.Error)
	require.Equal(t, serverlifecycle.PhaseSucceeded, got.Nodes[0].Phase)
	require.Equal(t, serverlifecycle.PhaseSucceeded, got.Nodes[1].Phase)
}

// The inventory being away at adoption, before the runners are even
// listed, is the same transient error as one mid-walk.
func TestWalkRetriesWhenTheInventoryIsAwayAtAdoption(t *testing.T) {
	fleet := newFakeFleet()
	fleet.add("a-runner", "runner", "v1.0.0", compute_v1alpha.READY)
	u, store := newTestUpgrader(t, fleet)
	op := handedOff(t, store)
	fleet.inventoryErrors = 2

	require.NoError(t, u.driveWithRetry(t.Context(), op.ID))

	got, err := store.Get(op.ID)
	require.NoError(t, err)
	require.Equal(t, serverlifecycle.PhaseSucceeded, got.Phase, got.Error)
	require.Len(t, got.Nodes, 1)
	require.Equal(t, serverlifecycle.PhaseSucceeded, got.Nodes[0].Phase)
}

// An error that never clears fails the operation on the record rather than
// leaving it handed off and holding the busy slot.
func TestWalkFailsTheOperationWhenErrorsOutlastTheRetries(t *testing.T) {
	fleet := newFakeFleet()
	fleet.add("a-runner", "runner", "v1.0.0", compute_v1alpha.READY)
	u, store := newTestUpgrader(t, fleet)
	op := handedOff(t, store)
	fleet.inventoryErrors = 1 << 30

	require.NoError(t, u.driveWithRetry(t.Context(), op.ID))

	got, err := store.Get(op.ID)
	require.NoError(t, err)
	require.Equal(t, serverlifecycle.PhaseFailed, got.Phase)
	require.Contains(t, got.Error, "gave up walking the runners after 2s of errors: list runners: entity server unavailable")
	require.NotNil(t, got.FinishedAt)
}

// Giving up is itself a ledger write, and the ledger can be unavailable at
// that moment too. The settlement keeps trying rather than leaving the
// record handed off: here the lock is what stands in the way.
func TestWalkKeepsTryingToRecordThatItGaveUp(t *testing.T) {
	fleet := newFakeFleet()
	fleet.add("a-runner", "runner", "v1.0.0", compute_v1alpha.READY)
	u, store := newTestUpgrader(t, fleet)
	op := handedOff(t, store)
	fleet.inventoryErrors = 1 << 30

	unlock, err := store.LockOperation(op.ID)
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() { done <- u.driveWithRetry(t.Context(), op.ID) }()

	// Past the retry deadline the walk has given up, but cannot say so.
	time.Sleep(u.opts.RetryFor + 500*time.Millisecond)
	select {
	case err := <-done:
		t.Fatalf("walk returned %v while the record was locked", err)
	default:
	}
	got, err := store.Get(op.ID)
	require.NoError(t, err)
	require.Equal(t, serverlifecycle.PhaseUpgradingRunners, got.Phase)

	unlock()
	require.NoError(t, <-done)
	got, err = store.Get(op.ID)
	require.NoError(t, err)
	require.Equal(t, serverlifecycle.PhaseFailed, got.Phase)
	require.Contains(t, got.Error, "gave up walking the runners")
}

// Shutdown ends the settlement attempts; the next instance adopts the
// record and tries again.
func TestWalkStopsTryingToSettleOnShutdown(t *testing.T) {
	fleet := newFakeFleet()
	fleet.add("a-runner", "runner", "v1.0.0", compute_v1alpha.READY)
	u, store := newTestUpgrader(t, fleet)
	op := handedOff(t, store)
	fleet.inventoryErrors = 1 << 30

	unlock, err := store.LockOperation(op.ID)
	require.NoError(t, err)
	defer unlock()
	ctx, cancel := context.WithTimeout(t.Context(), u.opts.RetryFor+500*time.Millisecond)
	defer cancel()

	require.ErrorIs(t, u.driveWithRetry(ctx, op.ID), context.DeadlineExceeded)
	got, err := store.Get(op.ID)
	require.NoError(t, err)
	require.Equal(t, serverlifecycle.PhaseUpgradingRunners, got.Phase, "left for the next instance")
}
