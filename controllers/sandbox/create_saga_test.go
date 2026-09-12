package sandbox

import (
	"context"
	"fmt"
	"log/slog"
	"net/netip"
	"sync"
	"syscall"
	"testing"
	"time"

	apitypes "github.com/containerd/containerd/api/types"
	"github.com/containerd/containerd/namespaces"
	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/core/containers"
	"github.com/containerd/containerd/v2/pkg/cio"
	"github.com/containerd/containerd/v2/pkg/oci"
	"github.com/containerd/errdefs"
	typeurl "github.com/containerd/typeurl/v2"
	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	compute "miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/network"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/types"
	"miren.dev/runtime/pkg/saga"
)

// =============================================================================
// Mock domain interfaces
// =============================================================================

type mockEntityStore struct {
	mu       sync.Mutex
	sandbox  *compute.Sandbox
	meta     *entity.Meta
	getCalls int
	// patchCalls tracks each PatchSandbox call's attrs
	patchCalls [][]entity.Attr
	getErr     error
	patchErr   error
	// getSandboxFunc allows overriding GetSandbox behavior per-call
	getSandboxFunc func(ctx context.Context, id string) (*compute.Sandbox, *entity.Meta, error)
}

func (m *mockEntityStore) GetSandbox(ctx context.Context, id string) (*compute.Sandbox, *entity.Meta, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.getCalls++
	if m.getSandboxFunc != nil {
		return m.getSandboxFunc(ctx, id)
	}
	if m.getErr != nil {
		return nil, nil, m.getErr
	}
	sb := *m.sandbox
	meta := *m.meta
	return &sb, &meta, nil
}

func (m *mockEntityStore) PatchSandbox(ctx context.Context, attrs []entity.Attr, revision int64) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.patchErr != nil {
		return 0, m.patchErr
	}
	m.patchCalls = append(m.patchCalls, attrs)
	return revision + 1, nil
}

type mockNetworking struct {
	mu            sync.Mutex
	allocateCalls int
	releaseCalls  int
	rebuildCalls  int
	bridgeCalls   int
	allocateErr   error
	releaseErr    error
	rebuildErr    error
	releasedAddrs []netip.Addr
}

func (m *mockNetworking) AllocateNetwork(ctx context.Context, sb *compute.Sandbox) (*network.EndpointConfig, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.allocateCalls++
	if m.allocateErr != nil {
		return nil, m.allocateErr
	}
	return &network.EndpointConfig{
		Addresses: []netip.Prefix{netip.MustParsePrefix("10.0.0.5/32")},
	}, nil
}

func (m *mockNetworking) ReleaseAddr(addr netip.Addr) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.releaseCalls++
	m.releasedAddrs = append(m.releasedAddrs, addr)
	return m.releaseErr
}

func (m *mockNetworking) RebuildEndpointConfig(addresses []string) (*network.EndpointConfig, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rebuildCalls++
	if m.rebuildErr != nil {
		return nil, m.rebuildErr
	}
	var addrs []netip.Prefix
	for _, a := range addresses {
		p, err := netip.ParsePrefix(a)
		if err != nil {
			return nil, err
		}
		addrs = append(addrs, p)
	}
	return &network.EndpointConfig{Addresses: addrs}, nil
}

func (m *mockNetworking) BridgeName() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.bridgeCalls++
	return "br0"
}

type mockContainerRuntime struct {
	mu                       sync.Mutex
	buildSpecCalls           int
	createContainerCalls     int
	loadContainerCalls       int
	cleanupContainerCalls    int
	bootInitialTaskCalls     int
	configureVolumesCalls    int
	bootContainersCalls      int
	destroySubCtrsCalls      int
	releaseDiskLeasesCalls   int
	releaseSqliteDisksCalls  int
	releaseTokenStateCalls   int
	releaseHubsCalls         int
	unconfigureFirewallCalls int
	waitForPortCalls         int
	lastWaitForPortTimeout   time.Duration
	diagnoseListeningCalls   int
	diagnoseRoutable         []int
	diagnoseLoopback         []int
	diagnoseOK               bool

	buildSpecErr         error
	createContainerErr   error
	loadContainerErr     error
	bootInitialTaskErr   error
	configureVolumesErr  error
	bootContainersErr    error
	destroySubCtrsErr    error
	releaseDiskLeasesErr error
	waitForPortErr       error

	// loadContainerErrFunc allows per-call error control (e.g. succeed on first, ErrNotFound on second)
	loadContainerErrFunc func(call int) error

	mockContainer *sagaMockContainer
	mockTask      *sagaMockTask
}

