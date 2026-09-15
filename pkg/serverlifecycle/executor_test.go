package serverlifecycle

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
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
	backups   int
	metadata  map[string]*release.Metadata
	instances int

	// The data side: the store lets Restart play the booting server, which
	// answers a pending restore request according to restoreBehavior.
	store           *Store
	restoreBehavior restoreBehavior
	restoresSeen    []string
	// restoreRefusing is the server refusing to boot because its restore
	// failed, which it keeps doing until the request is settled.
	restoreRefusing bool
	backupErr       error
	// installsAtBackup and backupsAtInstall pin the order of the two phases.
	installsAtBackup int
	backupsAtInstall int
	// requestAtRollback is the restore request on disk when the binary was
	// rolled back: the request has to be durable before the old binary is.
	requestAtRollback *DataRestore
}

type restoreBehavior int

const (
	restoreBehaviorRestore restoreBehavior = iota
	restoreBehaviorIgnore                  // an older build that predates data restore
	restoreBehaviorFail
)

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
	if h.restoreRefusing {
		return Snapshot{}, errors.New("connection refused")
	}
	return h.running, nil
}

func (h *fakeHost) Restart(context.Context) error {
	h.restarts++
	h.instances++
	if err := h.restoreOnBoot(); err != nil {
		return err
	}
	h.running = Snapshot{
		InstanceID:  fmt.Sprintf("inst-%d", h.instances),
		Version:     h.onDisk.Version,
		Commit:      h.onDisk.Commit,
		Ready:       true,
		InstallKind: "systemd",
		Components:  map[string]string{"containerd": "v2.0.4", "runc": "1.2.2"},
	}
	h.pending = h.bootProbes
	return nil
}

// restoreOnBoot is the booting server answering a pending restore request,
// the way the data-restore boot component does.
func (h *fakeHost) restoreOnBoot() error {
	if h.store == nil {
		return nil
	}
	op, err := h.store.PendingRestore()
	if err != nil || op == nil {
		return err
	}
	h.restoresSeen = append(h.restoresSeen, op.DataRestore.BackupRef)
	result := &RestoreResult{OperationID: op.ID, BackupRef: op.DataRestore.BackupRef, RestoredAt: time.Now().UTC()}
	switch h.restoreBehavior {
	case restoreBehaviorIgnore:
		return nil
	case restoreBehaviorFail:
		result.Error = "etcdutl exited 1"
		h.restoreRefusing = true
	case restoreBehaviorRestore:
	}
	return h.store.WriteRestoreResult(result)
}

func (h *fakeHost) Install(_ context.Context, d *release.DownloadedArtifact) error {
	h.installs++
	h.backupsAtInstall = h.backups
	prev := h.onDisk
	h.backup = &prev
	h.onDisk = release.VersionInfo{Version: d.Artifact.Version, Commit: "c-" + d.Artifact.Version}
	return nil
}
func (h *fakeHost) Backup(context.Context) error { return nil }

func (h *fakeHost) BackupData(_ context.Context, opID string) (string, error) {
	h.backups++
	h.installsAtBackup = h.installs
	if h.backupErr != nil {
		return "", h.backupErr
	}
	return "etcd:" + opID, nil
}

// dataBackup adapts the host to the DataBackup seam, whose method name
// collides with the installer's Backup.
type dataBackup struct{ *fakeHost }

