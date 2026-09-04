package sandbox

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	compute "miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/saga"
)

// newSagaControllerForResume wires up only what sagaResumeNeeded reads
// (storage + log), so no live containerd client is needed.
func newSagaControllerForResume(t *testing.T) *SagaSandboxController {
	t.Helper()
	return &SagaSandboxController{
		storage: saga.NewMemoryStorage(),
		log:     slog.Default().With("module", "test"),
	}
}

func TestCreateSandboxSagaID_StableAndNamedAfterEntity(t *testing.T) {
	co := &compute.Sandbox{ID: entity.Id("sandbox/abc-123")}
	// The resume key must be a pure function of the entity ID.
	assert.Equal(t, "create-sandbox-sandbox/abc-123", createSandboxSagaID(co))
}

// The routing decision the case-same branch makes: which incomplete records a
// surviving-container reconcile may resume.
func TestSagaResumeNeeded_DecidesOnForwardIncomplete(t *testing.T) {
	ctx := context.Background()
	co := &compute.Sandbox{ID: entity.Id("sb-1")}

	// The only shape safe to resume: containers up and recorded as such.
	bootedActions := map[string]*saga.ActionResult{actionBootCtrs: {}}

	cases := []struct {
		name     string
		status   saga.Status
		scope    string
		executed map[string]*saga.ActionResult
		want     bool
	}{
		// The bug's symptom window.
		{"legacy running past boot is resumed", saga.StatusRunning, "", bootedActions, true},
		{"scoped running past boot is resumed", saga.StatusRunning, "node/a", bootedActions, true},
		{"pending past boot is resumed", saga.StatusPending, "", bootedActions, true},

		// Terminal records need no resume.
		{"completed is not resumed", saga.StatusCompleted, "", bootedActions, false},
		{"failed is not resumed", saga.StatusFailed, "", bootedActions, false},

		// Resuming compensation would unwind the surviving containers.
		{"undoing is not resumed from reconcile", saga.StatusUndoing, "", bootedActions, false},

		// Would resume into createContainer/bootContainers against a live
		// sandbox, and an error there unwinds the saga and destroys it.
		{"running before boot is not resumed", saga.StatusRunning, "", map[string]*saga.ActionResult{}, false},
		{"running with only earlier actions is not resumed", saga.StatusRunning, "",
			map[string]*saga.ActionResult{actionAllocNetwork: {}, actionCreateCtr: {}}, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newSagaControllerForResume(t)
			require.NoError(t, s.storage.Save(ctx, &saga.Execution{
				ID:                createSandboxSagaID(co),
				DefinitionName:    sagaCreateSandbox,
				DefinitionVersion: 1,
				InitialInputs:     map[string]any{"sandbox_id": co.ID.String()},
				RecoveryScope:     tc.scope,
				Status:            tc.status,
				ExecutedActions:   tc.executed,
				ExecutionOrder:    []string{},
				CreatedAt:         time.Now(),
				UpdatedAt:         time.Now(),
			}))

			assert.Equal(t, tc.want, s.sagaResumeNeeded(ctx, co),
				"sagaResumeNeeded must report %v for status %q (scope %q)", tc.want, tc.status, tc.scope)
		})
	}
}

func TestSagaResumeNeeded_NoRecord(t *testing.T) {
	ctx := context.Background()
	co := &compute.Sandbox{ID: entity.Id("sb-none")}
	s := newSagaControllerForResume(t)

	// The common case: nothing to resume, and no error surfaced.
	assert.False(t, s.sagaResumeNeeded(ctx, co))
}

// The resume plumbing end to end: a legacy record that startup Recover skips is
// adopted by a routed Execute, its persisted actions are not re-run, and the
// pending tail runs exactly once.
func TestSagaResume_DrivesLegacyRecordToCompletion(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()
	execID := "create-sandbox-" + h.sandboxID

	now := time.Now()
	// Crashed after actionSetRunning, before actionUpdateSvcs. Empty
	// RecoveryScope marks it legacy, so a scoped Recover skips it.
	legacy := &saga.Execution{
		ID:                execID,
		DefinitionName:    sagaCreateSandbox,
		DefinitionVersion: 1,
		InitialInputs:     map[string]any{"sandbox_id": h.sandboxID},
		RecoveryScope:     "",
		Status:            saga.StatusRunning,
		ExecutedActions: map[string]*saga.ActionResult{
			actionAllocNetwork: {Output: []byte(`{"addresses":["10.0.0.5/32"]}`), ExecutedAt: now},
			actionPatchNetwork: {Output: []byte(`{"revision":2}`), ExecutedAt: now},
			actionCreateCtr:    {Output: []byte(`{"container_id":"sandbox.test-sandbox-1_pause"}`), ExecutedAt: now},
			actionBootTask:     {Output: []byte(`{"task_pid":4242,"cgroups":"/sys/fs/cgroup/sandbox/test"}`), ExecutedAt: now},
			actionBootCtrs: {
				Output:     []byte(`{"wait_port_ids":["web"],"wait_port_ports":[8080],"port_wait_timeout":"","all_cgroups":{"":"/sys/fs/cgroup/sandbox/test"}}`),
				ExecutedAt: now,
			},
			actionAddMetrics: {Output: []byte(`{"log_entity":"test-sandbox-1"}`), ExecutedAt: now},
			actionWaitPorts:  {Output: []byte(`{"observed_ports":[]}`), ExecutedAt: now},
			actionSetRunning: {Output: []byte(`{}`), ExecutedAt: now},
		},
		ExecutionOrder: []string{
			actionAllocNetwork, actionPatchNetwork, actionCreateCtr, actionBootTask,
			actionBootCtrs, actionAddMetrics, actionWaitPorts, actionSetRunning,
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	require.NoError(t, h.storage.Save(ctx, legacy))

	// The skip half of the bug: Recover makes no progress on a legacy record.
	require.NoError(t, h.executor.Recover(ctx))
	assert.Equal(t, 0, h.obs.updateSvcsCalls,
		"startup Recover must not resume a legacy unscoped record")
	stillLegacy, err := h.storage.Get(ctx, execID)
	require.NoError(t, err)
	assert.Equal(t, saga.StatusRunning, stillLegacy.Status)
	assert.Equal(t, "", stillLegacy.RecoveryScope, "Recover must not adopt a legacy record")

	// The resume route: a routed Execute adopts the record and completes it.
	require.NoError(t, h.executor.Start(sagaCreateSandbox).
		Input("sandbox_id", h.sandboxID).
		WithID(execID).
		Execute(ctx))

	resumed, err := h.storage.Get(ctx, execID)
	require.NoError(t, err)
	assert.Equal(t, saga.StatusCompleted, resumed.Status,
		"the legacy record must be driven to completion by the routed resume")
	assert.Equal(t, "node/test-node", resumed.RecoveryScope,
		"the routed resume must adopt the legacy record by stamping the scope")

	// Only the pending tail ran; persisted actions were skipped.
	assert.Equal(t, 1, h.obs.updateSvcsCalls, "the updateServices tail action must run once")
	assert.Equal(t, 0, h.obs.addMetricsCalls, "already-completed addMetrics must not re-run")
	assert.Equal(t, 0, h.runtime.bootContainersCalls, "already-completed bootContainers must not re-run")
	assert.Equal(t, 0, h.runtime.waitForPortCalls, "already-completed waitPorts must not re-run")
	assert.Equal(t, 0, h.networking.allocateCalls, "already-completed allocNetwork must not re-run")
}