func (m *mockContainerRuntime) BuildSpec(ctx context.Context, sb *compute.Sandbox, ep *network.EndpointConfig, meta *entity.Meta) ([]containerd.NewContainerOpts, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.buildSpecCalls++
	if m.buildSpecErr != nil {
		return nil, m.buildSpecErr
	}
	return []containerd.NewContainerOpts{}, nil
}

func (m *mockContainerRuntime) CreateContainer(ctx context.Context, id string, opts ...containerd.NewContainerOpts) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.createContainerCalls++
	if m.createContainerErr != nil {
		return "", m.createContainerErr
	}
	return id, nil
}

// ContainerSpec mirrors sandboxOps: the namespace is the adapter's job to
// supply, not the saga action's.
func (m *mockContainerRuntime) ContainerSpec(ctx context.Context, cont containerd.Container) (*oci.Spec, error) {
	return cont.Spec(namespaces.WithNamespace(ctx, "miren-test"))
}

func (m *mockContainerRuntime) LoadContainer(ctx context.Context, id string) (containerd.Container, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.loadContainerCalls++
	if m.loadContainerErrFunc != nil {
		if err := m.loadContainerErrFunc(m.loadContainerCalls); err != nil {
			return nil, err
		}
	}
	if m.loadContainerErr != nil {
		return nil, m.loadContainerErr
	}
	return m.mockContainer, nil
}

func (m *mockContainerRuntime) CleanupContainer(ctx context.Context, cont containerd.Container) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cleanupContainerCalls++
}

func (m *mockContainerRuntime) BootInitialTask(ctx context.Context, sb *compute.Sandbox, ep *network.EndpointConfig, container containerd.Container, shortID string) (containerd.Task, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.bootInitialTaskCalls++
	if m.bootInitialTaskErr != nil {
		return nil, m.bootInitialTaskErr
	}
	return m.mockTask, nil
}

func (m *mockContainerRuntime) ConfigureVolumes(ctx context.Context, sb *compute.Sandbox, meta *entity.Meta) (map[string]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.configureVolumesCalls++
	if m.configureVolumesErr != nil {
		return nil, m.configureVolumesErr
	}
	return map[string]string{}, nil
}

func (m *mockContainerRuntime) BootContainers(ctx context.Context, sb *compute.Sandbox, ep *network.EndpointConfig, sbPid int, cgroups map[string]string, meta *entity.Meta, volumeMounts map[string]string) ([]WaitPort, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.bootContainersCalls++
	if m.bootContainersErr != nil {
		return nil, m.bootContainersErr
	}
	return []WaitPort{{ID: "web", Port: 8080}}, nil
}

func (m *mockContainerRuntime) DestroySubContainers(ctx context.Context, id entity.Id) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.destroySubCtrsCalls++
	return m.destroySubCtrsErr
}

func (m *mockContainerRuntime) ReleaseDiskLeases(ctx context.Context, sandboxID entity.Id) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.releaseDiskLeasesCalls++
	return m.releaseDiskLeasesErr
}

func (m *mockContainerRuntime) ReleaseSqliteDisks(ctx context.Context, sandboxID entity.Id) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.releaseSqliteDisksCalls++
}

func (m *mockContainerRuntime) ReleaseTokenState(id entity.Id) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.releaseTokenStateCalls++
}

func (m *mockContainerRuntime) ReleaseHubs(id entity.Id) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.releaseHubsCalls++
}

func (m *mockContainerRuntime) UnconfigureFirewall(sb *compute.Sandbox) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.unconfigureFirewallCalls++
}

func (m *mockContainerRuntime) WaitForPort(ctx context.Context, id string, port int, timeout time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.waitForPortCalls++
	m.lastWaitForPortTimeout = timeout
	return m.waitForPortErr
}

