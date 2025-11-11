package controller

import (
	"context"

	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/rpc/stream"
)

// EntityAccessClient defines the interface for interacting with the entity server
type EntityAccessClient interface {
	// Get returns a single entity by its ID
	Get(ctx context.Context, id string) (*entityserver_v1alpha.EntityAccessClientGetResults, error)

	// List returns all entities matching the given index
	List(ctx context.Context, index entity.Attr) (*entityserver_v1alpha.EntityAccessClientListResults, error)

	// WatchIndex watches for changes to entities matching the given index
	// and sends updates through the provided sender
	WatchIndex(ctx context.Context, index entity.Attr, sender stream.SendStream[*entityserver_v1alpha.EntityOp]) error
}
