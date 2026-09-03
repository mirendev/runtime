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

// newSagaControllerForResume builds a SagaSandboxController with just enough
// wired up to exercise the resume-decision logic (storage + log) without
// constructing the full inner *SandboxController (which needs a live containerd
// client). sagaResumeNeeded consults only s.storage and s.log, so this is a
// faithful stand-in for that decision.
func newSagaControllerForResume(t *testing.T) *SagaSandboxController {
	t.Helper()
	return &SagaSandboxController{
		storage: saga.NewMemoryStorage(),
		log:     slog.Default().With("module", "test"),
	}
}

func TestCreateSandboxSagaID_StableAndNamedAfterEntity(t *testing.T) {
	co := &compute.Sandbox{ID: entity.Id("sandbox/abc-123")}
	// The execution name is the stable resume key the reconciler re-enters on
	// every pass; it must be a pure function of the entity ID.
	assert.Equal(t, "create-sandbox-sandbox/abc-123", createSandboxSagaID(co))
}

// TestSagaResumeNeeded_DecidesOnForwardIncomplete verifies the routing
// decision the SagaSandboxController makes in its case-same branch. A
// surviving-container reconcile that finds an incomplete create-sandbox saga
// must route through the resume path instead of returning early; the bug
// introduced by 9bf10a18 left no route to resume legacy (empty-scope) records.
func TestSagaResumeNeeded_DecidesOnForwardIncomplete(t *testing.T) {
	ctx := context.Background()
	co := &compute.Sandbox{ID: entity.Id("sb-1")}

	cases := []struct {
		name   string
		status saga.Status
		scope  string
		want   bool
	}{
		// The bug's symptom window: a forward-running legacy saga that crashed
		// before actionUpdateSvcs persisted, with healthy containers surviving.
		{"legacy running is resumed", saga.StatusRunning, "", true},
		{"scoped running is resumed", saga.StatusRunning, "node/a", true},
		{"pending is resumed", saga.StatusPending, "", true},

		// Terminal records need no resume.
		{"completed is not resumed", saga.StatusCompleted, "", false},
		{"failed is not resumed", saga.StatusFailed, "", false},

		// A record mid-compensation is deliberately not resumed from here:
		// resuming compensation unwinds the surviving containers this process
		// did not boot, which is strictly worse than leaving a healthy record.
		{"undoing is not resumed from reconcile", saga.StatusUndoing, "", false},
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
				ExecutedActions:   map[string]*saga.ActionResult{},
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

	// No persisted execution: nothing to resume, and no error surfaced. This
	// is the common case (a sandbox whose saga already completed, or one that
	// never had a saga) and must not route through createSandboxViaSaga.
	assert.False(t, s.sagaResumeNeeded(ctx, co))
}

// TestSagaResume_DrivesLegacyRecordToCompletion is the central resume-plumbing
// proof: a legacy (empty RecoveryScope) create-sandbox saga that crashed in the
// #7->#9 window (persisted through actionSetRunning, actionUpdateSvcs not run)
// is resumed by a scoped executor's routed Execute -- the very path the
// reconciler's case-same branch now routes to. It is adopted (scope stamped),
// the already-persisted tail actions are NOT re-run, the pending tail
// (updateServices) runs exactly once, and the saga reaches StatusCompleted.
func TestSagaResume_DrivesLegacyRecordToCompletion(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()
	execID := "create-sandbox-" + h.sandboxID

	now := time.Now()
	// Persist a legacy record as if it crashed immediately after actionSetRunning
	// (#8) persisted and before actionUpdateSvcs (#9) ran -- the report's
	// missing-Endpoints symptom window. Empty RecoveryScope marks it legacy
	// (pre-9bf10a18), so startup Recover would skip it on a scoped executor.
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

	// A scoped startup Recover must make no progress on the legacy record --
	// this is the skip half of the bug, and it is the reason the reconciler
	// must route to resume. The record is left running and unscoped.
	require.NoError(t, h.executor.Recover(ctx))
	assert.Equal(t, 0, h.obs.updateSvcsCalls,
		"startup Recover must not resume a legacy unscoped record")
	stillLegacy, err := h.storage.Get(ctx, execID)
	require.NoError(t, err)
	assert.Equal(t, saga.StatusRunning, stillLegacy.Status)
	assert.Equal(t, "", stillLegacy.RecoveryScope, "Recover must not adopt a legacy record")

	// The resume route the reconciler now reaches: a routed Execute under the
	// scoped executor adopts the legacy record and drives it to completion.
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

	// Only the pending tail action ran; every already-persisted forward action
	// was skipped. In particular updateServices ran exactly once (the
	// missing-Endpoints step) and addMetrics did not run again.
	assert.Equal(t, 1, h.obs.updateSvcsCalls, "the updateServices tail action must run once")
	assert.Equal(t, 0, h.obs.addMetricsCalls, "already-completed addMetrics must not re-run")
	assert.Equal(t, 0, h.runtime.bootContainersCalls, "already-completed bootContainers must not re-run")
	assert.Equal(t, 0, h.runtime.waitForPortCalls, "already-completed waitPorts must not re-run")
	assert.Equal(t, 0, h.networking.allocateCalls, "already-completed allocNetwork must not re-run")
}