func (m *mockContainerRuntime) DiagnoseListening(id string) (routable []int, loopback []int, ok bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.diagnoseListeningCalls++
	return m.diagnoseRoutable, m.diagnoseLoopback, m.diagnoseOK
}

type mockObservability struct {
	mu                 sync.Mutex
	addMetricsCalls    int
	removeMetricsCalls int
	updateSvcsCalls    int
	addMetricsErr      error
	updateSvcsErr      error

	lastLogEntity string
	lastCgroups   map[string]string
	lastAttrs     map[string]string

	loggedEvents []string
}

func (m *mockObservability) AddMetrics(logEntity string, cgroups map[string]string, attrs map[string]string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.addMetricsCalls++
	m.lastLogEntity = logEntity
	m.lastCgroups = cgroups
	m.lastAttrs = attrs
	if m.addMetricsErr != nil {
		return m.addMetricsErr
	}
	return nil
}

func (m *mockObservability) RemoveMetrics(logEntity string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.removeMetricsCalls++
}

func (m *mockObservability) UpdateServices(ctx context.Context, co *compute.Sandbox, meta *entity.Meta, ep *network.EndpointConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.updateSvcsCalls++
	return m.updateSvcsErr
}

func (m *mockObservability) LogSandboxEvent(sb *compute.Sandbox, shortID, line string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.loggedEvents = append(m.loggedEvents, line)
}

// =============================================================================
// Containerd mock types
// =============================================================================

type sagaMockContainer struct {
	id     string
	spec   *oci.Spec
	taskFn func(context.Context, cio.Attach) (containerd.Task, error)

	// specNamespace records the containerd namespace the last Spec call
	// arrived with, so tests can assert the caller supplied one.
	specNamespace string
}

func (c *sagaMockContainer) ID() string { return c.id }
func (c *sagaMockContainer) Info(context.Context, ...containerd.InfoOpts) (containers.Container, error) {
	return containers.Container{}, nil
}
func (c *sagaMockContainer) Delete(context.Context, ...containerd.DeleteOpts) error { return nil }
func (c *sagaMockContainer) NewTask(context.Context, cio.Creator, ...containerd.NewTaskOpts) (containerd.Task, error) {
	return nil, nil
}

// Spec refuses an un-namespaced context the way containerd does, so a caller
// that goes around sandboxOps fails here instead of silently passing on a
// client that happens to have a default namespace configured.
func (c *sagaMockContainer) Spec(ctx context.Context) (*oci.Spec, error) {
	ns, ok := namespaces.Namespace(ctx)
	if !ok {
		return nil, fmt.Errorf("namespace is required: %w", errdefs.ErrFailedPrecondition)
	}
	c.specNamespace = ns
	return c.spec, nil
}
func (c *sagaMockContainer) Task(ctx context.Context, attach cio.Attach) (containerd.Task, error) {
	if c.taskFn != nil {
		return c.taskFn(ctx, attach)
	}
	return nil, errdefs.ErrNotFound
}
func (c *sagaMockContainer) Image(context.Context) (containerd.Image, error)   { return nil, nil }
func (c *sagaMockContainer) Labels(context.Context) (map[string]string, error) { return nil, nil }
func (c *sagaMockContainer) SetLabels(context.Context, map[string]string) (map[string]string, error) {
	return nil, nil
}
func (c *sagaMockContainer) Extensions(context.Context) (map[string]typeurl.Any, error) {
	return nil, nil
}
func (c *sagaMockContainer) Update(context.Context, ...containerd.UpdateContainerOpts) error {
	return nil
}
func (c *sagaMockContainer) Checkpoint(context.Context, string, ...containerd.CheckpointOpts) (containerd.Image, error) {
	return nil, nil
}

type sagaMockTask struct {
	pid       uint32
	deleteErr error
}

