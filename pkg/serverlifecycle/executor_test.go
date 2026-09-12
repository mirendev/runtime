package serverlifecycle

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"miren.dev/runtime/pkg/release"
)

// fakeHost is the machine: the binary on disk, the running process, and what
// systemd restart does to them.
type fakeHost struct {
	onDisk  release.VersionInfo
	backup  *release.VersionInfo
	running Snapshot

	// bootProbes: "not ready" answers after a restart before flipping ready.
	// failBoot: never ready, like a broken binary.
	bootProbes int
	failBoot   bool
	pending    int

	restarts  int
	installs  int
	rollbacks int
	downloads int
	metadata  map[string]*release.Metadata
	instances int
}

func newFakeHost(version string) *fakeHost {
	return &fakeHost{
		onDisk:  release.VersionInfo{Version: version, Commit: "c-" + version},
		running: Snapshot{InstanceID: "inst-1", Version: version, Commit: "c-" + version, Ready: true, InstallKind: "systemd"},
		metadata: map[string]*release.Metadata{
			"latest": {Version: "v2.0.0", Commit: "c-v2.0.0"},
			"v2.0.0": {Version: "v2.0.0", Commit: "c-v2.0.0"},
			"v1.0.0": {Version: "v1.0.0", Commit: "c-v1.0.0"},
		},
		instances: 1,
	}
}

func (h *fakeHost) Probe(context.Context) (Snapshot, error) {
	if h.pending > 0 {
		h.pending--
		s := h.running
		s.Ready = false
		return s, nil
	}
	if h.failBoot && h.running.InstanceID != "inst-1" {
		return Snapshot{}, errors.New("connection refused")
	}
	return h.running, nil
}

func (h *fakeHost) Restart(context.Context) error {
	h.restarts++
	h.instances++
	h.running = Snapshot{
		InstanceID:  fmt.Sprintf("inst-%d", h.instances),
		Version:     h.onDisk.Version,
		Commit:      h.onDisk.Commit,
		Ready:       true,
		InstallKind: "systemd",
	}
	h.pending = h.bootProbes
	return nil
}

func (h *fakeHost) Install(_ context.Context, d *release.DownloadedArtifact) error {
	h.installs++
	prev := h.onDisk
	h.backup = &prev
	h.onDisk = release.VersionInfo{Version: d.Artifact.Version, Commit: "c-" + d.Artifact.Version}
	return nil
}
func (h *fakeHost) Backup(context.Context) error { return nil }
func (h *fakeHost) Rollback(context.Context) error {
	h.rollbacks++
	if h.backup == nil {
		return errors.New("no backup")
	}
	h.onDisk = *h.backup
	h.backup = nil
	h.failBoot = false
	return nil
}
func (h *fakeHost) GetCurrentVersion(context.Context) (release.VersionInfo, error) {
	return h.onDisk, nil
}
func (h *fakeHost) HasBackup() bool { return h.backup != nil }

func (h *fakeHost) Download(_ context.Context, a release.Artifact, opts release.DownloadOptions) (*release.DownloadedArtifact, error) {
	h.downloads++
	if opts.ProgressWriter != nil {
		if pw, ok := opts.ProgressWriter.(interface{ SetTotal(int64) }); ok {
			pw.SetTotal(100)
		}
		_, _ = opts.ProgressWriter.Write(make([]byte, 100))
	}
	meta := h.metadata[a.Version]
	return &release.DownloadedArtifact{Artifact: release.Artifact{Type: a.Type, Version: meta.Version}, Path: "/tmp/fake"}, nil
}
func (h *fakeHost) GetLatestVersion(context.Context, release.ArtifactType) (string, error) {
	return "v2.0.0", nil
}
func (h *fakeHost) GetVersionMetadata(_ context.Context, v string) (*release.Metadata, error) {
	m, ok := h.metadata[v]
	if !ok {
		return nil, fmt.Errorf("no such version %q", v)
	}
	return m, nil
}

func newTestExecutor(t *testing.T, host *fakeHost) (*Executor, *Store) {
	t.Helper()
	store, err := NewStore(t.TempDir())
	require.NoError(t, err)
	opts := DefaultOptions()
	opts.ReadyTimeout = 200 * time.Millisecond
	opts.ProbeInterval = 5 * time.Millisecond
	opts.PathSymlink = ""
	ex := NewExecutor(store, opts, slog.Default()).
		WithDownloader(host).WithInstaller(host).WithRestarter(host).WithProber(host)
	return ex, store
}

