package commands

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	es "miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	saga_v1alpha "miren.dev/runtime/api/saga/saga_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/saga"
)

// sagaEntityFor encodes an execution into the RPC entity the entity server
// would hand back, taking the same path EntityStorage.Save does so the test
// exercises the real round trip rather than a hand-built approximation.
func sagaEntityFor(t *testing.T, exec *saga.Execution, createdAt, updatedAt time.Time) *es.Entity {
	t.Helper()

	initialInputs, err := json.Marshal(exec.InitialInputs)
	require.NoError(t, err)
	executedActions, err := json.Marshal(exec.ExecutedActions)
	require.NoError(t, err)
	executionOrder, err := json.Marshal(exec.ExecutionOrder)
	require.NoError(t, err)

	status, ok := sagaStatusEnum(exec.Status)
	require.True(t, ok, "unknown status %q", exec.Status)

	sagaEntity := &saga_v1alpha.Saga{
		ID:                entity.Id(exec.ID),
		DefinitionName:    exec.DefinitionName,
		DefinitionVersion: int64(exec.DefinitionVersion),
		ParentExecutionId: entity.Id(exec.ParentExecutionID),
		RecoveryScope:     exec.RecoveryScope,
		Status:            status,
		InitialInputs:     initialInputs,
		ExecutedActions:   executedActions,
		ExecutionOrder:    executionOrder,
		Error:             exec.Error,
	}

	ent := entity.New(entity.DBId, entity.Id(exec.ID), sagaEntity.Encode())

	e := &es.Entity{}
	e.SetId(exec.ID)
	e.SetAttrs(ent.Attrs())
	e.SetCreatedAt(createdAt.UnixMilli())
	e.SetUpdatedAt(updatedAt.UnixMilli())

	return e
}

func sagaStatusEnum(s saga.Status) (saga_v1alpha.SagaStatus, bool) {
	switch s {
	case saga.StatusPending:
		return saga_v1alpha.PENDING, true
	case saga.StatusRunning:
		return saga_v1alpha.RUNNING, true
	case saga.StatusUndoing:
		return saga_v1alpha.UNDOING, true
	case saga.StatusCompleted:
		return saga_v1alpha.COMPLETED, true
	case saga.StatusFailed:
		return saga_v1alpha.FAILED, true
	}
	return "", false
}

// wedgedSaga is the case these commands exist for: a saga that ran two actions
// and then stopped, still claiming to be running hours later.
func wedgedSaga() *saga.Execution {
	executedAt := time.Now().Add(-3 * time.Hour)
	undoneAt := executedAt.Add(time.Minute)

	return &saga.Execution{
		ID:                "saga/sg-Wedged1",
		DefinitionName:    "provision_mysql_dedicated",
		DefinitionVersion: 2,
		RecoveryScope:     "node/runner-a",
		Status:            saga.StatusRunning,
		InitialInputs: map[string]any{
			"app_id": "app/checkout",
			"size":   float64(20),
		},
		ExecutionOrder: []string{"create_disk", "attach_disk"},
		ExecutedActions: map[string]*saga.ActionResult{
			"create_disk": {
				Output:     []byte(`{"disk_id":"disk-abc","size_gb":20}`),
				ExecutedAt: executedAt,
			},
			"attach_disk": {
				ExecutedAt: executedAt.Add(30 * time.Second),
				UndoneAt:   &undoneAt,
				Error:      "sandbox not ready",
			},
		},
	}
}

func TestDecodeSagaRecordRoundTrip(t *testing.T) {
	exec := wedgedSaga()
	created := time.Now().Add(-3 * time.Hour).Truncate(time.Millisecond)
	updated := time.Now().Add(-2 * time.Hour).Truncate(time.Millisecond)

	record, err := decodeSagaRecord(sagaEntityFor(t, exec, created, updated))
	require.NoError(t, err)

	assert.Equal(t, exec.ID, record.exec.ID)
	assert.Equal(t, "provision_mysql_dedicated", record.exec.DefinitionName)
	assert.Equal(t, 2, record.exec.DefinitionVersion)
	assert.Equal(t, saga.StatusRunning, record.exec.Status)
	assert.Equal(t, "node/runner-a", record.exec.RecoveryScope)
	assert.Equal(t, []string{"create_disk", "attach_disk"}, record.exec.ExecutionOrder)
	assert.Equal(t, "attach_disk", record.lastAction())
	assert.Equal(t, "app/checkout", record.exec.InitialInputs["app_id"])

	// The saga schema does not persist Execution.CreatedAt/UpdatedAt, so these
	// have to come from the entity store's own metadata. This is what makes the
	// UPDATED column meaningful.
	assert.True(t, created.Equal(record.createdAt), "created %v != %v", created, record.createdAt)
	assert.True(t, updated.Equal(record.updatedAt), "updated %v != %v", updated, record.updatedAt)

	result := record.exec.ExecutedActions["attach_disk"]
	require.NotNil(t, result)
	assert.Equal(t, "sandbox not ready", result.Error)
	require.NotNil(t, result.UndoneAt)
}