func (t *sagaMockTask) ID() string                  { return "mock-task" }
func (t *sagaMockTask) Pid() uint32                 { return t.pid }
func (t *sagaMockTask) Start(context.Context) error { return nil }
func (t *sagaMockTask) Delete(_ context.Context, _ ...containerd.ProcessDeleteOpts) (*containerd.ExitStatus, error) {
	if t.deleteErr != nil {
		return nil, t.deleteErr
	}
	return nil, nil
}
func (t *sagaMockTask) Kill(context.Context, syscall.Signal, ...containerd.KillOpts) error {
	return nil
}
func (t *sagaMockTask) Wait(context.Context) (<-chan containerd.ExitStatus, error) {
	return nil, nil
}
func (t *sagaMockTask) CloseIO(context.Context, ...containerd.IOCloserOpts) error { return nil }
func (t *sagaMockTask) Resize(_ context.Context, _, _ uint32) error               { return nil }
func (t *sagaMockTask) IO() cio.IO                                                { return nil }
func (t *sagaMockTask) Status(context.Context) (containerd.Status, error) {
	return containerd.Status{}, nil
}
func (t *sagaMockTask) Pause(context.Context) error  { return nil }
func (t *sagaMockTask) Resume(context.Context) error { return nil }
func (t *sagaMockTask) Exec(context.Context, string, *specs.Process, cio.Creator) (containerd.Process, error) {
	return nil, nil
}
func (t *sagaMockTask) Pids(context.Context) ([]containerd.ProcessInfo, error) { return nil, nil }
func (t *sagaMockTask) Checkpoint(context.Context, ...containerd.CheckpointTaskOpts) (containerd.Image, error) {
	return nil, nil
}
func (t *sagaMockTask) Update(context.Context, ...containerd.UpdateTaskOpts) error { return nil }
func (t *sagaMockTask) LoadProcess(context.Context, string, cio.Attach) (containerd.Process, error) {
	return nil, nil
}
func (t *sagaMockTask) Metrics(context.Context) (*apitypes.Metric, error) { return nil, nil }
func (t *sagaMockTask) Spec(context.Context) (*oci.Spec, error)           { return nil, nil }

// =============================================================================
// Test harness
// =============================================================================

type testHarness struct {
	t          *testing.T
	entities   *mockEntityStore
	networking *mockNetworking
	runtime    *mockContainerRuntime
	obs        *mockObservability
	registry   *saga.Registry
	storage    *saga.MemoryStorage
	executor   *saga.Executor
	sandboxID  string
}

func newTestHarness(t *testing.T) *testHarness {
	t.Helper()

	sandboxID := "test-sandbox-1"

	mockCtr := &sagaMockContainer{
		id: sandboxID,
		spec: &oci.Spec{
			Linux: &specs.Linux{
				CgroupsPath: "/sys/fs/cgroup/sandbox/test-sandbox-1",
			},
		},
	}
	mockTsk := &sagaMockTask{pid: 4242}

	sb := &compute.Sandbox{
		ID:     entity.Id(sandboxID),
		Status: compute.PENDING,
		Spec: compute.SandboxSpec{
			Version: entity.Id("v1"),
		},
	}

	meta := &entity.Meta{
		Entity:   entity.New(entity.Ref(entity.DBId, entity.Id(sandboxID))),
		Revision: 1,
	}

	h := &testHarness{
		t:         t,
		sandboxID: sandboxID,
		entities: &mockEntityStore{
			sandbox: sb,
			meta:    meta,
		},
		networking: &mockNetworking{},
		runtime: &mockContainerRuntime{
			mockContainer: mockCtr,
			mockTask:      mockTsk,
		},
		obs:      &mockObservability{},
		registry: saga.NewRegistry(),
		storage:  saga.NewMemoryStorage(),
	}

	// Register the saga definition
	log := slog.Default()
	err := registerCreateSandboxSaga(h.registry, h.entities, h.networking, h.runtime, h.obs, "node/test-node", log)
	require.NoError(t, err)

	h.executor = saga.NewExecutor(h.storage,
		saga.WithRegistry(h.registry),
		saga.WithLogger(log),
		saga.WithRecoveryScope("node/test-node"),
	)
	return h
}

func (h *testHarness) execute(t *testing.T) error {
	t.Helper()
	ctx := context.Background()
	return h.executor.Start(sagaCreateSandbox).
		Input("sandbox_id", h.sandboxID).
		WithID("test-exec-1").
		Execute(ctx)
}

func (h *testHarness) execution(t *testing.T) *saga.Execution {
	t.Helper()
	exec, err := h.storage.Get(context.Background(), "test-exec-1")
	require.NoError(t, err)
	return exec
}

// =============================================================================
// Tests
// =============================================================================