func TestRestartSucceedsOnNewReadyInstance(t *testing.T) {
	host := newFakeHost("v1.0.0")
	host.bootProbes = 3
	ex, store := newTestExecutor(t, host)

	op := NewOperation(ActionRestart, "test")
	require.NoError(t, store.Create(op))

	got, err := ex.Run(context.Background(), op.ID)
	require.NoError(t, err)
	require.Equal(t, PhaseSucceeded, got.Phase, got.Error)
	require.Equal(t, "inst-1", got.PreviousInstanceID)
	require.Equal(t, "inst-2", got.NewInstanceID)
	require.Equal(t, 1, host.restarts)
	require.NotNil(t, got.FinishedAt)
}

func TestUpgradeDownloadsInstallsRestartsVerifies(t *testing.T) {
	host := newFakeHost("v1.0.0")
	ex, store := newTestExecutor(t, host)

	op := NewOperation(ActionUpgrade, "test")
	op.TargetVersion = "latest"
	require.NoError(t, store.Create(op))

	got, err := ex.Run(context.Background(), op.ID)
	require.NoError(t, err)
	require.Equal(t, PhaseSucceeded, got.Phase, got.Error)
	require.Equal(t, "v2.0.0", got.ResolvedVersion)
	require.Equal(t, "v1.0.0", got.PreviousVersion)
	require.Equal(t, "v2.0.0", got.NewVersion)
	require.Equal(t, 1, host.downloads)
	require.Equal(t, 1, host.installs)
	require.Equal(t, 1, host.restarts)
	require.Equal(t, 0, host.rollbacks)
}

func TestUpgradeRollsBackWhenNewBinaryNeverReady(t *testing.T) {
	host := newFakeHost("v1.0.0")
	host.failBoot = true
	ex, store := newTestExecutor(t, host)

	op := NewOperation(ActionUpgrade, "test")
	op.TargetVersion = "v2.0.0"
	require.NoError(t, store.Create(op))

	got, err := ex.Run(context.Background(), op.ID)
	require.NoError(t, err)
	require.Equal(t, PhaseRolledBack, got.Phase)
	require.Contains(t, got.Error, "not ready within")
	require.Equal(t, 1, host.rollbacks)
	require.Equal(t, 2, host.restarts)
	require.Equal(t, "v1.0.0", host.onDisk.Version)
	require.Equal(t, "v1.0.0", got.NewVersion)
	require.NotEqual(t, got.PreviousInstanceID, got.NewInstanceID)
}

func TestUpgradeFailsWithoutRollbackWhenDisabled(t *testing.T) {
	host := newFakeHost("v1.0.0")
	host.failBoot = true
	ex, store := newTestExecutor(t, host)
	ex.opts.AutoRollback = false

	op := NewOperation(ActionUpgrade, "test")
	op.TargetVersion = "v2.0.0"
	require.NoError(t, store.Create(op))

	got, err := ex.Run(context.Background(), op.ID)
	require.NoError(t, err)
	require.Equal(t, PhaseFailed, got.Phase)
	require.Equal(t, 0, host.rollbacks)
	require.Equal(t, "v2.0.0", host.onDisk.Version)
}

func TestUpgradeAlreadyAtTargetDoesNotRestart(t *testing.T) {
	host := newFakeHost("v2.0.0")
	ex, store := newTestExecutor(t, host)

	op := NewOperation(ActionUpgrade, "test")
	op.TargetVersion = "latest"
	require.NoError(t, store.Create(op))

	got, err := ex.Run(context.Background(), op.ID)
	require.NoError(t, err)
	require.Equal(t, PhaseSucceeded, got.Phase)
	require.Equal(t, 0, host.restarts)
	require.Equal(t, 0, host.downloads)
	require.Equal(t, got.PreviousInstanceID, got.NewInstanceID)
}

func TestResumeAfterRestartDoesNotBounceAgain(t *testing.T) {
	host := newFakeHost("v1.0.0")
	ex, store := newTestExecutor(t, host)

	// The executor issued the restart and died before checkpointing.
	op := NewOperation(ActionRestart, "test")
	op.Phase = PhaseRestarting
	op.PreviousInstanceID = "inst-1"
	require.NoError(t, store.Create(op))
	require.NoError(t, host.Restart(context.Background()))
	host.restarts = 0

	got, err := ex.Run(context.Background(), op.ID)
	require.NoError(t, err)
	require.Equal(t, PhaseSucceeded, got.Phase)
	require.Equal(t, 0, host.restarts)
	require.Equal(t, "inst-2", got.NewInstanceID)
}

