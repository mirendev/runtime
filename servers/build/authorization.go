package build

import (
	"context"
	"errors"
	"fmt"

	"miren.dev/runtime/api/app"
	"miren.dev/runtime/api/core/core_v1alpha"
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

// refuseDisabled stops a build of a disabled app before any work happens. A
// deploy to an app that's off has no coherent behavior: its deploy tasks would
// boot sandboxes against whatever the app shares, and nothing could verify the
// new version without booting the app. Maintenance mode is for apps that should
// keep taking deploys.
func (b *Builder) refuseDisabled(ctx context.Context, name string) error {
	var rec core_v1alpha.App
	if err := b.ec.Get(ctx, name, &rec); err != nil {
		if errors.Is(err, cond.ErrNotFound{}) {
			return nil
		}
		return err
	}
	if !rec.Disabled.Empty() {
		return fmt.Errorf("app %s is disabled; run `miren app enable -a %s` before deploying", name, name)
	}
	return nil
}