func TestCreateSandboxSaga_HappyPath(t *testing.T) {
	h := newTestHarness(t)

	err := h.execute(t)
	require.NoError(t, err)

	exec := h.execution(t)
	assert.Equal(t, saga.StatusCompleted, exec.Status)
	assert.Equal(t, "node/test-node", exec.RecoveryScope)

	// All forward actions called
	assert.Equal(t, 1, h.networking.allocateCalls)
	assert.GreaterOrEqual(t, h.entities.getCalls, 1)
	assert.GreaterOrEqual(t, len(h.entities.patchCalls), 1) // patchNetwork + setRunning
	assert.Equal(t, 1, h.runtime.createContainerCalls)
	assert.Equal(t, 1, h.runtime.bootInitialTaskCalls)
	assert.Equal(t, 1, h.runtime.configureVolumesCalls)
	assert.Equal(t, 1, h.runtime.bootContainersCalls)
	assert.Equal(t, 1, h.runtime.waitForPortCalls)
	assert.Equal(t, 1, h.obs.addMetricsCalls)
	assert.Equal(t, 1, h.obs.updateSvcsCalls)

	// No undo actions called
	assert.Equal(t, 0, h.networking.releaseCalls)
	assert.Equal(t, 0, h.runtime.cleanupContainerCalls)
	assert.Equal(t, 0, h.runtime.destroySubCtrsCalls)
	assert.Equal(t, 0, h.obs.removeMetricsCalls)
}

func TestCreateSandboxSaga_AllocNetworkFails(t *testing.T) {
	h := newTestHarness(t)
	h.networking.allocateErr = fmt.Errorf("no IPs available")

	err := h.execute(t)
	require.Error(t, err)

	exec := h.execution(t)
	assert.Equal(t, saga.StatusFailed, exec.Status)

	// Only allocNetwork was attempted
	assert.Equal(t, 1, h.networking.allocateCalls)
	// No later forward actions ran
	assert.Equal(t, 0, h.runtime.createContainerCalls)
	assert.Equal(t, 0, h.runtime.bootInitialTaskCalls)
	// No undos needed (nothing to compensate)
	assert.Equal(t, 0, h.networking.releaseCalls)
}

func TestCreateSandboxSaga_CreateContainerFails(t *testing.T) {
	h := newTestHarness(t)
	h.runtime.createContainerErr = fmt.Errorf("image not found")

	err := h.execute(t)
	require.Error(t, err)

	exec := h.execution(t)
	assert.Equal(t, saga.StatusFailed, exec.Status)

	// allocNetwork and patchNetwork ran before createContainer
	assert.Equal(t, 1, h.networking.allocateCalls)
	assert.GreaterOrEqual(t, len(h.entities.patchCalls), 1)

	// createContainer failed, no later actions
	assert.Equal(t, 1, h.runtime.createContainerCalls)
	assert.Equal(t, 0, h.runtime.bootInitialTaskCalls)

	// Undo: IP released (undoAllocNetwork); other undos are no-ops
	assert.Equal(t, 1, h.networking.releaseCalls)
}

func TestCreateSandboxSaga_BootTaskFails(t *testing.T) {
	h := newTestHarness(t)
	h.runtime.bootInitialTaskErr = fmt.Errorf("cgroup limit exceeded")

	err := h.execute(t)
	require.Error(t, err)

	exec := h.execution(t)
	assert.Equal(t, saga.StatusFailed, exec.Status)

	// Container was created, bootTask loaded it then failed
	assert.Equal(t, 1, h.runtime.createContainerCalls)
	assert.GreaterOrEqual(t, h.runtime.loadContainerCalls, 1)

	// bootTask failed so its undo is skipped. undoCreateContainer runs
	// (loads container again + cleans up), and undoAllocNetwork releases IP.
	assert.Equal(t, 1, h.runtime.cleanupContainerCalls)
	assert.Equal(t, 1, h.networking.releaseCalls)
}