func TestResumeInInstallingRedownloads(t *testing.T) {
	host := newFakeHost("v1.0.0")
	ex, store := newTestExecutor(t, host)

	op := NewOperation(ActionUpgrade, "test")
	op.TargetVersion = "v2.0.0"
	op.ResolvedVersion = "v2.0.0"
	op.Phase = PhaseInstalling
	op.PreviousInstanceID = "inst-1"
	op.PreviousVersion = "v1.0.0"
	require.NoError(t, store.Create(op))

	got, err := ex.Run(context.Background(), op.ID)
	require.NoError(t, err)
	require.Equal(t, PhaseSucceeded, got.Phase, got.Error)
	require.Equal(t, 1, host.downloads)
	require.Equal(t, 1, host.installs)
}

func TestResumeInInstallingSkipsWhenBinaryAlreadyInstalled(t *testing.T) {
	host := newFakeHost("v2.0.0")
	host.running.Version = "v1.0.0"
	host.running.Commit = "c-v1.0.0"
	ex, store := newTestExecutor(t, host)

	op := NewOperation(ActionUpgrade, "test")
	op.TargetVersion = "v2.0.0"
	op.ResolvedVersion = "v2.0.0"
	op.Phase = PhaseInstalling
	op.PreviousInstanceID = "inst-1"
	op.PreviousVersion = "v1.0.0"
	require.NoError(t, store.Create(op))

	got, err := ex.Run(context.Background(), op.ID)
	require.NoError(t, err)
	require.Equal(t, PhaseSucceeded, got.Phase, got.Error)
	require.Equal(t, 0, host.downloads)
	require.Equal(t, 0, host.installs)
	require.Equal(t, 1, host.restarts)
}

func TestContainerInstallIsRefused(t *testing.T) {
	host := newFakeHost("v1.0.0")
	host.running.InstallKind = "container"
	ex, store := newTestExecutor(t, host)

	op := NewOperation(ActionRestart, "test")
	require.NoError(t, store.Create(op))

	got, err := ex.Run(context.Background(), op.ID)
	require.NoError(t, err)
	require.Equal(t, PhaseFailed, got.Phase)
	require.Contains(t, got.Error, "container")
	require.Equal(t, 0, host.restarts)
}

func TestRunOnFinishedOperationIsANoop(t *testing.T) {
	host := newFakeHost("v1.0.0")
	ex, store := newTestExecutor(t, host)

	op := NewOperation(ActionRestart, "test")
	require.NoError(t, store.Create(op))
	first, err := ex.Run(context.Background(), op.ID)
	require.NoError(t, err)

	second, err := ex.Run(context.Background(), op.ID)
	require.NoError(t, err)
	require.Equal(t, first.Phase, second.Phase)
	require.Equal(t, first.NewInstanceID, second.NewInstanceID)
	require.Equal(t, 1, host.restarts)
}

func TestCancelledRunLeavesOperationResumable(t *testing.T) {
	host := newFakeHost("v1.0.0")
	host.bootProbes = 1000
	ex, store := newTestExecutor(t, host)
	ex.opts.ReadyTimeout = time.Hour

	op := NewOperation(ActionRestart, "test")
	require.NoError(t, store.Create(op))

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	got, err := ex.Run(ctx, op.ID)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Equal(t, PhaseVerifying, got.Phase)

	stored, err := store.Get(op.ID)
	require.NoError(t, err)
	require.False(t, stored.Done())
}

func TestStoreRefusesConcurrentOperations(t *testing.T) {
	store, err := NewStore(t.TempDir())
	require.NoError(t, err)

	first := NewOperation(ActionRestart, "test")
	require.NoError(t, store.Create(first))

	second := NewOperation(ActionRestart, "test")
	require.ErrorIs(t, store.Create(second), ErrBusy)

	first.Phase = PhaseSucceeded
	require.NoError(t, store.Update(first))
	require.NoError(t, store.Create(second))

	_, err = store.Get("nope")
	require.ErrorIs(t, err, ErrNotFound)

	ops, err := store.List()
	require.NoError(t, err)
	require.Len(t, ops, 2)
	require.Equal(t, first.ID, ops[0].ID)
}