func (d dataBackup) Backup(ctx context.Context, opID string) (string, error) {
	return d.BackupData(ctx, opID)
}
func (h *fakeHost) Rollback(context.Context) error {
	h.rollbacks++
	if h.backup == nil {
		return errors.New("no backup")
	}
	if h.store != nil {
		if ops, err := h.store.List(); err == nil {
			for _, op := range ops {
				if op.DataRestore != nil {
					h.requestAtRollback = op.DataRestore
				}
			}
		}
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
		WithDownloader(host).WithInstaller(host).WithRestarter(host).WithProber(host).
		WithDataBackup(dataBackup{host})
	host.store = store
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
	// The restarted server's runtime versions are the record that the bundle
	// swap took, not just the miren binary.
	require.Equal(t, map[string]string{"containerd": "v2.0.4", "runc": "1.2.2"}, got.Components)
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

	// The data came back with the binary: the booting server saw the request
	// and its answer is on the record.
	require.Equal(t, []string{"etcd:" + op.ID}, host.restoresSeen)
	// The request was on disk before the old binary was, and it names the
	// build it is for, so a systemd restart in that window cannot start the
	// old build on migrated data or let the new one restore for it.
	require.NotNil(t, host.requestAtRollback)
	require.Equal(t, "v1.0.0", host.requestAtRollback.ForVersion)
	require.Equal(t, "c-v1.0.0", host.requestAtRollback.ForCommit)
	require.True(t, got.DataRestore.MeantFor("v1.0.0", "c-v1.0.0"))
	require.False(t, got.DataRestore.MeantFor("v2.0.0", "c-v2.0.0"))
	require.NotNil(t, got.DataRestore)
	require.Equal(t, got.BackupRef, got.DataRestore.BackupRef)
	require.NotNil(t, got.DataRestore.RestoredAt)
	require.Empty(t, got.DataRestore.Error)
}

func TestUpgradeBacksUpAfterDownloadBeforeInstall(t *testing.T) {
	host := newFakeHost("v1.0.0")
	ex, store := newTestExecutor(t, host)

	op := NewOperation(ActionUpgrade, "test")
	op.TargetVersion = "v2.0.0"
	require.NoError(t, store.Create(op))

	got, err := ex.Run(context.Background(), op.ID)
	require.NoError(t, err)
	require.Equal(t, PhaseSucceeded, got.Phase, got.Error)
	require.Equal(t, "etcd:"+op.ID, got.BackupRef)
	require.Equal(t, 1, host.backups)
	require.Equal(t, 0, host.installsAtBackup)
	require.Equal(t, 1, host.backupsAtInstall)
	require.Nil(t, got.DataRestore)
}

func TestUpgradeFailsWhenBackupFails(t *testing.T) {
	host := newFakeHost("v1.0.0")
	host.backupErr = errors.New("etcd unreachable")
	ex, store := newTestExecutor(t, host)

	op := NewOperation(ActionUpgrade, "test")
	op.TargetVersion = "v2.0.0"
	require.NoError(t, store.Create(op))

	got, err := ex.Run(context.Background(), op.ID)
	require.NoError(t, err)
	require.Equal(t, PhaseFailed, got.Phase)
	require.Contains(t, got.Error, "etcd unreachable")
	require.Equal(t, 0, host.installs)
	require.Equal(t, 0, host.restarts)
	require.Equal(t, "v1.0.0", host.onDisk.Version)
}

func TestNoRollbackSkipsBackup(t *testing.T) {
	host := newFakeHost("v1.0.0")
	host.backupErr = errors.New("would fail if asked")
	ex, store := newTestExecutor(t, host)

	op := NewOperation(ActionUpgrade, "test")
	op.TargetVersion = "v2.0.0"
	op.NoRollback = true
	require.NoError(t, store.Create(op))

	got, err := ex.Run(context.Background(), op.ID)
	require.NoError(t, err)
	require.Equal(t, PhaseSucceeded, got.Phase, got.Error)
	require.Equal(t, 0, host.backups)
	require.Empty(t, got.BackupRef)
}

func TestUpgradeWithoutDataBackupRollsBackBinaryOnly(t *testing.T) {
	host := newFakeHost("v1.0.0")
	host.failBoot = true
	ex, store := newTestExecutor(t, host)
	ex.WithDataBackup(nil)

	op := NewOperation(ActionUpgrade, "test")
	op.TargetVersion = "v2.0.0"
	require.NoError(t, store.Create(op))

	got, err := ex.Run(context.Background(), op.ID)
	require.NoError(t, err)
	require.Equal(t, PhaseRolledBack, got.Phase, got.Error)
	require.Empty(t, got.BackupRef)
	require.Nil(t, got.DataRestore)
	require.Empty(t, host.restoresSeen)
}

func TestRollbackFailsWhenServerDoesNotRestoreData(t *testing.T) {
	host := newFakeHost("v1.0.0")
	host.failBoot = true
	host.restoreBehavior = restoreBehaviorIgnore
	ex, store := newTestExecutor(t, host)

	op := NewOperation(ActionUpgrade, "test")
	op.TargetVersion = "v2.0.0"
	require.NoError(t, store.Create(op))

	got, err := ex.Run(context.Background(), op.ID)
	require.NoError(t, err)
	// The old binary is back and serving, but on the new build's data: not
	// a rollback, and the record must say so.
	require.Equal(t, PhaseFailed, got.Phase)
	require.Contains(t, got.Error, "did not restore etcd:"+op.ID)
	require.Equal(t, "v1.0.0", got.NewVersion)
	require.NotNil(t, got.DataRestore)
	require.Nil(t, got.DataRestore.RestoredAt)
}

// A server whose restore fails refuses to boot rather than serve the data
// the rollback was meant to replace. The executor times out on it, but the
// record carries the restore error and the way out, and the request stays
// pending so the server keeps refusing until an operator abandons it.
func TestRollbackFailsWhenDataRestoreFails(t *testing.T) {
	host := newFakeHost("v1.0.0")
	host.failBoot = true
	host.restoreBehavior = restoreBehaviorFail
	ex, store := newTestExecutor(t, host)

	op := NewOperation(ActionUpgrade, "test")
	op.TargetVersion = "v2.0.0"
	require.NoError(t, store.Create(op))

	got, err := ex.Run(context.Background(), op.ID)
	require.NoError(t, err)
	require.Equal(t, PhaseFailed, got.Phase)
	require.Contains(t, got.Error, "server not ready after rollback")
	require.Contains(t, got.Error, "data restore failed: etcdutl exited 1")
	require.Contains(t, got.Error, "operations abandon "+op.ID)
	require.Equal(t, "etcdutl exited 1", got.DataRestore.Error)

	pending, err := store.PendingRestore()
	require.NoError(t, err)
	require.NotNil(t, pending, "a failed restore is still pending, even on a finished operation")

	abandoned, err := Abandon(store, op.ID)
	require.NoError(t, err)
	require.Equal(t, PhaseFailed, abandoned.Phase)
	require.Equal(t, "etcdutl exited 1; abandoned by operator", abandoned.DataRestore.Error)
	pending, err = store.PendingRestore()
	require.NoError(t, err)
	require.Nil(t, pending)
	result, err := store.ReadRestoreResult(op.ID)
	require.NoError(t, err)
	require.True(t, result.Abandoned)
}

func TestAbandonFinishesADeadExecutorsOperation(t *testing.T) {
	store, err := NewStore(t.TempDir())
	require.NoError(t, err)
	op := NewOperation(ActionUpgrade, "test")
	op.TargetVersion = "v2.0.0"
	op.Phase = PhaseRollingBack
	op.Error = "server not ready within 200ms"
	op.BackupRef = "etcd:" + op.ID
	op.DataRestore = &DataRestore{BackupRef: op.BackupRef}
	require.NoError(t, store.Create(op))

	// Held by a live executor: refused.
	unlock, err := store.LockOperation(op.ID)
	require.NoError(t, err)
	_, err = Abandon(store, op.ID)
	require.ErrorIs(t, err, ErrLocked)
	unlock()

	got, err := Abandon(store, op.ID)
	require.NoError(t, err)
	require.Equal(t, PhaseFailed, got.Phase)
	require.Equal(t, "server not ready within 200ms; abandoned by operator", got.Error)
	require.Equal(t, "abandoned by operator", got.DataRestore.Error)
	pending, err := store.PendingRestore()
	require.NoError(t, err)
	require.Nil(t, pending)

	// Nothing left to abandon.
	_, err = Abandon(store, op.ID)
	require.ErrorIs(t, err, ErrNothingToAbandon)
	require.Equal(t, 1, len(mustList(t, store)))
}

func mustList(t *testing.T, store *Store) []*Operation {
	t.Helper()
	ops, err := store.List()
	require.NoError(t, err)
	return ops
}

func TestResumeRollbackAfterRestoreDoesNotRestoreAgain(t *testing.T) {
	host := newFakeHost("v1.0.0")
	ex, store := newTestExecutor(t, host)

	// The server restored the data and came up as v1 again, then the
	// executor died before reading the result.
	host.onDisk = release.VersionInfo{Version: "v1.0.0", Commit: "c-v1.0.0"}
	host.backup = nil
	host.running = Snapshot{InstanceID: "inst-2", Version: "v1.0.0", Commit: "c-v1.0.0", Ready: true, InstallKind: "systemd"}
	host.instances = 2

	op := NewOperation(ActionUpgrade, "test")
	op.TargetVersion = "v2.0.0"
	op.ResolvedVersion = "v2.0.0"
	op.Phase = PhaseRollingBack
	op.Error = "server not ready within 200ms"
	op.PreviousInstanceID = "inst-1"
	op.PreviousVersion = "v1.0.0"
	op.PreviousCommit = "c-v1.0.0"
	op.BackupRef = "etcd:" + op.ID
	op.DataRestore = &DataRestore{BackupRef: op.BackupRef}
	require.NoError(t, store.Create(op))
	restoredAt := time.Now().UTC().Add(-time.Minute)
	require.NoError(t, store.WriteRestoreResult(&RestoreResult{OperationID: op.ID, BackupRef: op.BackupRef, RestoredAt: restoredAt}))

	got, err := ex.Run(context.Background(), op.ID)
	require.NoError(t, err)
	require.Equal(t, PhaseRolledBack, got.Phase, got.Error)
	require.Empty(t, host.restoresSeen)
	require.Equal(t, 1, host.restarts)
	require.True(t, restoredAt.Equal(*got.DataRestore.RestoredAt))
}

func TestDataRestoreMeantForUnknownBuildIsForAnyone(t *testing.T) {
	r := &DataRestore{BackupRef: "etcd:x"}
	require.True(t, r.MeantFor("v9.9.9", "whatever"))
	r.ForVersion = "v1.0.0"
	require.True(t, r.MeantFor("v1.0.0", "unknown"), "version decides when a commit is unknown")
	require.False(t, r.MeantFor("v1.0.1", "unknown"))
}

// A record that cannot be decoded might be the one carrying the request, so
// PendingRestore reports the error rather than answering "nothing pending".
// List stays lenient for the operator listing.
func TestStorePendingRestoreReadsStrictly(t *testing.T) {
	store, err := NewStore(t.TempDir())
	require.NoError(t, err)
	op := NewOperation(ActionRestart, "test")
	require.NoError(t, store.Create(op))
	require.NoError(t, os.WriteFile(filepath.Join(store.Dir(), "01BROKEN.json"), []byte("{not json"), 0o644))

	ops, err := store.List()
	require.NoError(t, err)
	require.Len(t, ops, 1)

	_, err = store.PendingRestore()
	require.ErrorContains(t, err, "decode operation 01BROKEN")
}

func TestStorePendingRestore(t *testing.T) {
	store, err := NewStore(t.TempDir())
	require.NoError(t, err)

	op := NewOperation(ActionUpgrade, "test")
	op.TargetVersion = "v2.0.0"
	op.BackupRef = "etcd:" + op.ID
	require.NoError(t, store.Create(op))

	pending, err := store.PendingRestore()
	require.NoError(t, err)
	require.Nil(t, pending, "nothing to restore until a rollback asks")

	op.Phase = PhaseRollingBack
	op.DataRestore = &DataRestore{BackupRef: op.BackupRef}
	require.NoError(t, store.Update(op))
	pending, err = store.PendingRestore()
	require.NoError(t, err)
	require.NotNil(t, pending)
	require.Equal(t, op.ID, pending.ID)

	_, err = store.ReadRestoreResult(op.ID)
	require.ErrorIs(t, err, ErrNotFound)
	require.NoError(t, store.WriteRestoreResult(&RestoreResult{OperationID: op.ID, BackupRef: op.BackupRef, Error: "disk full"}))
	pending, err = store.PendingRestore()
	require.NoError(t, err)
	require.NotNil(t, pending, "a failed restore is retried on the next boot")

	op.Phase = PhaseFailed
	require.NoError(t, store.Update(op))
	pending, err = store.PendingRestore()
	require.NoError(t, err)
	require.NotNil(t, pending, "the request outlives the operation")

	require.NoError(t, store.WriteRestoreResult(&RestoreResult{OperationID: op.ID, BackupRef: op.BackupRef, RestoredAt: time.Now()}))
	pending, err = store.PendingRestore()
	require.NoError(t, err)
	require.Nil(t, pending, "a successful restore settles the request")

	// Results are not operations.
	ops, err := store.List()
	require.NoError(t, err)
	require.Len(t, ops, 1)
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

// The executor does not care how the server is supervised; the Restarter
// and Launcher it is given carry that.
func TestContainerInstallRestartsLikeAnyOther(t *testing.T) {
	host := newFakeHost("v1.0.0")
	host.running.InstallKind = "container"
	ex, store := newTestExecutor(t, host)

	op := NewOperation(ActionRestart, "test")
	require.NoError(t, store.Create(op))

	got, err := ex.Run(context.Background(), op.ID)
	require.NoError(t, err)
	require.Equal(t, PhaseSucceeded, got.Phase, got.Error)
	require.Equal(t, 1, host.restarts)
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

// An operator who abandons a restore while the server is mid-attempt (and
// then restarts the service, which fails that attempt) must not have the
// failed attempt reopen the request behind them.
func TestRecordRestoreAttemptKeepsAbandonment(t *testing.T) {
	store, err := NewStore(t.TempDir())
	require.NoError(t, err)
	const id = "01ABANDONED"

	failed := &RestoreResult{OperationID: id, BackupRef: "etcd:" + id, Error: "context canceled"}
	got, err := store.RecordRestoreAttempt(failed)
	require.NoError(t, err)
	require.False(t, got.Settled(), "a failed attempt with no abandonment is recorded and stays pending")

	require.NoError(t, store.WriteRestoreResult(&RestoreResult{OperationID: id, BackupRef: "etcd:" + id, Error: "abandoned by operator", Abandoned: true}))
	got, err = store.RecordRestoreAttempt(failed)
	require.NoError(t, err)
	require.True(t, got.Abandoned, "a failed attempt does not overwrite an abandonment")
	onDisk, err := store.ReadRestoreResult(id)
	require.NoError(t, err)
	require.True(t, onDisk.Abandoned)

	restored := &RestoreResult{OperationID: id, BackupRef: "etcd:" + id, RestoredAt: time.Now()}
	got, err = store.RecordRestoreAttempt(restored)
	require.NoError(t, err)
	require.False(t, got.Abandoned, "a restore that did complete is recorded as such")
	require.True(t, got.Settled())
}
