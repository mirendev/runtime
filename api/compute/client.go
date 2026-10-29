package compute

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/rpc"
)

// Client provides a domain-specific client for Sandbox entities
type Client struct {
	log *slog.Logger
	ec  *entityserver.Client
}

// NewClient creates a new Compute client from an RPC client
func NewClient(log *slog.Logger, client rpc.Client) *Client {
	eac := entityserver_v1alpha.NewEntityAccessClient(client)
	entityClient := entityserver.NewClient(log, eac)

	return &Client{
		log: log.With("module", "compute-client"),
		ec:  entityClient,
	}
}

func (c *Client) getSandbox(ctx context.Context, sandboxID string) (*compute_v1alpha.Sandbox, int64, error) {
	var sandbox compute_v1alpha.Sandbox

	// Try as name first, then as ID
	record, err := c.ec.GetWithEntity(ctx, sandboxID, &sandbox)
	if err != nil {
		if !errors.Is(err, cond.ErrNotFound{}) {
			return nil, 0, err
		}
		// If Get fails, try GetById in case the user provided an unprefixed ID
		record, err = c.ec.GetByIdWithEntity(ctx, entity.Id(sandboxID), &sandbox)
		if err != nil {
			return nil, 0, fmt.Errorf("sandbox %q not found (tried as name and as ID): %w", sandboxID, err)
		}
	}

	return &sandbox, record.Revision(), nil
}

// StopSandbox updates a sandbox status to stopped
func (c *Client) StopSandbox(ctx context.Context, sandboxID string) error {
	sandbox, revision, err := c.getSandbox(ctx, sandboxID)
	if err != nil {
		return err
	}

	// Check if already stopped or dead
	if sandbox.Status == compute_v1alpha.STOPPED || sandbox.Status == compute_v1alpha.DEAD {
		c.log.Info("Sandbox already stopped or dead", "sandbox", sandboxID, "status", sandbox.Status)
		return nil
	}

	// A runner may have marked it dead since the read. Do not resurrect it or
	// overwrite any runner-owned fields, and let a revision conflict surface.
	err = c.ec.Patch(ctx, sandbox.ID, revision,
		entity.Ref(compute_v1alpha.SandboxStatusId, compute_v1alpha.SandboxStatusStoppedId))
	if err != nil {
		return fmt.Errorf("failed to update sandbox %s: %w", sandboxID, err)
	}

	c.log.Info("Stopped sandbox", "sandbox", sandboxID)
	return nil
}

// DeleteSandbox deletes a sandbox, but only if it's dead
func (c *Client) DeleteSandbox(ctx context.Context, sandboxID string) error {
	sandbox, _, err := c.getSandbox(ctx, sandboxID)
	if err != nil {
		return err
	}

	// Check if sandbox is dead
	if sandbox.Status != compute_v1alpha.DEAD {
		return fmt.Errorf("cannot delete sandbox %s: status is %s (must be dead)", sandboxID, sandbox.Status)
	}

	// Delete the sandbox
	err = c.ec.Delete(ctx, sandbox.ID)
	if err != nil {
		return fmt.Errorf("failed to delete sandbox %s: %w", sandboxID, err)
	}

	c.log.Info("Deleted sandbox", "sandbox", sandboxID)
	return nil
}
