package serverlifecycle

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// containerHost stands in for the server process the executor runs inside.
// Shutdown cancels the launcher's context, which is what a SIGTERM does to
// the real one; the "next instance" is a new launcher over the same store
// with the fake host reporting a new instance id.
type containerHost struct {
	*fakeHost
	store  *Store
	cancel context.CancelFunc
	// shutdowns counts restart requests; a resumed run must not add to it.
	shutdowns int
}

func newContainerHost(t *testing.T, version string) *containerHost {
	t.Helper()
	store, err := NewStore(t.TempDir())
	require.NoError(t, err)
	host := newFakeHost(version)
	host.running.InstallKind = "container"
	host.store = store
	return &containerHost{fakeHost: host, store: store}
}

// launcher returns a launcher for one instance's lifetime; the executor it
// builds uses the fake host for everything but restarting and probing.
func (h *containerHost) launcher(t *testing.T) *ContainerLauncher {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	h.cancel = cancel
	return NewContainerLauncher(ctx, slog.Default(), func() (*Executor, error) {
		opts := DefaultOptions()
		opts.ReadyTimeout = 200 * time.Millisecond
		opts.ProbeInterval = 5 * time.Millisecond
		opts.PathSymlink = ""
		return NewExecutor(h.store, opts, slog.Default()).
			WithDownloader(h.fakeHost).WithInstaller(h.fakeHost).WithDataBackup(dataBackup{h.fakeHost}).
			WithRestarter(ContainerRestarter{Shutdown: h.shutdown}).
			WithProber(h.fakeHost), nil
	})
}

func (h *containerHost) shutdown() error {
	h.shutdowns++
	h.cancel()
	return nil
}

// reboot is the container runtime bringing a new container up on the
// volume: a new instance id running whatever binary is on disk.
func (h *containerHost) reboot() {
	h.instances++
	h.running = Snapshot{
		InstanceID:  "inst-" + string(rune('0'+h.instances)),
		Version:     h.onDisk.Version,
		Commit:      h.onDisk.Commit,
		Ready:       true,
		InstallKind: "container",
	}
}

func TestContainerRestartResumesInTheNextInstance(t *testing.T) {
	host := newContainerHost(t, "v1.0.0")
	op := NewOperation(ActionRestart, "test")
	require.NoError(t, host.store.Create(op))

	first := host.launcher(t)
	require.NoError(t, first.Launch(context.Background(), op.ID))
	first.Wait()

	paused, err := host.store.Get(op.ID)
	require.NoError(t, err)
	require.False(t, paused.Done(), "the run ends with the process, not the operation")
	require.Contains(t, []Phase{PhaseRestarting, PhaseVerifying}, paused.Phase)
	require.Equal(t, 1, host.shutdowns)
	require.Equal(t, "inst-1", paused.PreviousInstanceID)

	host.reboot()
	next := host.launcher(t)
	require.NoError(t, next.Resume(context.Background(), host.store))
	next.Wait()

	got, err := host.store.Get(op.ID)
	require.NoError(t, err)
	require.Equal(t, PhaseSucceeded, got.Phase, got.Error)
	require.Equal(t, "inst-2", got.NewInstanceID)
	require.Equal(t, 1, host.shutdowns, "the resumed run must not restart again")
}

func TestContainerUpgradeResumesOnTheNewBuild(t *testing.T) {
	host := newContainerHost(t, "v1.0.0")
	op := NewOperation(ActionUpgrade, "test")
	op.TargetVersion = "v2.0.0"
	require.NoError(t, host.store.Create(op))

	first := host.launcher(t)
	require.NoError(t, first.Launch(context.Background(), op.ID))
	first.Wait()
	require.Equal(t, 1, host.installs)
	require.Equal(t, "v2.0.0", host.onDisk.Version)

	host.reboot()
	next := host.launcher(t)
	require.NoError(t, next.Resume(context.Background(), host.store))
	next.Wait()

	got, err := host.store.Get(op.ID)
	require.NoError(t, err)
	require.Equal(t, PhaseSucceeded, got.Phase, got.Error)
	require.Equal(t, "v2.0.0", got.NewVersion)
	require.Equal(t, "v1.0.0", got.PreviousVersion)
	require.Equal(t, 1, host.shutdowns)
}

func TestContainerResumeWithNothingPendingIsANoop(t *testing.T) {
	host := newContainerHost(t, "v1.0.0")
	done := NewOperation(ActionRestart, "test")
	done.Phase = PhaseSucceeded
	require.NoError(t, host.store.Create(done))

	launcher := host.launcher(t)
	require.NoError(t, launcher.Resume(context.Background(), host.store))
	launcher.Wait()
	require.Equal(t, 0, host.shutdowns)
}

func TestContainerRestarterUsesTheShutdownHook(t *testing.T) {
	// Only the wiring is checked; sending SIGTERM to the test binary is not
	// an option. A Shutdown hook replaces the signal entirely.
	called := false
	r := ContainerRestarter{Shutdown: func() error { called = true; return nil }}
	require.NoError(t, r.Restart(context.Background()))
	require.True(t, called)
}
