//go:build linux

package server

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"miren.dev/runtime/pkg/serverconfig"
	"miren.dev/runtime/pkg/serverlifecycle"
)

func TestDataRestoreBootIsQuietWithoutLifecycleDir(t *testing.T) {
	b := &dataRestoreBoot{inputs: dataRestoreInputs{lifecycleDir: filepath.Join(t.TempDir(), "missing")}}
	require.NoError(t, b.start(t.Context(), containerdBootOutput{}, observabilityBootOutput{log: slog.Default()}))
}

func TestDataRestoreBootIgnoresOperationsWithoutARequest(t *testing.T) {
	dir := t.TempDir()
	store, err := serverlifecycle.NewStore(dir)
	require.NoError(t, err)
	op := serverlifecycle.NewOperation(serverlifecycle.ActionUpgrade, "test")
	op.TargetVersion = "v2.0.0"
	op.Phase = serverlifecycle.PhaseVerifying
	op.BackupRef = "/nope"
	require.NoError(t, store.Create(op))

	b := &dataRestoreBoot{inputs: dataRestoreInputs{lifecycleDir: dir}}
	require.NoError(t, b.start(t.Context(), containerdBootOutput{}, observabilityBootOutput{log: slog.Default()}))
	_, err = store.ReadRestoreResult(op.ID)
	require.ErrorIs(t, err, serverlifecycle.ErrNotFound)
}

// A restore the server cannot perform is answered and then refused: the
// answer gives the executor the reason, and the refusal keeps the old build
// off the data the rollback was meant to replace until someone abandons it.
func TestDataRestoreBootRecordsFailureAndRefusesToStart(t *testing.T) {
	dir := t.TempDir()
	store, err := serverlifecycle.NewStore(dir)
	require.NoError(t, err)
	op := serverlifecycle.NewOperation(serverlifecycle.ActionUpgrade, "test")
	op.TargetVersion = "v2.0.0"
	op.Phase = serverlifecycle.PhaseRollingBack
	op.BackupRef = filepath.Join(dir, "backups", op.ID+".etcd.db")
	op.DataRestore = &serverlifecycle.DataRestore{BackupRef: op.BackupRef}
	require.NoError(t, store.Create(op))

	embedded := false
	b := &dataRestoreBoot{inputs: dataRestoreInputs{
		lifecycleDir: dir,
		dataPath:     t.TempDir(),
		etcd:         serverconfig.EtcdConfig{StartEmbedded: &embedded},
	}}
	err = b.start(t.Context(), containerdBootOutput{}, observabilityBootOutput{log: slog.Default()})
	require.ErrorContains(t, err, "not embedded")

	result, err := store.ReadRestoreResult(op.ID)
	require.NoError(t, err)
	require.Equal(t, op.ID, result.OperationID)
	require.Equal(t, op.BackupRef, result.BackupRef)
	require.Contains(t, result.Error, "not embedded")
	require.True(t, result.RestoredAt.IsZero())

	// Still pending: the next boot tries again, until abandoned.
	pending, err := store.PendingRestore()
	require.NoError(t, err)
	require.NotNil(t, pending)
	_, err = serverlifecycle.Abandon(store, op.ID)
	require.NoError(t, err)
	require.NoError(t, b.start(t.Context(), containerdBootOutput{}, observabilityBootOutput{log: slog.Default()}))
}

// The build being rolled back from may boot while the request is pending;
// it leaves the request alone rather than restore data it would migrate
// again before the intended build gets to it.
func TestDataRestoreBootLeavesRequestsForOtherBuilds(t *testing.T) {
	dir := t.TempDir()
	store, err := serverlifecycle.NewStore(dir)
	require.NoError(t, err)
	op := serverlifecycle.NewOperation(serverlifecycle.ActionUpgrade, "test")
	op.TargetVersion = "v2.0.0"
	op.Phase = serverlifecycle.PhaseRollingBack
	op.BackupRef = "/nope"
	op.DataRestore = &serverlifecycle.DataRestore{BackupRef: op.BackupRef, ForVersion: "v1.0.0", ForCommit: "c-v1"}
	require.NoError(t, store.Create(op))

	b := &dataRestoreBoot{inputs: dataRestoreInputs{lifecycleDir: dir, buildVersion: "v2.0.0", buildCommit: "c-v2"}}
	require.NoError(t, b.start(t.Context(), containerdBootOutput{}, observabilityBootOutput{log: slog.Default()}))
	_, err = store.ReadRestoreResult(op.ID)
	require.ErrorIs(t, err, serverlifecycle.ErrNotFound, "not answered, still pending for v1")
	pending, err := store.PendingRestore()
	require.NoError(t, err)
	require.NotNil(t, pending)
}

// A store that cannot be read might be hiding a request, so the server does
// not guess: it fails closed rather than let etcd open the data directory.
func TestDataRestoreBootFailsClosedOnUnreadableStore(t *testing.T) {
	dir := t.TempDir()
	store, err := serverlifecycle.NewStore(dir)
	require.NoError(t, err)
	op := serverlifecycle.NewOperation(serverlifecycle.ActionUpgrade, "test")
	op.TargetVersion = "v2.0.0"
	op.Phase = serverlifecycle.PhaseRollingBack
	op.BackupRef = "/nope"
	op.DataRestore = &serverlifecycle.DataRestore{BackupRef: op.BackupRef}
	require.NoError(t, store.Create(op))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "restores"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "restores", op.ID+".json"), []byte("not json"), 0o644))

	b := &dataRestoreBoot{inputs: dataRestoreInputs{lifecycleDir: dir}}
	err = b.start(t.Context(), containerdBootOutput{}, observabilityBootOutput{log: slog.Default()})
	require.ErrorContains(t, err, "check for a pending data restore")
}
