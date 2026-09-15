package containerboot

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"miren.dev/runtime/pkg/serverlifecycle"
)

func newBoot(t *testing.T, image string) Boot {
	t.Helper()
	dir := t.TempDir()
	imagePath := filepath.Join(dir, "image-miren")
	require.NoError(t, os.WriteFile(imagePath, []byte(image), 0755))
	return Boot{
		ReleaseDir:  filepath.Join(dir, "release"),
		ImageBinary: imagePath,
		Log:         slog.New(slog.NewTextHandler(os.Stderr, nil)),
	}
}

func releaseContents(t *testing.T, b Boot) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(b.ReleaseDir, "miren"))
	require.NoError(t, err)
	return string(data)
}

func marker(t *testing.T, b Boot) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(b.ReleaseDir, imageMarker))
	require.NoError(t, err)
	return string(data)
}

func TestPrepareSeedsFreshVolume(t *testing.T) {
	b := newBoot(t, "image-v1")
	// The image bakes the bundle into the volume before the first boot, so the
	// binary is there but nothing says which image put it there.
	require.NoError(t, os.MkdirAll(b.ReleaseDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(b.ReleaseDir, "miren"), []byte("image-v1"), 0755))

	bin, err := b.Prepare(context.Background())
	require.NoError(t, err)
	require.Equal(t, filepath.Join(b.ReleaseDir, "miren"), bin)
	require.Equal(t, "image-v1", releaseContents(t, b))
	require.NotEmpty(t, marker(t, b))

	info, err := os.Stat(bin)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0755), info.Mode().Perm())
}

func TestPrepareKeepsInPlaceUpgrade(t *testing.T) {
	b := newBoot(t, "image-v1")
	_, err := b.Prepare(context.Background())
	require.NoError(t, err)
	first := marker(t, b)

	// An upgrade replaced the volume's binary while the image stayed put.
	require.NoError(t, os.WriteFile(filepath.Join(b.ReleaseDir, "miren"), []byte("upgraded-v2"), 0755))

	bin, err := b.Prepare(context.Background())
	require.NoError(t, err)
	require.Equal(t, filepath.Join(b.ReleaseDir, "miren"), bin)
	require.Equal(t, "upgraded-v2", releaseContents(t, b), "the volume's binary is authoritative when the image has not changed")
	require.Equal(t, first, marker(t, b))
}

func TestPrepareReseedsWhenImageChanges(t *testing.T) {
	b := newBoot(t, "image-v1")
	_, err := b.Prepare(context.Background())
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(b.ReleaseDir, "miren"), []byte("upgraded-v2"), 0755))

	// A reinstall with a different image, against the same volume. Whether the
	// image is newer or older, it is what the operator asked to run.
	require.NoError(t, os.WriteFile(b.ImageBinary, []byte("image-v3"), 0755))
	bin, err := b.Prepare(context.Background())
	require.NoError(t, err)
	require.Equal(t, filepath.Join(b.ReleaseDir, "miren"), bin)
	require.Equal(t, "image-v3", releaseContents(t, b))

	// Now the marker matches the new image, so a further in-place upgrade sticks.
	require.NoError(t, os.WriteFile(filepath.Join(b.ReleaseDir, "miren"), []byte("upgraded-v4"), 0755))
	_, err = b.Prepare(context.Background())
	require.NoError(t, err)
	require.Equal(t, "upgraded-v4", releaseContents(t, b))
}

func TestPrepareRestoresMissingBinary(t *testing.T) {
	b := newBoot(t, "image-v1")
	_, err := b.Prepare(context.Background())
	require.NoError(t, err)
	require.NoError(t, os.Remove(filepath.Join(b.ReleaseDir, "miren")))

	bin, err := b.Prepare(context.Background())
	require.NoError(t, err)
	require.Equal(t, "image-v1", releaseContents(t, b))
	require.Equal(t, filepath.Join(b.ReleaseDir, "miren"), bin)
}

func TestPrepareLeavesOtherFilesAlone(t *testing.T) {
	b := newBoot(t, "image-v1")
	require.NoError(t, os.MkdirAll(b.ReleaseDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(b.ReleaseDir, "containerd"), []byte("containerd"), 0755))

	_, err := b.Prepare(context.Background())
	require.NoError(t, err)
	data, err := os.ReadFile(filepath.Join(b.ReleaseDir, "containerd"))
	require.NoError(t, err)
	require.Equal(t, "containerd", string(data))

	entries, err := os.ReadDir(b.ReleaseDir)
	require.NoError(t, err)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	require.ElementsMatch(t, []string{"miren", "containerd", imageMarker}, names, "no staging files left behind")
}