func TestDecodeSagaRecordRejectsNonSaga(t *testing.T) {
	ent := entity.New(entity.DBId, entity.Id("app/checkout"))

	e := &es.Entity{}
	e.SetId("app/checkout")
	e.SetAttrs(ent.Attrs())

	_, err := decodeSagaRecord(e)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "is not a saga")
}

func TestPrintSagaShow(t *testing.T) {
	record, err := decodeSagaRecord(sagaEntityFor(t, wedgedSaga(),
		time.Now().Add(-3*time.Hour), time.Now().Add(-2*time.Hour)))
	require.NoError(t, err)

	child, err := decodeSagaRecord(sagaEntityFor(t, &saga.Execution{
		ID:                "saga/sg-Child1",
		DefinitionName:    "format_disk",
		Status:            saga.StatusCompleted,
		ParentExecutionID: "saga/sg-Wedged1",
	}, time.Now(), time.Now()))
	require.NoError(t, err)

	var buf bytes.Buffer
	ctx := &Context{Stdout: &buf}
	printSagaShow(ctx, record, []*sagaRecord{child}, false)
	out := buf.String()

	// The table and header drop the kind namespace; lookups put it back.
	assert.Contains(t, out, "sg-Wedged1")
	assert.NotContains(t, out, "saga/sg-Wedged1")
	assert.Contains(t, out, "provision_mysql_dedicated (v2)")
	assert.Contains(t, out, "running")
	assert.Contains(t, out, "node/runner-a")
	assert.Contains(t, out, "app_id")

	// Both actions, in execution order, with their outcomes.
	assert.Contains(t, out, "1. create_disk")
	assert.Contains(t, out, "2. attach_disk")
	assert.Contains(t, out, "disk-abc")
	assert.Contains(t, out, "error: sandbox not ready")
	assert.Contains(t, out, "undone")

	assert.Contains(t, out, "sg-Child1")
	assert.Contains(t, out, "format_disk")

	assert.Less(t, strings.Index(out, "create_disk"), strings.Index(out, "attach_disk"))
}

func TestPrintSagaShowNoActions(t *testing.T) {
	record, err := decodeSagaRecord(sagaEntityFor(t, &saga.Execution{
		ID:             "saga/sg-Fresh1",
		DefinitionName: "build_and_deploy",
		Status:         saga.StatusPending,
	}, time.Now(), time.Now()))
	require.NoError(t, err)

	var buf bytes.Buffer
	ctx := &Context{Stdout: &buf}
	printSagaShow(ctx, record, nil, false)

	assert.Contains(t, buf.String(), "Actions: none executed yet")
}

func TestSagaActionOrderIncludesResultsMissingFromOrder(t *testing.T) {
	exec := &saga.Execution{
		ExecutionOrder: []string{"first"},
		ExecutedActions: map[string]*saga.ActionResult{
			"first":  {},
			"orphan": {},
		},
	}

	// A result the execution order never recorded is a corrupt record, and a
	// debug command that hid it would hide the evidence.
	assert.Equal(t, []string{"first", "orphan"}, sagaActionOrder(exec))
}

func TestFormatSagaJSONTruncates(t *testing.T) {
	raw := []byte(`{"blob":"` + strings.Repeat("x", 500) + `"}`)

	short := formatSagaJSON(raw, false, 0)
	assert.Less(t, len(short), 200)
	assert.Contains(t, short, "--full")
	assert.NotContains(t, short, "\n")

	full := formatSagaJSON(raw, true, 0)
	assert.Contains(t, full, strings.Repeat("x", 500))

	assert.Equal(t, "-", formatSagaJSON(nil, false, 0))
}

func TestFormatSagaJSONPassesThroughInvalidJSON(t *testing.T) {
	// Outputs are written by json.Marshal so this should not happen, but a
	// debug command should show whatever is actually stored.
	assert.Equal(t, "not json", formatSagaJSON([]byte("not json"), false, 0))
}

