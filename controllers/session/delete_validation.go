package session

import (
	"context"
	"errors"

	computeapi "miren.dev/runtime/api/compute"
	compute "miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/entity"
)

// ValidateSandboxDelete prevents a Session-managed sandbox from being removed
// before the runner has confirmed its containers are gone.
func ValidateSandboxDelete(ctx context.Context, stored *entity.Entity, store entity.Store) error {
	var sb compute.Sandbox
	sb.Decode(stored)
	if sb.SessionInfo.Owner == "" && sb.SessionInfo.Group == "" {
		return nil
	}
	var schedule compute.Schedule
	if terminal(sb.Status) && schedule.Is(stored) {
		schedule.Decode(stored)
		if schedule.Key.Node != "" {
			_, err := store.GetEntity(ctx, schedule.Key.Node)
			if errors.Is(err, cond.ErrNotFound{}) || errors.Is(err, entity.ErrEntityNotFound) {
				return nil // A removed node cannot leave running containers to acknowledge teardown.
			}
			if err != nil {
				return err
			}
		}
	}
	ack, err := store.GetEntity(ctx, computeapi.TeardownID(sb.ID))
	if errors.Is(err, cond.ErrNotFound{}) || errors.Is(err, entity.ErrEntityNotFound) {
		return cond.ValidationFailure("sandbox-teardown", "session sandbox must finish runner teardown before deletion")
	}
	if err != nil {
		return err
	}
	var teardown compute.SandboxTeardown
	teardown.Decode(ack)
	if teardown.Sandbox != sb.ID.String() {
		return errors.New("sandbox teardown acknowledgment belongs to another sandbox")
	}
	return nil
}
