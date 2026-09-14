package serverlifecycle

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

type recordingLauncher struct {
	mu       sync.Mutex
	launched []string
	err      error
	// running makes the launcher report the unit as active despite err, the
	// shape of a systemd-run that failed after submitting the unit.
	running bool
	// sawCancelled records whether Launch was handed an already-cancelled
	// context, which Start must never do.
	sawCancelled bool
}

func (l *recordingLauncher) Launch(ctx context.Context, id string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if ctx.Err() != nil {
		l.sawCancelled = true
	}
	l.launched = append(l.launched, id)
	return l.err
}

func (l *recordingLauncher) Launched(ctx context.Context, _ string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if ctx.Err() != nil {
		l.sawCancelled = true
		return false
	}
	return l.running
}

func TestStartRecordsAndLaunches(t *testing.T) {
	store, err := NewStore(t.TempDir())
	require.NoError(t, err)
	launcher := &recordingLauncher{}

	op := NewOperation(ActionRestart, "test")
	started, created, err := Start(context.Background(), store, launcher, op)
	require.NoError(t, err)
	require.True(t, created)
	require.Equal(t, op.ID, started.ID)
	require.Equal(t, []string{op.ID}, launcher.launched)

	stored, err := store.Get(op.ID)
	require.NoError(t, err)
	require.Equal(t, PhasePending, stored.Phase)
}

func TestStartIsClosedOverID(t *testing.T) {
	store, err := NewStore(t.TempDir())
	require.NoError(t, err)
	launcher := &recordingLauncher{}

	op := NewOperation(ActionUpgrade, "cloud")
	op.TargetVersion = "v1.2.3"
	_, created, err := Start(context.Background(), store, launcher, op)
	require.NoError(t, err)
	require.True(t, created)

	// The same request again, as a retry after a lost reply would send it.
	again := NewOperation(ActionUpgrade, "cloud")
	again.ID = op.ID
	again.TargetVersion = "v1.2.3"
	existing, created, err := Start(context.Background(), store, launcher, again)
	require.NoError(t, err)
	require.False(t, created)
	require.Equal(t, op.ID, existing.ID)
	require.Len(t, launcher.launched, 1, "a retry must not launch a second executor")

	// A different request wearing the same id is a bug, not a retry.
	other := NewOperation(ActionRestart, "cloud")
	other.ID = op.ID
	_, _, err = Start(context.Background(), store, launcher, other)
	require.Error(t, err)
	require.Contains(t, err.Error(), "already exists")
}

func TestStartRejectsNonULIDIDs(t *testing.T) {
	store, err := NewStore(t.TempDir())
	require.NoError(t, err)
	launcher := &recordingLauncher{}

	op := NewOperation(ActionRestart, "test")
	op.ID = "../etc/passwd"
	_, _, err = Start(context.Background(), store, launcher, op)
	require.ErrorIs(t, err, ErrInvalidID)
	require.Empty(t, launcher.launched)
}

func TestStartMarksFailedWhenLaunchFails(t *testing.T) {
	store, err := NewStore(t.TempDir())
	require.NoError(t, err)
	launcher := &recordingLauncher{err: errors.New("systemd-run: no bus")}

	op := NewOperation(ActionRestart, "test")
	_, _, err = Start(context.Background(), store, launcher, op)
	require.Error(t, err)

	stored, err := store.Get(op.ID)
	require.NoError(t, err)
	require.Equal(t, PhaseFailed, stored.Phase)
	require.Contains(t, stored.Error, "could not start executor")

	// The failed record must not block the next attempt.
	next := NewOperation(ActionRestart, "test")
	launcher.err = nil
	_, created, err := Start(context.Background(), store, launcher, next)
	require.NoError(t, err)
	require.True(t, created)
}

func TestStartKeepsRecordWhenUnitIsRunningDespiteLaunchError(t *testing.T) {
	store, err := NewStore(t.TempDir())
	require.NoError(t, err)
	launcher := &recordingLauncher{err: errors.New("context canceled"), running: true}

	op := NewOperation(ActionRestart, "test")
	started, created, err := Start(context.Background(), store, launcher, op)
	require.NoError(t, err)
	require.True(t, created)
	require.Equal(t, PhasePending, started.Phase)

	// The slot stays taken: the executor owns this record now.
	_, _, err = Start(context.Background(), store, launcher, NewOperation(ActionRestart, "test"))
	require.ErrorIs(t, err, ErrBusy)
}

func TestStartRemovesRecordWhenFailureCannotBeWritten(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	require.NoError(t, err)
	launcher := &sabotagingLauncher{store: store, err: errors.New("systemd-run: no bus")}

	op := NewOperation(ActionRestart, "test")
	_, _, err = Start(context.Background(), store, launcher, op)
	require.Error(t, err)
	require.Contains(t, err.Error(), "could not record the failure")

	// The pending record must not survive to block the next operation.
	_, err = store.Get(op.ID)
	require.ErrorIs(t, err, ErrNotFound)
	_, created, err := Start(context.Background(), store, &recordingLauncher{}, NewOperation(ActionRestart, "test"))
	require.NoError(t, err)
	require.True(t, created)
}

// sabotagingLauncher fails the launch and, before returning, swaps the record
// for an empty directory of the same name: the store's rename-into-place then
// fails, while removing the empty directory still succeeds. That is the shape
// of "the failure could not be written, but the record can still be cleared".
type sabotagingLauncher struct {
	store *Store
	err   error
}

func (l *sabotagingLauncher) Launch(_ context.Context, id string) error {
	path := l.store.path(id)
	_ = os.Remove(path)
	_ = os.Mkdir(path, 0o755)
	return l.err
}

func TestStartLaunchesOnADetachedContext(t *testing.T) {
	store, err := NewStore(t.TempDir())
	require.NoError(t, err)
	launcher := &recordingLauncher{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, created, err := Start(ctx, store, launcher, NewOperation(ActionRestart, "test"))
	require.NoError(t, err)
	require.True(t, created)
	require.False(t, launcher.sawCancelled, "the caller's cancellation must not reach the launch")
}

func TestStartSettlesConcurrentRetriesUnderTheLock(t *testing.T) {
	store, err := NewStore(t.TempDir())
	require.NoError(t, err)
	launcher := &recordingLauncher{}
	id := NewID()

	const callers = 8
	var wg sync.WaitGroup
	results := make([]bool, callers)
	errs := make([]error, callers)
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			op := NewOperation(ActionUpgrade, "cloud")
			op.ID = id
			op.TargetVersion = "latest"
			_, results[i], errs[i] = Start(context.Background(), store, launcher, op)
		}(i)
	}
	wg.Wait()
	createdCount := 0
	for i := range results {
		require.NoError(t, errs[i], "every caller gets the record, none gets an error")
		if results[i] {
			createdCount++
		}
	}
	require.Equal(t, 1, createdCount, "exactly one caller creates")
	launcher.mu.Lock()
	require.Len(t, launcher.launched, 1)
	launcher.mu.Unlock()
}

func TestStartRefusesWhileBusy(t *testing.T) {
	store, err := NewStore(t.TempDir())
	require.NoError(t, err)
	launcher := &recordingLauncher{}

	first := NewOperation(ActionRestart, "test")
	_, _, err = Start(context.Background(), store, launcher, first)
	require.NoError(t, err)

	second := NewOperation(ActionRestart, "test")
	_, _, err = Start(context.Background(), store, launcher, second)
	require.ErrorIs(t, err, ErrBusy)
	require.Len(t, launcher.launched, 1)
}
