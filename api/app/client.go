package app

import (
	"context"
	"fmt"
	"log/slog"

	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/rpc"
)

// Client provides a domain-specific client for App entities
type Client struct {
	log          *slog.Logger
	entityClient *entityserver.Client
}

// NewClient creates a new App client from an RPC client
func NewClient(log *slog.Logger, client rpc.Client) *Client {
	// Get the entity access client
	eac := entityserver_v1alpha.NewEntityAccessClient(client)

	// Create entity client wrapper
	entityClient := entityserver.NewClient(log, eac)

	return &Client{
		log:          log,
		entityClient: entityClient,
	}
}

// Create creates a new app entity
func (c *Client) Create(ctx context.Context, name string) (*core_v1alpha.App, error) {
	app, _, err := EnsureApp(ctx, c.entityClient.EAC(), name, CreationSubject(ctx))
	if err != nil {
		return nil, fmt.Errorf("failed to create app %s: %w", name, err)
	}

	return app, nil
}

// CreationSubject returns a Cloud user subject eligible for app ownership.
// Other auth methods have distinct subject namespaces or short-lived identities;
// their deployment attribution must not become a Cloud user's owner grant.
// Unattributed apps remain unowned rather than becoming owned by a later deployer.
func CreationSubject(ctx context.Context) string {
	if identity := rpc.IdentityFromContext(ctx); identity != nil && identity.Method == rpc.AuthMethodJWT {
		return identity.Subject
	}
	return ""
}

// EnsureApp atomically creates an app without changing an existing app's owner
// or configuration. creator must be an eligible Cloud user subject from
// authenticated context or durable server-owned inputs, never app configuration.
func EnsureApp(ctx context.Context, eac *entityserver_v1alpha.EntityAccessClient, name, creator string) (*core_v1alpha.App, int64, error) {
	app := &core_v1alpha.App{
		ID: entity.Id("app/" + name), Project: "project/default", OwnedBy: creator,
	}
	attrs := append(app.Encode(), (&core_v1alpha.Metadata{Name: name}).Encode()...)
	attrs = append(attrs, entity.Ref(entity.DBId, app.ID))
	if _, err := eac.Ensure(ctx, attrs); err != nil {
		return nil, 0, err
	}
	res, err := eac.Get(ctx, app.ID.String())
	if err != nil {
		return nil, 0, err
	}
	app = &core_v1alpha.App{}
	app.Decode(res.Entity().Entity())
	return app, res.Entity().Revision(), nil
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

// GetById retrieves an app by its ID
func (c *Client) GetById(ctx context.Context, id entity.Id) (*core_v1alpha.App, error) {
	var app core_v1alpha.App
	err := c.entityClient.GetById(ctx, id, &app)
	if err != nil {
		return nil, fmt.Errorf("failed to get app %s: %w", id, err)
	}
	return &app, nil
}

// List returns all apps
func (c *Client) List(ctx context.Context) ([]*core_v1alpha.App, error) {
	// List would need to be implemented using the List method on entityClient
	// For now, return an error indicating it's not implemented
	return nil, fmt.Errorf("list not implemented")
}
