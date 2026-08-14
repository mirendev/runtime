package saga

import (
	"context"
	"errors"
	"fmt"
)

// Naming an execution after the entity it belongs to makes the record durable
// across passes, which is the point. It also means the record can outlive the
// truth: a teardown that failed without compensating anything, or a successful
// build whose resources have since disappeared. Execute will not second-guess
// either, because deciding that a recorded outcome no longer describes reality
// needs knowledge the saga framework does not have.
//
// These hand that decision to the caller that does have it. Dropping an
// execution that is absent, or that is in any other state, is not an error, so
// a caller can say what it means without first reading the record.

// DropIfFailed removes an execution that failed, so a later Execute under the
// same name runs again instead of handing back the recorded error.
//
// For a caller that owns the operation and has decided a retry is right.
// Addon teardown is the motivating case: its undos are all no-ops, so a failed
// run compensated nothing and left its resources exactly where they were, and
// a second attempt has strictly more to do rather than something to redo.
func DropIfFailed(ctx context.Context, s Storage, id string) error {
	return dropIf(ctx, s, id, StatusFailed)
}

// DropIfCompleted removes an execution that succeeded, so a later Execute under
// the same name builds again instead of reporting the old success.
//
// For a caller that knows the resources the run created are gone. Sandbox
// creation is the motivating case: containers can vanish under a live runner
// with nothing written to the entity, and the controller that notices arrives
// here wanting a build, not a receipt for one.
func DropIfCompleted(ctx context.Context, s Storage, id string) error {
	return dropIf(ctx, s, id, StatusCompleted)
}

func dropIf(ctx context.Context, s Storage, id string, want Status) error {
	exec, err := s.Get(ctx, id)
	if errors.Is(err, ErrExecutionNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("loading execution %q: %w", id, err)
	}
	if exec.Status != want {
		return nil
	}
	return s.Delete(ctx, id)
}
