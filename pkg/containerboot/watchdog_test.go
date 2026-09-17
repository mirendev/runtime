package containerboot

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"miren.dev/runtime/pkg/serverlifecycle"
)

// sleeper stands in for the server: a process the watchdog can see is
// alive and can end.
func sleeper(t *testing.T) *exec.Cmd {
	t.Helper()
	cmd := exec.Command("sleep", "60")
	require.NoError(t, cmd.Start())
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	return cmd
}

func newWatchdog(t *testing.T, timeout time.Duration) (Watchdog, *serverlifecycle.Store, *serverlifecycle.Operation, *exec.Cmd, *[]int) {
	t.Helper()
	b, store, op := newUpgradeBoot(t)
	server := sleeper(t)
	var killed []int
	w := Watchdog{
		Boot:        b,
		OperationID: op.ID,
		ServerPID:   server.Process.Pid,
		Timeout:     timeout,
		Interval:    5 * time.Millisecond,
		Kill: func(pid int) error {
			killed = append(killed, pid)
			return server.Process.Kill()
		},
	}
	return w, store, op, server, &killed
}

func TestWatchdogRollsBackAHungBuild(t *testing.T) {
	w, store, op, server, killed := newWatchdog(t, 50*time.Millisecond)
	require.NoError(t, w.Run(context.Background()))

	got, err := store.Get(op.ID)
	require.NoError(t, err)
	require.Equal(t, serverlifecycle.PhaseRollingBack, got.Phase)
	require.Equal(t, RollbackFrom, got.RollbackFrom)
	require.Contains(t, got.Error, "did not report ready within")
	require.NotNil(t, got.DataRestore)
	require.Equal(t, "v1", releaseContents(t, w.Boot))
	require.Equal(t, []int{server.Process.Pid}, *killed)
}

func TestWatchdogStandsDownWhenTheUpgradeMovesOn(t *testing.T) {
	w, store, op, _, killed := newWatchdog(t, 5*time.Second)
	go func() {
		time.Sleep(30 * time.Millisecond)
		op.Phase = serverlifecycle.PhaseSucceeded
		_ = store.Update(op)
	}()
	start := time.Now()
	require.NoError(t, w.Run(context.Background()))
	require.Less(t, time.Since(start), 2*time.Second)
	require.Empty(t, *killed)
	require.Equal(t, "v2", releaseContents(t, w.Boot))
}

func TestWatchdogStandsDownWhenTheServerExits(t *testing.T) {
	w, store, op, server, killed := newWatchdog(t, 5*time.Second)
	require.NoError(t, server.Process.Kill())
	_ = server.Wait()
	require.NoError(t, w.Run(context.Background()))
	require.Empty(t, *killed)
	got, err := store.Get(op.ID)
	require.NoError(t, err)
	require.Equal(t, serverlifecycle.PhaseVerifying, got.Phase, "counting the boot is the next boot guard's job")
	require.Equal(t, "v2", releaseContents(t, w.Boot))
}

// The executor holds the operation's lock while it runs; a watchdog that
// reaches its deadline while it does leaves the outcome to the executor.
func TestWatchdogDefersToARunningExecutor(t *testing.T) {
	w, store, op, _, killed := newWatchdog(t, 30*time.Millisecond)
	unlock, err := store.LockOperation(op.ID)
	require.NoError(t, err)
	defer unlock()
	require.NoError(t, w.Run(context.Background()))
	require.Empty(t, *killed)
	require.Equal(t, "v2", releaseContents(t, w.Boot))
}

func TestWatchdogWithNothingToRollBackToFailsAndLeavesTheServer(t *testing.T) {
	w, store, op, _, killed := newWatchdog(t, 30*time.Millisecond)
	require.NoError(t, os.Remove(filepath.Join(w.ReleaseDir, "miren.old")))
	require.NoError(t, w.Run(context.Background()))
	got, err := store.Get(op.ID)
	require.NoError(t, err)
	require.Equal(t, serverlifecycle.PhaseFailed, got.Phase)
	require.Empty(t, *killed, "ending the server would only start the same build again")
}