func TestStoreCreateSerializesConcurrentCallers(t *testing.T) {
	store, err := NewStore(t.TempDir())
	require.NoError(t, err)

	const n = 8
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		go func() { errs <- store.Create(NewOperation(ActionRestart, "test")) }()
	}
	created := 0
	for i := 0; i < n; i++ {
		if err := <-errs; err == nil {
			created++
		} else {
			require.ErrorIs(t, err, ErrBusy)
		}
	}
	require.Equal(t, 1, created)
}

func TestCancelledDownloadKeepsPhase(t *testing.T) {
	host := newFakeHost("v1.0.0")
	ex, store := newTestExecutor(t, host)
	ctx, cancel := context.WithCancel(context.Background())
	// A downloader that observes cancellation the way the real one does.
	ex.WithDownloader(cancellingDownloader{fakeHost: host, cancel: cancel})

	op := NewOperation(ActionUpgrade, "test")
	op.TargetVersion = "v2.0.0"
	require.NoError(t, store.Create(op))

	got, err := ex.Run(ctx, op.ID)
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, PhaseDownloading, got.Phase)

	stored, err := store.Get(op.ID)
	require.NoError(t, err)
	require.Equal(t, PhaseDownloading, stored.Phase)
	require.Empty(t, stored.Error)
}

// cancellingDownloader cancels the run mid-download and returns the error
// the real HTTP client would.
type cancellingDownloader struct {
	*fakeHost
	cancel context.CancelFunc
}

func (d cancellingDownloader) Download(ctx context.Context, a release.Artifact, opts release.DownloadOptions) (*release.DownloadedArtifact, error) {
	d.cancel()
	return nil, ctx.Err()
}

func TestResumeRollbackAfterRestoreRestartsAnyway(t *testing.T) {
	host := newFakeHost("v1.0.0")
	ex, store := newTestExecutor(t, host)

	// The failed v2 upgrade was rolled back on disk, then the executor died
	// before restarting: no backup left, v1 on disk, broken v2 still running.
	host.onDisk = release.VersionInfo{Version: "v1.0.0", Commit: "c-v1.0.0"}
	host.backup = nil
	host.running = Snapshot{InstanceID: "inst-2", Version: "v2.0.0", Commit: "c-v2.0.0", Ready: false, InstallKind: "systemd"}
	host.instances = 2

	op := NewOperation(ActionUpgrade, "test")
	op.TargetVersion = "v2.0.0"
	op.ResolvedVersion = "v2.0.0"
	op.ResolvedCommit = "c-v2.0.0"
	op.Phase = PhaseRollingBack
	op.Error = "server not ready within 200ms"
	op.PreviousInstanceID = "inst-1"
	op.PreviousVersion = "v1.0.0"
	op.PreviousCommit = "c-v1.0.0"
	require.NoError(t, store.Create(op))

	got, err := ex.Run(context.Background(), op.ID)
	require.NoError(t, err)
	require.Equal(t, PhaseRolledBack, got.Phase, got.Error)
	require.Equal(t, 0, host.rollbacks)
	require.Equal(t, 1, host.restarts)
	require.Equal(t, "v1.0.0", got.NewVersion)
	require.Contains(t, got.Error, "not ready within")
}

func TestSameBuildPrefersCommits(t *testing.T) {
	require.True(t, sameBuild("v1.0.0", "abc", "v1.0.0", "abc"))
	require.False(t, sameBuild("v1.0.0", "abc", "v1.0.0", "def"))
	require.True(t, sameBuild("v1.0.0", "", "v1.0.0", "def"))
	require.False(t, sameBuild("v1.0.0", "", "v1.0.1", ""))
}

func TestRunRefusesOperationHeldByAnotherExecutor(t *testing.T) {
	host := newFakeHost("v1.0.0")
	ex, store := newTestExecutor(t, host)

	op := NewOperation(ActionRestart, "test")
	require.NoError(t, store.Create(op))

	unlock, err := store.LockOperation(op.ID)
	require.NoError(t, err)
	_, err = ex.Run(context.Background(), op.ID)
	require.ErrorIs(t, err, ErrLocked)
	require.Equal(t, 0, host.restarts)
	unlock()

	got, err := ex.Run(context.Background(), op.ID)
	require.NoError(t, err)
	require.Equal(t, PhaseSucceeded, got.Phase)
}
