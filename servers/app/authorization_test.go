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
	appID, err := inmem.Client.Create(ctx, "shop", &core_v1alpha.App{CreatedBy: "alice"})
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
			require.Equal(t, "alice", stored.CreatedBy)
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
