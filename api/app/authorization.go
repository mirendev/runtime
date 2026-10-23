package app

import (
	"context"
	"errors"
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
	if !rpc.UsesLogicalAuthorization(ctx) {
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
		if !errors.Is(err, cond.ErrNotFound{}) {
			return err
		}
		// Let explicitly authorized callers reach the handler's missing-app
		// behavior. Without a stored owner, an $owner grant cannot match.
		return rpc.AuthorizeResource(ctx, rpc.ResourceRequest{
			Resource: "app", Action: action, Instance: strings.TrimPrefix(id, "app/"),
		})
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
		Resource: "app", Action: action, Instance: metadata.Name, Owner: app.OwnedBy,
	})
}

// AuthorizeSandbox follows the sandbox's pool, or a poolless sandbox's version,
// to its owning app. A sandbox with no verifiable app cannot inherit an
// app-scoped permission.
func AuthorizeSandbox(ctx context.Context, eac *entityserver_v1alpha.EntityAccessClient, target, action string) error {
	if !rpc.UsesLogicalAuthorization(ctx) {
		return nil
	}
	record, err := eac.Get(ctx, target)
	if err != nil {
		if !errors.Is(err, cond.ErrNotFound{}) {
			return err
		}
		return rpc.AuthorizeResource(ctx, rpc.ResourceRequest{Resource: "cluster", Action: "admin"})
	}
	var sandbox compute_v1alpha.Sandbox
	if !sandbox.Is(record.Entity().Entity()) {
		return rpc.ErrUnauthorized
	}
	sandbox.Decode(record.Entity().Entity())
	var metadata core_v1alpha.Metadata
	metadata.Decode(record.Entity().Entity())
	var appID string
	if poolID, ok := metadata.Labels.Get("pool"); ok && poolID != "" {
		record, err := eac.Get(ctx, poolID)
		if err != nil {
			if !errors.Is(err, cond.ErrNotFound{}) {
				return err
			}
			return rpc.AuthorizeResource(ctx, rpc.ResourceRequest{Resource: "cluster", Action: "admin"})
		}
		var pool compute_v1alpha.SandboxPool
		if record.Entity().Id() != poolID || !pool.Is(record.Entity().Entity()) {
			return rpc.AuthorizeResource(ctx, rpc.ResourceRequest{Resource: "cluster", Action: "admin"})
		}
		pool.Decode(record.Entity().Entity())
		appID = pool.App.String()
	} else if sandbox.Spec.Version != "" {
		record, err = eac.Get(ctx, sandbox.Spec.Version.String())
		if err != nil {
			if !errors.Is(err, cond.ErrNotFound{}) {
				return err
			}
			return rpc.AuthorizeResource(ctx, rpc.ResourceRequest{Resource: "cluster", Action: "admin"})
		}
		var version core_v1alpha.AppVersion
		if record.Entity().Id() != sandbox.Spec.Version.String() || !version.Is(record.Entity().Entity()) {
			return rpc.AuthorizeResource(ctx, rpc.ResourceRequest{Resource: "cluster", Action: "admin"})
		}
		version.Decode(record.Entity().Entity())
		appID = version.App.String()
	}
	if appID == "" {
		return rpc.AuthorizeResource(ctx, rpc.ResourceRequest{Resource: "cluster", Action: "admin"})
	}
	// Unlike a direct app read, a sandbox cannot inherit a grant from an app
	// that no longer exists. Cluster administrators retain orphan recovery.
	record, err = eac.Get(ctx, appID)
	if err != nil {
		if !errors.Is(err, cond.ErrNotFound{}) {
			return err
		}
		return rpc.AuthorizeResource(ctx, rpc.ResourceRequest{Resource: "cluster", Action: "admin"})
	}
	var app core_v1alpha.App
	if record.Entity().Id() != appID || !app.Is(record.Entity().Entity()) {
		return rpc.AuthorizeResource(ctx, rpc.ResourceRequest{Resource: "cluster", Action: "admin"})
	}
	app.Decode(record.Entity().Entity())
	metadata.Decode(record.Entity().Entity())
	if !rpc.AllowApp(ctx, metadata.Name) {
		return rpc.AppAccessError(ctx, metadata.Name)
	}
	return rpc.AuthorizeResource(ctx, rpc.ResourceRequest{
		Resource: "app", Action: action, Instance: metadata.Name, Owner: app.OwnedBy,
	})
}
