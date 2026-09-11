package sandbox

import (
	"context"
	"errors"
	"fmt"

	compute "miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/saga"
)

// createSandboxSagaID is the durable execution name for a sandbox's
// create-sandbox saga. Naming it after the entity makes a re-entered reconcile
// pass resume the same run rather than starting a second one.
func createSandboxSagaID(co *compute.Sandbox) string {
	return fmt.Sprintf("create-sandbox-%s", co.ID)
}

// sagaResumeNeeded reports whether a create-sandbox record is safe to resume
// against containers that are already healthy.
//
// Requiring actionBootCtrs is what keeps this safe: it confines the resume to
// the tail (add-metrics, wait-ports, set-running, update-services), all of
// which tolerate re-execution. Resuming earlier would re-run createContainer or
// bootContainers against a live sandbox, and since every action here has an
// Undo, one that errored would unwind the saga, destroy the healthy containers,
// and leave the sandbox DEAD. Undoing records are skipped for the same reason.
func (c *SandboxController) sagaResumeNeeded(ctx context.Context, co *compute.Sandbox) bool {
	exec, err := c.sagaStorage.Get(ctx, createSandboxSagaID(co))
	if err != nil {
		if !errors.Is(err, saga.ErrExecutionNotFound) {
			c.Log.Warn("checking for incomplete create-sandbox saga",
				"id", co.ID, "error", err)
		}
		return false
	}

	if exec.Status != saga.StatusPending && exec.Status != saga.StatusRunning {
		return false
	}

	if _, booted := exec.ExecutedActions[actionBootCtrs]; !booted {
		c.Log.Debug("not resuming create-sandbox saga: containers survive but the record predates boot-containers",
			"id", co.ID, "status", exec.Status)
		return false
	}
	return true
}

// createSandboxViaSaga runs sandbox creation as a saga for crash recovery.
// resuming marks the call as adopting a record whose containers are still alive.
func (c *SandboxController) createSandboxViaSaga(ctx context.Context, co *compute.Sandbox, resuming bool) error {
	c.Log.Info("creating sandbox via saga", "id", co.ID)

	execID := createSandboxSagaID(co)

	// With the containers missing, a completed record describes resources that
	// are no longer there: left in place it would resume straight to success
	// and the sandbox would never be rebuilt. When resuming, the containers are
	// alive, and dropping the record would restart the saga from alloc-network
	// underneath them.
	if !resuming {
		if err := saga.DropIfCompleted(ctx, c.sagaStorage, execID); err != nil {
			return fmt.Errorf("clearing stale creation record: %w", err)
		}
	}

	err := c.executor.Start(sagaCreateSandbox).
		Input("sandbox_id", co.ID.String()).
		WithID(execID).
		Execute(ctx)

	if errors.Is(err, saga.ErrExecutionInProgress) {
		// This controller keeps one executor for its lifetime, so the claim is
		// real here: an earlier pass is still driving this creation. Nothing
		// has failed, so leave the sandbox PENDING for the reconciler rather
		// than killing it over work that is still going.
		c.Log.Debug("sandbox creation already in flight", "id", co.ID)
		return nil
	}

	if err != nil {
		c.Log.Error("saga sandbox creation failed, marking DEAD", "id", co.ID, "error", err)

		// Saga compensating actions handle resource cleanup. The controller
		// owns the domain-level outcome: mark the sandbox DEAD so the pool
		// replaces it rather than retrying the same entity.
		// NOTE: this runs at the call site, so a crash between saga completion
		// and this patch leaves the entity PENDING (retried by reconciler).
		// Durable saga outcome declaration is future work.
		patchAttrs := entity.New(
			entity.Ref(entity.DBId, co.ID),
			(&compute.Sandbox{Status: compute.DEAD}).Encode,
		)
		if _, patchErr := c.ops.PatchSandbox(ctx, patchAttrs.Attrs(), 0); patchErr != nil {
			c.Log.Error("failed to mark sandbox DEAD after saga failure", "id", co.ID, "error", patchErr)
		}

		return fmt.Errorf("saga sandbox creation failed: %w", err)
	}

	return nil
}
