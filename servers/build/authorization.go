package build

import (
	"context"
	"errors"

	"miren.dev/runtime/api/app"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/rpc"
)

// Logical policy requires both permissions before creating anything. Legacy
// deploy roles retain implicit creation, confined to their bound app.
func (b *Builder) authorizeBuild(ctx context.Context, name string) error {
	if !rpc.AllowApp(ctx, name) {
		return rpc.AppAccessError(ctx, name)
	}
	if !rpc.UsesLogicalAuthorization(ctx) {
		return nil
	}
	_, err := b.ec.EAC().Get(ctx, "app/"+name)
	if err == nil {
		return app.Authorize(ctx, b.ec.EAC(), name, "deploy")
	}
	if !errors.Is(err, cond.ErrNotFound{}) {
		return err
	}
	if err := app.Authorize(ctx, b.ec.EAC(), name, "create"); err != nil {
		return err
	}
	return rpc.AuthorizeResource(ctx, rpc.ResourceRequest{
		Resource: "app", Action: "deploy", Instance: name, Owner: app.CreationSubject(ctx),
	})
}