func newUpgradeBoot(t *testing.T) (Boot, *serverlifecycle.Store, *serverlifecycle.Operation) {
	t.Helper()
	b := newBoot(t, "image-v1")
	b.LifecycleDir = filepath.Join(t.TempDir(), "lifecycle")
	b.MaxBootAttempts = 2
	// An upgrade just installed v2 over v1 and restarted onto it.
	require.NoError(t, os.MkdirAll(b.ReleaseDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(b.ReleaseDir, "miren"), []byte("v2"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(b.ReleaseDir, "miren.old"), []byte("v1"), 0755))
	store, err := serverlifecycle.NewStore(b.LifecycleDir)
	require.NoError(t, err)
	op := serverlifecycle.NewOperation(serverlifecycle.ActionUpgrade, "test")
	op.TargetVersion = "v2.0.0"
	op.ResolvedVersion = "v2.0.0"
	op.ResolvedCommit = "c-v2"
	op.PreviousInstanceID = "inst-1"
	op.PreviousVersion = "v1.0.0"
	op.PreviousCommit = "c-v1"
	op.BackupRef = "etcd:snapshot"
	op.Phase = serverlifecycle.PhaseVerifying
	require.NoError(t, store.Create(op))
	return b, store, op
}

func TestGuardUpgradeCountsBootsOfTheNewBuild(t *testing.T) {
	b, store, op := newUpgradeBoot(t)
	for attempt := 1; attempt <= b.MaxBootAttempts; attempt++ {
		b.GuardUpgrade(context.Background())
		got, err := store.Get(op.ID)
		require.NoError(t, err)
		require.Equal(t, attempt, got.BootAttempts)
		require.Equal(t, serverlifecycle.PhaseVerifying, got.Phase)
	}
	require.Equal(t, "v2", releaseContents(t, b), "the new build keeps its chances")
}

func TestGuardUpgradeRollsBackACrashLoop(t *testing.T) {
	b, store, op := newUpgradeBoot(t)
	for range b.MaxBootAttempts + 1 {
		b.GuardUpgrade(context.Background())
	}
	got, err := store.Get(op.ID)
	require.NoError(t, err)
	require.Equal(t, serverlifecycle.PhaseRollingBack, got.Phase)
	require.Equal(t, RollbackFrom, got.RollbackFrom)
	require.Contains(t, got.Error, "did not come up in 2 boots")
	require.NotNil(t, got.DataRestore)
	require.Equal(t, "etcd:snapshot", got.DataRestore.BackupRef)
	require.Equal(t, "v1.0.0", got.DataRestore.ForVersion)
	require.Equal(t, "v1", releaseContents(t, b))
	require.NoFileExists(t, filepath.Join(b.ReleaseDir, "miren.old"))

	// The rolled-back build boots, restores the data, and its executor
	// resumes the rollback without restarting again.
	require.NoError(t, store.WriteRestoreResult(&serverlifecycle.RestoreResult{OperationID: op.ID, BackupRef: "etcd:snapshot", RestoredAt: time.Now().UTC()}))
	opts := serverlifecycle.DefaultOptions()
	opts.InstallPath = filepath.Join(b.ReleaseDir, "miren")
	opts.PathSymlink = ""
	opts.ReadyTimeout = 200 * time.Millisecond
	opts.ProbeInterval = 5 * time.Millisecond
	ex := serverlifecycle.NewExecutor(store, opts, b.Log).
		WithProber(staticProber{serverlifecycle.Snapshot{InstanceID: "inst-3", Version: "v1.0.0", Commit: "c-v1", Ready: true, InstallKind: "container"}}).
		WithRestarter(restarterFunc(func(context.Context) error {
			return errors.New("the restart already happened; restarting again would loop")
		}))
	done, err := ex.Run(context.Background(), op.ID)
	require.NoError(t, err)
	require.Equal(t, serverlifecycle.PhaseRolledBack, done.Phase, done.Error)
	require.Equal(t, "inst-3", done.NewInstanceID)
	require.NotNil(t, done.DataRestore.RestoredAt)
}

func TestGuardUpgradeFailsWhenThereIsNothingToRollBackTo(t *testing.T) {
	b, store, op := newUpgradeBoot(t)
	require.NoError(t, os.Remove(filepath.Join(b.ReleaseDir, "miren.old")))
	for range b.MaxBootAttempts + 1 {
		b.GuardUpgrade(context.Background())
	}
	got, err := store.Get(op.ID)
	require.NoError(t, err)
	require.Equal(t, serverlifecycle.PhaseFailed, got.Phase)
	require.Contains(t, got.Error, "no previous binary")
	require.Equal(t, "v2", releaseContents(t, b))
}

func TestGuardUpgradeHonoursNoRollback(t *testing.T) {
	b, store, op := newUpgradeBoot(t)
	op.NoRollback = true
	require.NoError(t, store.Update(op))
	for range b.MaxBootAttempts + 1 {
		b.GuardUpgrade(context.Background())
	}
	got, err := store.Get(op.ID)
	require.NoError(t, err)
	require.Equal(t, serverlifecycle.PhaseFailed, got.Phase)
	require.Contains(t, got.Error, "turned off")
	require.Equal(t, "v2", releaseContents(t, b))
	require.FileExists(t, filepath.Join(b.ReleaseDir, "miren.old"))
}

func TestGuardUpgradeLeavesOtherOperationsAlone(t *testing.T) {
	b, store, op := newUpgradeBoot(t)
	op.Action = serverlifecycle.ActionRestart
	op.TargetVersion = ""
	require.NoError(t, store.Update(op))
	for range b.MaxBootAttempts + 1 {
		b.GuardUpgrade(context.Background())
	}
	got, err := store.Get(op.ID)
	require.NoError(t, err)
	require.Equal(t, 0, got.BootAttempts)
	require.Equal(t, serverlifecycle.PhaseVerifying, got.Phase)
	require.Equal(t, "v2", releaseContents(t, b))

	// No ledger at all is a fresh install, not an error.
	b.LifecycleDir = filepath.Join(t.TempDir(), "absent")
	b.GuardUpgrade(context.Background())
}

type staticProber struct{ snap serverlifecycle.Snapshot }

func (p staticProber) Probe(context.Context) (serverlifecycle.Snapshot, error) { return p.snap, nil }

type restarterFunc func(context.Context) error

func (f restarterFunc) Restart(ctx context.Context) error { return f(ctx) }
