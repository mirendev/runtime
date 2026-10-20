package app

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
	"miren.dev/runtime/api/app/app_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/testutils"
	"miren.dev/runtime/pkg/rbac"
	"miren.dev/runtime/pkg/rpc"
)

type crudResourceAuthorizer struct {
	allow bool
}

func (*crudResourceAuthorizer) Authorize(context.Context, *rpc.Identity, string, string) error {
	return nil
}

func (a *crudResourceAuthorizer) AuthorizeResource(_ context.Context, _ *rpc.Identity, request rpc.ResourceRequest) error {
	if !a.allow {
		return rpc.ErrUnauthorized
	}
	return nil
}

func TestCrudResourceAuthorizationBeforeMutation(t *testing.T) {
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)
	ctx := t.Context()
	server := &AppInfo{Log: slog.Default(), EC: inmem.Client}
	client := app_v1alpha.NewCrudClient(rpc.LocalClient(app_v1alpha.AdaptCrud(server)))
	appID, err := inmem.Client.Create(ctx, "shop", &core_v1alpha.App{OwnedBy: "alice"})
	require.NoError(t, err)
	var value app_v1alpha.NamedValue
	value.SetKey("KEY")
	value.SetValue("changed")
	for _, tc := range []struct {
		method, action string
		call           func(context.Context) error
	}{
		{"new", "create", func(ctx context.Context) error { _, err := client.New(ctx, "new-app"); return err }},
		{"destroy", "destroy", func(ctx context.Context) error { _, err := client.Destroy(ctx, "shop"); return err }},
		{"setconfiguration", "configure", func(ctx context.Context) error {
			_, err := client.SetConfiguration(ctx, "shop", &app_v1alpha.Configuration{})
			return err
		}},
		{"setenvvar", "configure", func(ctx context.Context) error {
			_, err := client.SetEnvVar(ctx, "shop", "KEY", "changed", false, "")
			return err
		}},
		{"setenvvars", "configure", func(ctx context.Context) error {
			_, err := client.SetEnvVars(ctx, "shop", []*app_v1alpha.NamedValue{&value}, "")
			return err
		}},
		{"setinitialenvvars", "configure", func(ctx context.Context) error {
			_, err := client.SetInitialEnvVars(ctx, "shop", []*app_v1alpha.NamedValue{&value}, "")
			return err
		}},
		{"deleteenvvar", "configure", func(ctx context.Context) error { _, err := client.DeleteEnvVar(ctx, "shop", "KEY", ""); return err }},
		{"restart", "deploy", func(ctx context.Context) error { _, err := client.Restart(ctx, "shop", ""); return err }},
		{"getconfiguration", "read", func(ctx context.Context) error { _, err := client.GetConfiguration(ctx, "shop"); return err }},
		{"listversions", "read", func(ctx context.Context) error {
			_, err := client.ListVersions(ctx, appID.String(), false, 0)
			return err
		}},
	} {
		t.Run(tc.method, func(t *testing.T) {
			auth := &crudResourceAuthorizer{}
			caller := rpc.ContextWithIdentity(ctx, &rpc.Identity{Subject: "mallory", Method: rpc.AuthMethodJWT})
			caller = rpc.ContextWithAuthorization(caller, auth, "crud", tc.method)
			require.EqualError(t, tc.call(caller), rpc.ErrUnauthorized.Error())
			var stored core_v1alpha.App
			require.NoError(t, inmem.Client.Get(ctx, "shop", &stored))
			require.Equal(t, "alice", stored.OwnedBy)
			require.Empty(t, stored.ActiveVersion)
			for _, kind := range []entity.Id{core_v1alpha.KindAppVersion, core_v1alpha.KindConfigVersion, core_v1alpha.KindDeployment} {
				list, err := inmem.Client.List(ctx, entity.Ref(entity.EntityKind, kind))
				require.NoError(t, err)
				require.False(t, list.Next(), "denied call created %s", kind)
			}
		})
	}
	var missing core_v1alpha.App
	require.Error(t, inmem.Client.Get(ctx, "new-app", &missing))

	auth := &crudResourceAuthorizer{allow: true}
	allowed := rpc.ContextWithAuthorization(ctx, auth, "crud", "listversions")
	_, err = client.ListVersions(allowed, appID.String(), false, 0)
	require.NoError(t, err)
}

type appVisibilityAuthorizer struct {
	policy    rbac.Policy
	evaluator *rbac.Evaluator
}

func (a *appVisibilityAuthorizer) GetPolicy() *rbac.Policy { return &a.policy }
func (*appVisibilityAuthorizer) Authorize(context.Context, *rpc.Identity, string, string) error {
	return nil
}
func (a *appVisibilityAuthorizer) AuthorizeResource(_ context.Context, id *rpc.Identity, req rpc.ResourceRequest) error {
	if a.evaluator.Evaluate(&rbac.Request{
		Subject: id.Subject, Groups: []string{"members"}, Resource: req.Resource,
		Action: req.Action, Instance: req.Instance, Owner: req.Owner,
	}) == rbac.DecisionAllow {
		return nil
	}
	return rpc.ErrUnauthorized
}

func TestAppInventoryScopedVisibility(t *testing.T) {
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)
	for name, owner := range map[string]string{"mine": "alice", "shared-web": "bob", "private": "bob", "legacy": ""} {
		_, err := inmem.Client.Create(t.Context(), name, &core_v1alpha.App{CreatedBy: owner})
		require.NoError(t, err)
	}
	client := app_v1alpha.NewCrudClient(rpc.LocalClient(app_v1alpha.AdaptCrud(&AppInfo{Log: slog.Default(), EC: inmem.Client})))
	for _, tc := range []struct {
		name, action, scope string
		want                []string
	}{
		{"owner", "list", "$owner", []string{"mine"}},
		{"collaborator", "list", "shared-*", []string{"shared-web"}},
		{"read does not grant list", "read", "*", nil},
		{"administrator", "list", "*", []string{"legacy", "mine", "private", "shared-web"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			auth := &appVisibilityAuthorizer{policy: rbac.Policy{Rules: []rbac.Rule{{Groups: []string{"members"}, Permissions: []rbac.Permission{
				{Resource: "app", Actions: []string{tc.action}, Scope: tc.scope},
			}}}}}
			auth.evaluator = rbac.NewEvaluator(t.Context(), auth, slog.Default())
			ctx := rpc.ContextWithIdentity(t.Context(), &rpc.Identity{Subject: "alice", Method: rpc.AuthMethodJWT})
			ctx = rpc.ContextWithAuthorization(ctx, auth, "crud", "list")
			result, err := client.List(ctx)
			require.NoError(t, err)
			var names []string
			for _, app := range result.Apps() {
				names = append(names, app.Name())
			}
			require.ElementsMatch(t, tc.want, names)
		})
	}
	for _, method := range []rpc.AuthMethod{rpc.AuthMethodOIDC, rpc.AuthMethodWorkload} {
		ctx := rpc.ContextWithIdentity(t.Context(), &rpc.Identity{Method: method, Metadata: map[string]any{"app": "mine", "bound_app": "mine"}})
		result, err := client.List(ctx)
		require.NoError(t, err)
		require.Len(t, result.Apps(), 1)
		require.Equal(t, "mine", result.Apps()[0].Name())
	}
}