func TestParseSagaStatus(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want saga.Status
	}{
		{"running", saga.StatusRunning},
		{"FAILED", saga.StatusFailed},
		{"  undoing  ", saga.StatusUndoing},
	} {
		got, err := parseSagaStatus(tc.in)
		require.NoError(t, err, tc.in)
		assert.Equal(t, tc.want, got)
	}

	_, err := parseSagaStatus("wedged")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown saga status")
	assert.Contains(t, err.Error(), "undoing")
}

func TestSagaStatusIndexCoversEveryStatus(t *testing.T) {
	for _, s := range []saga.Status{
		saga.StatusPending, saga.StatusRunning, saga.StatusUndoing,
		saga.StatusCompleted, saga.StatusFailed,
	} {
		_, err := sagaStatusIndex(s)
		require.NoError(t, err, "no index for %s", s)
	}
}

func TestSagaShowJSON(t *testing.T) {
	record, err := decodeSagaRecord(sagaEntityFor(t, wedgedSaga(),
		time.Now().Add(-3*time.Hour), time.Now().Add(-2*time.Hour)))
	require.NoError(t, err)

	t.Run("describes every action", func(t *testing.T) {
		out := newSagaShowJSON(record, nil)

		require.Len(t, out.Actions, 2)
		assert.Equal(t, "create_disk", out.Actions[0].Name)
		assert.Equal(t, 1, out.Actions[0].Order)
		assert.Equal(t, "sandbox not ready", out.Actions[1].Error)
		assert.NotEmpty(t, out.Actions[1].UndoneAt)
	})

	t.Run("carries raw output as JSON, not a string", func(t *testing.T) {
		out := newSagaShowJSON(record, nil)

		encoded, err := json.Marshal(out)
		require.NoError(t, err)
		// Embedded as an object rather than an escaped string.
		assert.Contains(t, string(encoded), `"output":{"disk_id":"disk-abc"`)
	})

	t.Run("record is whole, with no dependence on --full", func(t *testing.T) {
		// JSON used to emit initial_inputs unconditionally while withholding
		// action outputs unless --full, so the default handed a machine a
		// partial record with nothing marking what was missing. Both halves
		// go out together now, and nothing about the JSON keys off --full.
		out := newSagaShowJSON(record, nil)

		assert.NotEmpty(t, out.InitialInputs, "inputs should be present")
		require.NotEmpty(t, out.Actions)
		assert.NotEmpty(t, out.Actions[0].Output, "outputs should be present too")
		assert.True(t, json.Valid(out.Actions[0].Output))
	})

	t.Run("timestamps are RFC3339", func(t *testing.T) {
		out := newSagaShowJSON(record, nil)

		_, err := time.Parse(time.RFC3339, out.CreatedAt)
		assert.NoError(t, err)
		_, err = time.Parse(time.RFC3339, out.UpdatedAt)
		assert.NoError(t, err)
		_, err = time.Parse(time.RFC3339, out.Actions[0].ExecutedAt)
		assert.NoError(t, err)
	})
}

func TestSagaListJSONReportsProgress(t *testing.T) {
	record, err := decodeSagaRecord(sagaEntityFor(t, wedgedSaga(),
		time.Now().Add(-3*time.Hour), time.Now().Add(-2*time.Hour)))
	require.NoError(t, err)

	item := newSagaListJSON(record)

	assert.Equal(t, "saga/sg-Wedged1", item.ID)
	assert.Equal(t, "running", item.Status)
	assert.Equal(t, "node/runner-a", item.RecoveryScope)
	assert.Equal(t, 2, item.ActionCount)
	assert.Equal(t, "attach_disk", item.LastAction)
}

func TestSagaIDCandidates(t *testing.T) {
	// The bare name is what a listing prints, so it has to resolve.
	assert.Equal(t, []string{"saga/sg-Abc", "sg-Abc"}, sagaIDCandidates("sg-Abc"))

	// A fully qualified ID is taken at face value.
	assert.Equal(t, []string{"saga/sg-Abc"}, sagaIDCandidates("saga/sg-Abc"))

	// IDs minted before the namespace existed are still typeable, which is why
	// the unqualified form stays in the candidate list rather than being
	// rewritten away.
	assert.Equal(t, []string{"saga/sagaOldStyle", "sagaOldStyle"}, sagaIDCandidates("sagaOldStyle"))

	// Something addressing another kind is passed through, so the error names
	// what the user actually asked for.
	assert.Equal(t, []string{"app/checkout"}, sagaIDCandidates("app/checkout"))
}