func TestCreateSandboxSaga_BootContainersFails(t *testing.T) {
	h := newTestHarness(t)
	h.runtime.bootContainersErr = fmt.Errorf("port already in use")

	// Set up container mock's Task() to return the mock task for undo
	h.runtime.mockContainer.taskFn = func(ctx context.Context, attach cio.Attach) (containerd.Task, error) {
		return h.runtime.mockTask, nil
	}

	err := h.execute(t)
	require.Error(t, err)

	exec := h.execution(t)
	assert.Equal(t, saga.StatusFailed, exec.Status)

	// undoBootTask: firewall unconfigured, task killed
	assert.Equal(t, 1, h.runtime.unconfigureFirewallCalls)

	// undoCreateContainer: container cleaned up
	assert.Equal(t, 1, h.runtime.cleanupContainerCalls)

	// undoAllocNetwork: IP released
	assert.Equal(t, 1, h.networking.releaseCalls)
}

// TestCreateSandboxSaga_ConfigureVolumesFailsLogsEvent verifies that when
// ConfigureVolumes returns an error, the saga logs a synthetic event on
// the sandbox's log stream so operators can see the reason via
// `miren logs sandbox <id>`. Without this, volume-stage failures never
// surface to the user without ssh access to the node.
func TestCreateSandboxSaga_ConfigureVolumesFailsLogsEvent(t *testing.T) {
	h := newTestHarness(t)
	h.runtime.configureVolumesErr = fmt.Errorf("disk image /foo/disk.img is already attached to /dev/loop5")

	// Set up container Task() for undo cleanup.
	h.runtime.mockContainer.taskFn = func(ctx context.Context, attach cio.Attach) (containerd.Task, error) {
		return h.runtime.mockTask, nil
	}

	err := h.execute(t)
	require.Error(t, err)

	exec := h.execution(t)
	assert.Equal(t, saga.StatusFailed, exec.Status)
	assert.Equal(t, 1, h.runtime.configureVolumesCalls)

	// A lifecycle event was logged on the sandbox's log stream.
	require.Len(t, h.obs.loggedEvents, 1)
	assert.Contains(t, h.obs.loggedEvents[0], "volume configuration failed")
	assert.Contains(t, h.obs.loggedEvents[0], "already attached")
}

func TestCreateSandboxSaga_WaitPortsFails(t *testing.T) {
	h := newTestHarness(t)
	// Configured port times out, and the diagnosis finds nothing listening.
	h.runtime.waitForPortErr = fmt.Errorf("timeout waiting for port")

	// Set up container Task() for undo
	h.runtime.mockContainer.taskFn = func(ctx context.Context, attach cio.Attach) (containerd.Task, error) {
		return h.runtime.mockTask, nil
	}

	err := h.execute(t)
	require.Error(t, err)

	exec := h.execution(t)
	assert.Equal(t, saga.StatusFailed, exec.Status)

	// All prior forward actions up through wait-ports completed;
	// set-running and update-services depend on wait-ports via Edge
	// so they did NOT run before wait-ports failed.
	assert.Equal(t, 1, h.networking.allocateCalls)
	assert.Equal(t, 1, h.runtime.createContainerCalls)
	assert.Equal(t, 1, h.runtime.bootInitialTaskCalls)
	assert.Equal(t, 1, h.runtime.bootContainersCalls)
	assert.Equal(t, 1, h.obs.addMetricsCalls)
	assert.Equal(t, 0, h.obs.updateSvcsCalls, "update-services should not run before wait-ports")

	// Undos run in reverse: metrics removed, subcontainers destroyed,
	// task killed, container cleaned, IP released (plus no-op undos for patchNetwork)
	assert.Equal(t, 1, h.obs.removeMetricsCalls)
	assert.Equal(t, 1, h.runtime.destroySubCtrsCalls)
	assert.Equal(t, 1, h.runtime.releaseDiskLeasesCalls)
	assert.Equal(t, 1, h.runtime.releaseTokenStateCalls,
		"undoing boot-containers must release the token state it registered")
	assert.Equal(t, 1, h.runtime.releaseSqliteDisksCalls,
		"undoing create must stop replicating the sqlite disks ConfigureVolumes registered")
	assert.Equal(t, 1, h.runtime.unconfigureFirewallCalls)
	assert.Equal(t, 1, h.runtime.cleanupContainerCalls)
	assert.Equal(t, 1, h.networking.releaseCalls)
}

