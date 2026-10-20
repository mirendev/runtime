package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/rpc"
)

// Authorize resolves names or IDs before evaluating an app operation. Workload
// confinement remains mandatory even on callers without a resource authorizer.
func Authorize(ctx context.Context, eac *entityserver_v1alpha.EntityAccessClient, target, action string) error {
	if !rpc.HasResourceAuthorizer(ctx) {
		if !rpc.AllowApp(ctx, target) {
			return rpc.AppAccessError(ctx, target)
		}
		return nil
	}
	if action == "create" {
		return rpc.AuthorizeResource(ctx, rpc.ResourceRequest{Resource: "app", Action: action})
	}
	id := target
	if !strings.Contains(id, "/") {
		id = "app/" + id
	}
	record, err := eac.Get(ctx, id)
	if err != nil {
		if errors.Is(err, cond.ErrNotFound{}) && (action == "read" || action == "destroy") {
			// Preserve missing-app reads and idempotent deletion for callers
			// with an explicit grant. No stored owner means $owner cannot match.
			return rpc.AuthorizeResource(ctx, rpc.ResourceRequest{
				Resource: "app", Action: action, Instance: strings.TrimPrefix(id, "app/"),
			})
		}
		return fmt.Errorf("%w: cannot resolve app", rpc.ErrUnauthorized)
	}
	ent := record.Entity().Entity()
	var app core_v1alpha.App
	if !app.Is(ent) {
		return rpc.ErrUnauthorized
	}
	app.Decode(ent)
	var metadata core_v1alpha.Metadata
	metadata.Decode(ent)
	if !rpc.AllowApp(ctx, metadata.Name) {
		return rpc.AppAccessError(ctx, metadata.Name)
	}
	return rpc.AuthorizeResource(ctx, rpc.ResourceRequest{
		Resource: "app", Action: action, Instance: metadata.Name, Owner: app.CreatedBy,
	})
}

// AuthorizeSandbox follows the sandbox's pool, or a poolless sandbox's version,
// to its owning app. A sandbox with no verifiable app cannot inherit an
// app-scoped permission.
func AuthorizeSandbox(ctx context.Context, eac *entityserver_v1alpha.EntityAccessClient, target, action string) error {
	if !rpc.HasResourceAuthorizer(ctx) {
		return nil
	}
	record, err := eac.Get(ctx, target)
	if err != nil {
		return rpc.ErrUnauthorized
	}
	var sandbox compute_v1alpha.Sandbox
	if !sandbox.Is(record.Entity().Entity()) {
		return rpc.ErrUnauthorized
	}
	sandbox.Decode(record.Entity().Entity())
	var metadata core_v1alpha.Metadata
	metadata.Decode(record.Entity().Entity())
	if poolID, ok := metadata.Labels.Get("pool"); ok && poolID != "" {
		record, err := eac.Get(ctx, poolID)
		if err != nil {
			return rpc.ErrUnauthorized
		}
		var pool compute_v1alpha.SandboxPool
		if record.Entity().Id() != poolID || !pool.Is(record.Entity().Entity()) {
			return rpc.ErrUnauthorized
		}
		pool.Decode(record.Entity().Entity())
		if pool.App == "" {
			return rpc.AuthorizeResource(ctx, rpc.ResourceRequest{Resource: "cluster", Action: "admin"})
		}
		return Authorize(ctx, eac, pool.App.String(), action)
	}
	if sandbox.Spec.Version == "" {
		return rpc.AuthorizeResource(ctx, rpc.ResourceRequest{Resource: "cluster", Action: "admin"})
	}
	record, err = eac.Get(ctx, sandbox.Spec.Version.String())
	if err != nil {
		return rpc.ErrUnauthorized
	}
	var version core_v1alpha.AppVersion
	if record.Entity().Id() != sandbox.Spec.Version.String() || !version.Is(record.Entity().Entity()) {
		return rpc.ErrUnauthorized
	}
	version.Decode(record.Entity().Entity())
	if version.App == "" {
		return rpc.ErrUnauthorized
	}
	return Authorize(ctx, eac, version.App.String(), action)
}
