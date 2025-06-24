package app

import (
	"context"
	"fmt"
	"log/slog"

	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/api/ingress/ingress_v1alpha"
	"miren.dev/runtime/pkg/rpc"
)

// Client provides a domain-specific client for App entities
type Client struct {
	log          *slog.Logger
	entityClient *entityserver.Client
}

// NewClient creates a new App client from an RPC client
func NewClient(ctx context.Context, log *slog.Logger, client rpc.Client) (*Client, error) {
	// Get the entity access client
	eac := entityserver_v1alpha.NewEntityAccessClient(client)

	// Create entity client wrapper
	entityClient := entityserver.NewClient(log, eac)

	return &Client{
		log:          log,
		entityClient: entityClient,
	}, nil
}

// Create creates a new app entity
func (c *Client) Create(ctx context.Context, name string) (*core_v1alpha.App, error) {
	app := &core_v1alpha.App{}

	// Create the app entity
	id, err := c.entityClient.Create(ctx, name, app)
	if err != nil {
		return nil, fmt.Errorf("failed to create app %s: %w", name, err)
	}

	// Set the ID on the app
	app.ID = id

	return app, nil
}

// GetByName retrieves an app by its name
func (c *Client) GetByName(ctx context.Context, name string) (*core_v1alpha.App, error) {
	var app core_v1alpha.App
	err := c.entityClient.Get(ctx, name, &app)
	if err != nil {
		return nil, fmt.Errorf("failed to get app %s: %w", name, err)
	}
	return &app, nil
}

// Destroy deletes an app by its name
func (c *Client) Destroy(ctx context.Context, name string) error {
	// First get the app to ensure it exists and get its ID
	app, err := c.GetByName(ctx, name)
	if err != nil {
		// If app doesn't exist, that's fine
		return nil
	}

	// Delete the app entity
	err = c.entityClient.Delete(ctx, app.ID)
	if err != nil {
		return fmt.Errorf("failed to delete app %s: %w", name, err)
	}

	return nil
}

// SetHost sets the host for an app by creating/updating an http_route entity
func (c *Client) SetHost(ctx context.Context, appName, host string) error {
	// First verify the app exists
	app, err := c.GetByName(ctx, appName)
	if err != nil {
		return err
	}

	// Create the http_route entity
	route := &ingress_v1alpha.HttpRoute{
		Host: host,
		App:  app.ID,
	}

	// Use the host as the route name
	_, err = c.entityClient.CreateOrUpdate(ctx, host, route)
	if err != nil {
		return fmt.Errorf("failed to create/update route: %w", err)
	}

	return nil
}

// List returns all apps
func (c *Client) List(ctx context.Context) ([]*core_v1alpha.App, error) {
	// List would need to be implemented using the List method on entityClient
	// For now, return an error indicating it's not implemented
	return nil, fmt.Errorf("list not implemented")
}