func TestSingleAlternativePort(t *testing.T) {
	// Exactly one routable port that isn't the configured one -> auto-routable.
	alt, ok := singleAlternativePort([]int{3000}, 8080)
	assert.True(t, ok)
	assert.Equal(t, 3000, alt)

	// The configured port itself is filtered out before counting candidates.
	_, ok = singleAlternativePort([]int{8080}, 8080)
	assert.False(t, ok)

	// Ambiguous: more than one alternative.
	_, ok = singleAlternativePort([]int{3000, 9090}, 8080)
	assert.False(t, ok)

	// Nothing routable.
	_, ok = singleAlternativePort(nil, 8080)
	assert.False(t, ok)
}

func TestDescribePortFailure(t *testing.T) {
	// A different routable port -> name it and point at the config knobs.
	msg := describePortFailure(3000, []int{8080, 9090}, nil)
	assert.Contains(t, msg, ":8080, :9090")
	assert.Contains(t, msg, "expects :3000")
	assert.Contains(t, msg, "$PORT")

	// Loopback-only bind -> tell them to bind 0.0.0.0.
	msg = describePortFailure(3000, nil, []int{8080})
	assert.Contains(t, msg, "loopback only")
	assert.Contains(t, msg, "0.0.0.0")

	// Nothing listening at all -> the plain timeout story.
	msg = describePortFailure(3000, nil, nil)
	assert.Contains(t, msg, "nothing is listening on :3000")
}

func TestCreateSandboxSaga_WaitPortAutoRoutesToObservedPort(t *testing.T) {
	h := newTestHarness(t)
	// Configured port (8080, from the mock BootContainers) never binds, but the
	// diagnosis finds the app on a single routable alternative.
	h.runtime.waitForPortErr = fmt.Errorf("timeout waiting for port 8080")
	h.runtime.diagnoseOK = true
	h.runtime.diagnoseRoutable = []int{3000}

	err := h.execute(t)
	require.NoError(t, err)

	exec := h.execution(t)
	assert.Equal(t, saga.StatusCompleted, exec.Status)
	assert.Equal(t, 1, h.runtime.diagnoseListeningCalls)
	assert.Contains(t, exec.ExecutionOrder, actionSetRunning)

	// The observed port should be persisted as a bound_port component so the
	// activator can route to it.
	boundPortFound := false
	for _, attrs := range h.entities.patchCalls {
		for _, attr := range attrs {
			if attr.ID == compute.SandboxBoundPortId {
				boundPortFound = true
			}
		}
	}
	assert.True(t, boundPortFound, "observed alternative port should be persisted as bound_port")
}

func TestCreateSandboxSaga_WaitPortAmbiguousFails(t *testing.T) {
	h := newTestHarness(t)
	// Configured port times out and the diagnosis finds two routable
	// alternatives: Miren can't guess which, so it must fail.
	h.runtime.waitForPortErr = fmt.Errorf("timeout waiting for port 8080")
	h.runtime.diagnoseOK = true
	h.runtime.diagnoseRoutable = []int{3000, 9090}

	// Task() needed for the undo path.
	h.runtime.mockContainer.taskFn = func(ctx context.Context, attach cio.Attach) (containerd.Task, error) {
		return h.runtime.mockTask, nil
	}

	err := h.execute(t)
	require.Error(t, err)

	exec := h.execution(t)
	assert.Equal(t, saga.StatusFailed, exec.Status)
	assert.Equal(t, 1, h.runtime.diagnoseListeningCalls)
	assert.Equal(t, 1, h.runtime.destroySubCtrsCalls)
}

func TestCreateSandboxSaga_PortWaitTimeoutDefault(t *testing.T) {
	h := newTestHarness(t)

	err := h.execute(t)
	require.NoError(t, err)

	assert.Equal(t, 1, h.runtime.waitForPortCalls)
	assert.Equal(t, defaultPortWaitTimeout, h.runtime.lastWaitForPortTimeout,
		"empty PortWaitTimeout should resolve to default")
}

func TestCreateSandboxSaga_PortWaitTimeoutOverride(t *testing.T) {
	h := newTestHarness(t)
	h.entities.sandbox.Spec.PortWaitTimeout = "30s"

	err := h.execute(t)
	require.NoError(t, err)

	assert.Equal(t, 1, h.runtime.waitForPortCalls)
	assert.Equal(t, 30*time.Second, h.runtime.lastWaitForPortTimeout,
		"spec.PortWaitTimeout should override the default")
}

