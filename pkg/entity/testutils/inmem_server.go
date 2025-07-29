package testutils

import (
	"context"
	"io"
	"log/slog"
	"testing"

	apiserver "miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/schema"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/servers/entityserver"
)

// InMemEntityServer provides an in-memory entity server for testing
type InMemEntityServer struct {
	Store  *entity.MockStore
	Server *entityserver.EntityServer
	EAC    *entityserver_v1alpha.EntityAccessClient
	Client *apiserver.Client
}

// NewInMemEntityServer creates a new in-memory entity server for testing
func NewInMemEntityServer(t *testing.T) (*InMemEntityServer, func()) {
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))

	// Create mock store
	mockStore := entity.NewMockStore()

	// Apply schema to the store
	err := schema.Apply(ctx, mockStore)
	if err != nil {
		t.Fatalf("failed to apply schema: %v", err)
	}

	// Create entity server
	es, err := entityserver.NewEntityServer(log, mockStore)
	if err != nil {
		t.Fatalf("failed to create entity server: %v", err)
	}

	// Create entity access client with local transport
	localClient := rpc.LocalClient(entityserver_v1alpha.AdaptEntityAccess(es))
	eac := entityserver_v1alpha.NewEntityAccessClient(localClient)

	// Create the high-level entityserver client
	client := apiserver.NewClient(log, eac)

	cleanup := func() {
		// Nothing to clean up with local client
	}

	return &InMemEntityServer{
		Store:  mockStore,
		Server: es,
		EAC:    eac,
		Client: client,
	}, cleanup
}

// AddEntity adds an entity to the mock store
func (s *InMemEntityServer) AddEntity(ent *entity.Entity) {
	s.Store.Entities[ent.ID] = ent
}

// GetEntity retrieves an entity from the mock store
func (s *InMemEntityServer) GetEntity(id entity.Id) *entity.Entity {
	return s.Store.Entities[id]
}

// TestLogger creates a test logger that discards all output
func TestLogger(t *testing.T) *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug}))
}
