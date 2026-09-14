package serverlifecycle

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	"miren.dev/runtime/api/server/server_v1alpha"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/rpc"
	lifecycle "miren.dev/runtime/pkg/serverlifecycle"
)

type noopCall struct{}

func (noopCall) NewClient(*rpc.Capability) rpc.Client         { return nil }
func (noopCall) Args(any)                                     {}
func (noopCall) Results(any)                                  {}
func (noopCall) NewCapability(*rpc.Interface) *rpc.Capability { return nil }
func (noopCall) IsAuthenticated() bool                        { return true }

// Server-side args only have getters and results only setters, so tests go
// through the JSON codec the wire uses.
func setArgs(t *testing.T, args interface{ UnmarshalJSON([]byte) error }, v any) {
	t.Helper()
	raw, err := json.Marshal(v)
	require.NoError(t, err)
	require.NoError(t, args.UnmarshalJSON(raw))
}

func readResults(t *testing.T, results interface{ MarshalJSON() ([]byte, error) }, into any) {
	t.Helper()
	raw, err := results.MarshalJSON()
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, into))
}

type operationResult struct {
	Operation *server_v1alpha.Operation `json:"operation"`
}

type recordingLauncher struct{ launched []string }

func (l *recordingLauncher) Launch(_ context.Context, id string) error {
	l.launched = append(l.launched, id)
	return nil
}

func newTestServer(t *testing.T) (*Server, *lifecycle.Store, *recordingLauncher) {
	t.Helper()
	store, err := lifecycle.NewStore(t.TempDir())
	require.NoError(t, err)
	launcher := &recordingLauncher{}
	return NewServer(store, launcher, slog.Default()), store, launcher
}

func TestListAndGetReadTheLedger(t *testing.T) {
	srv, store, _ := newTestServer(t)
	op := lifecycle.NewOperation(lifecycle.ActionUpgrade, "cli")
	op.TargetVersion = "v0.15.0"
	require.NoError(t, store.Create(op))
	op.Phase = lifecycle.PhaseSucceeded
	op.NewVersion = "v0.15.0"
	require.NoError(t, store.Update(op))

	list := &server_v1alpha.ServerLifecycleList{Call: noopCall{}}
	require.NoError(t, srv.List(context.Background(), list))
	var listed struct {
		Operations []*server_v1alpha.Operation `json:"operations"`
	}
	readResults(t, list.Results(), &listed)
	require.Len(t, listed.Operations, 1)
	got := FromRPC(listed.Operations[0])
	require.Equal(t, op.ID, got.ID)
	require.Equal(t, lifecycle.PhaseSucceeded, got.Phase)
	require.NotNil(t, got.FinishedAt)
	require.True(t, got.Succeeded())

	get := &server_v1alpha.ServerLifecycleGet{Call: noopCall{}}
	setArgs(t, get.Args(), map[string]any{"id": op.ID})
	require.NoError(t, srv.Get(context.Background(), get))
	var one operationResult
	readResults(t, get.Results(), &one)
	require.Equal(t, "v0.15.0", one.Operation.NewVersion())

	missing := &server_v1alpha.ServerLifecycleGet{Call: noopCall{}}
	setArgs(t, missing.Args(), map[string]any{"id": lifecycle.NewID()})
	require.ErrorIs(t, srv.Get(context.Background(), missing), cond.ErrNotFound{})
}

// The verbatim record is what keeps the RPC honest as the executor grows the
// operation: a field the schema does not name still reaches a client that
// decodes the record.
func TestRecordCarriesFieldsTheSchemaDoesNotName(t *testing.T) {
	srv, store, _ := newTestServer(t)
	op := lifecycle.NewOperation(lifecycle.ActionRestart, "cli")
	require.NoError(t, store.Create(op))

	get := &server_v1alpha.ServerLifecycleGet{Call: noopCall{}}
	setArgs(t, get.Args(), map[string]any{"id": op.ID})
	require.NoError(t, srv.Get(context.Background(), get))
	var one operationResult
	readResults(t, get.Results(), &one)
	require.True(t, one.Operation.HasRecord())

	// Stand in for a field added later: it survives the round trip untouched.
	var raw map[string]any
	require.NoError(t, json.Unmarshal([]byte(one.Operation.Record()), &raw))
	raw["backup_ref"] = "snap-1"
	edited, err := json.Marshal(raw)
	require.NoError(t, err)
	one.Operation.SetRecord(string(edited))
	decoded := FromRPC(one.Operation)
	require.Equal(t, op.ID, decoded.ID)
	require.Equal(t, lifecycle.PhasePending, decoded.Phase)
	// FromRPC re-encoding preserves the unknown field only if the client's
	// Operation type knows it; what this guarantees is that the record itself
	// is the transport, so a client on a newer build sees the field.
	var again map[string]any
	require.NoError(t, json.Unmarshal([]byte(one.Operation.Record()), &again))
	require.Equal(t, "snap-1", again["backup_ref"])

	// Without a record, the typed fields still decode.
	one.Operation.SetRecord("")
	fallback := FromRPC(one.Operation)
	require.Equal(t, op.ID, fallback.ID)
	require.Equal(t, lifecycle.ActionRestart, fallback.Action)
}

func TestStartRecordsCallerAndLaunches(t *testing.T) {
	srv, store, launcher := newTestServer(t)
	ctx := rpc.ContextWithIdentity(context.Background(), &rpc.Identity{Subject: "paul@example.com"})

	start := &server_v1alpha.ServerLifecycleStart{Call: noopCall{}}
	setArgs(t, start.Args(), map[string]any{"action": "restart", "ready_timeout_seconds": 90})
	require.NoError(t, srv.Start(ctx, start))

	var started operationResult
	readResults(t, start.Results(), &started)
	id := started.Operation.Id()
	require.NoError(t, lifecycle.ValidateID(id))
	require.Equal(t, []string{id}, launcher.launched)
	stored, err := store.Get(id)
	require.NoError(t, err)
	require.Equal(t, "paul@example.com", stored.RequestedBy)
	require.Equal(t, 90, stored.ReadyTimeoutSeconds)

	// Busy: the first is still pending.
	again := &server_v1alpha.ServerLifecycleStart{Call: noopCall{}}
	setArgs(t, again.Args(), map[string]any{"action": "restart"})
	require.ErrorIs(t, srv.Start(ctx, again), lifecycle.ErrBusy)

	// Same id: the record, not a second launch.
	retry := &server_v1alpha.ServerLifecycleStart{Call: noopCall{}}
	setArgs(t, retry.Args(), map[string]any{"id": id, "action": "restart"})
	require.NoError(t, srv.Start(ctx, retry))
	var retried operationResult
	readResults(t, retry.Results(), &retried)
	require.Equal(t, id, retried.Operation.Id())
	require.Len(t, launcher.launched, 1)
}

func TestStartValidatesTheRequest(t *testing.T) {
	srv, _, launcher := newTestServer(t)
	upgrade := &server_v1alpha.ServerLifecycleStart{Call: noopCall{}}
	setArgs(t, upgrade.Args(), map[string]any{"action": "upgrade"})
	err := srv.Start(context.Background(), upgrade)
	require.Error(t, err)
	require.Contains(t, err.Error(), "no target version")

	bogus := &server_v1alpha.ServerLifecycleStart{Call: noopCall{}}
	setArgs(t, bogus.Args(), map[string]any{"action": "explode"})
	err = srv.Start(context.Background(), bogus)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown action")
	require.Empty(t, launcher.launched)
}