func TestCreateSandboxSaga_UndoContainerAlreadyGone(t *testing.T) {
	h := newTestHarness(t)
	h.runtime.bootInitialTaskErr = fmt.Errorf("task boot failed")

	// LoadContainer succeeds on first call (forward bootTask), returns NotFound on second (undo)
	h.runtime.loadContainerErrFunc = func(call int) error {
		if call >= 2 {
			return errdefs.ErrNotFound
		}
		return nil
	}

	err := h.execute(t)
	require.Error(t, err)

	exec := h.execution(t)
	assert.Equal(t, saga.StatusFailed, exec.Status)

	// undoCreateContainer ran but container was not found — should not panic
	// IP still released
	assert.Equal(t, 1, h.networking.releaseCalls)
	// CleanupContainer was not called since container was not found
	assert.Equal(t, 0, h.runtime.cleanupContainerCalls)
}

func TestCreateSandboxSaga_UndoTaskAlreadyGone(t *testing.T) {
	h := newTestHarness(t)
	h.runtime.bootContainersErr = fmt.Errorf("containers failed")

	// Task() returns NotFound during undo (task already gone)
	h.runtime.mockContainer.taskFn = func(ctx context.Context, attach cio.Attach) (containerd.Task, error) {
		return nil, errdefs.ErrNotFound
	}

	err := h.execute(t)
	require.Error(t, err)

	exec := h.execution(t)
	assert.Equal(t, saga.StatusFailed, exec.Status)

	// undoBootTask ran: firewall still unconfigured even though task not found
	assert.Equal(t, 1, h.runtime.unconfigureFirewallCalls)

	// Other undos still ran
	assert.Equal(t, 1, h.runtime.cleanupContainerCalls)
	assert.Equal(t, 1, h.networking.releaseCalls)
}

func TestCreateSandboxSaga_SetRunningSkipsTerminalStatus(t *testing.T) {
	h := newTestHarness(t)

	// Sandbox is already DEAD — setRunning should skip the RUNNING patch
	h.entities.sandbox.Status = compute.DEAD

	err := h.execute(t)
	require.NoError(t, err)

	exec := h.execution(t)
	assert.Equal(t, saga.StatusCompleted, exec.Status)

	// Verify setRunning actually executed (it's in the execution log)
	assert.Contains(t, exec.ExecutionOrder, actionSetRunning,
		"setRunning should have executed")

	// Check that no RUNNING status patch was issued.
	// patchNetwork always runs; setRunning should have skipped the patch
	// because the sandbox was in terminal DEAD state.
	runningPatchFound := false
	for _, attrs := range h.entities.patchCalls {
		for _, attr := range attrs {
			if attr.ID == compute.SandboxStatusRunningId {
				runningPatchFound = true
			}
		}
	}
	assert.False(t, runningPatchFound, "should not have patched RUNNING status when sandbox is DEAD")
}

func TestCreateSandboxSaga_MetricsAttributes(t *testing.T) {
	h := newTestHarness(t)

	// Configure sandbox with specific log attributes
	h.entities.sandbox.Spec.LogEntity = "my-app"
	h.entities.sandbox.Spec.Version = entity.Id("v42")
	h.entities.sandbox.Spec.LogAttribute = types.Labels{
		{Key: "env", Value: "production"},
		{Key: "region", Value: "us-east"},
	}

	err := h.execute(t)
	require.NoError(t, err)

	assert.Equal(t, 1, h.obs.addMetricsCalls)
	assert.Equal(t, "my-app", h.obs.lastLogEntity)
	assert.Equal(t, h.sandboxID, h.obs.lastAttrs["miren.sandbox"])
	assert.Equal(t, "v42", h.obs.lastAttrs["miren.version"])
	assert.Equal(t, "production", h.obs.lastAttrs["env"])
	assert.Equal(t, "us-east", h.obs.lastAttrs["region"])

	// Verify cgroups map includes root cgroup
	assert.Contains(t, h.obs.lastCgroups, "")
	assert.Equal(t, "/sys/fs/cgroup/sandbox/test-sandbox-1", h.obs.lastCgroups[""])
}
