package compute

import (
	"context"
	"fmt"
	"log/slog"

	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
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

// StopSandbox updates a sandbox status to stopped
func (c *Client) StopSandbox(ctx context.Context, sandboxID string) error {
	var sandbox compute_v1alpha.Sandbox

	// Get the current sandbox
	err := c.ec.Get(ctx, sandboxID, &sandbox)
	if err != nil {
		return fmt.Errorf("failed to get sandbox %s: %w", sandboxID, err)
	}

	// Check if already stopped or dead
	if sandbox.Status == compute_v1alpha.STOPPED || sandbox.Status == compute_v1alpha.DEAD {
		c.log.Info("Sandbox already stopped or dead", "sandbox", sandboxID, "status", sandbox.Status)
		return nil
	}

	// Update status to stopped
	sandbox.Status = compute_v1alpha.STOPPED

	// Update the entity
	err = c.ec.Update(ctx, &sandbox)
	if err != nil {
		return fmt.Errorf("failed to update sandbox %s: %w", sandboxID, err)
	}

	c.log.Info("Stopped sandbox", "sandbox", sandboxID)
	return nil
}

// DeleteSandbox deletes a sandbox, but only if it's dead
func (c *Client) DeleteSandbox(ctx context.Context, sandboxID string) error {
	var sandbox compute_v1alpha.Sandbox

	// Get the current sandbox
	err := c.ec.Get(ctx, sandboxID, &sandbox)
	if err != nil {
		return fmt.Errorf("failed to get sandbox %s: %w", sandboxID, err)
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
