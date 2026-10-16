package app

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"miren.dev/runtime/api/app/app_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/testutils"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/rpc/standard"
)

func TestListVersions(t *testing.T) {
	ctx := context.Background()
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()
	info := &AppInfo{
		Log: slog.Default(), EC: inmem.Client,
	}
	client := app_v1alpha.NewCrudClient(rpc.LocalClient(app_v1alpha.AdaptCrud(info)))
	appID, err := inmem.Client.Create(ctx, "shop", &core_v1alpha.App{ActiveVersion: "app_version/active"})
	require.NoError(t, err)
	otherID, err := inmem.Client.Create(ctx, "other", &core_v1alpha.App{})
	require.NoError(t, err)
	_, err = inmem.Client.Create(ctx, "empty", &core_v1alpha.App{})
	require.NoError(t, err)

	base := time.Date(2026, 10, 15, 12, 0, 0, 123000000, time.UTC)
	expires := base.Add(24 * time.Hour)
	fixtures := []struct {
		name string
		ver  core_v1alpha.AppVersion
		hour int
	}{
		{"preview", core_v1alpha.AppVersion{App: appID, Version: "preview-display", EphemeralLabel: "pr-17", EphemeralTtl: "24h", EphemeralExpiresAt: expires}, 3},
		{"old", core_v1alpha.AppVersion{App: appID, Version: "old-display"}, 1},
		{"new", core_v1alpha.AppVersion{App: appID, Version: "new-display", EphemeralTtl: "ignored", EphemeralExpiresAt: expires}, 4},
		{"active", core_v1alpha.AppVersion{App: appID, Version: "active-display", EphemeralLabel: "promoted", EphemeralTtl: "48h"}, 2},
		{"foreign", core_v1alpha.AppVersion{App: otherID, Version: "foreign-display", EphemeralLabel: "foreign"}, 5},
	}
	for _, f := range fixtures {
		f.ver.AdminToken = "private-admin-token"
		f.ver.Config.Entrypoint = "private-entrypoint"
		f.ver.ConfigVersion = "config_version/private"
		inmem.AddEntity(entity.New(
			entity.DBId, entity.Id("app_version/"+f.name), f.ver.Encode,
			entity.Time(entity.CreatedAt, base.Add(time.Duration(f.hour)*time.Hour)),
		))
	}

	for _, tc := range []struct {
		name      string
		ephemeral bool
		limit     int64
		want      []string
	}{
		{"all_zero_limit", false, 0, []string{"new-display", "preview-display", "active-display", "old-display"}},
		{"all_negative_limit", false, -2, []string{"new-display", "preview-display", "active-display", "old-display"}},
		{"limited", false, 2, []string{"new-display", "preview-display"}},
		{"larger_than_history", false, 99, []string{"new-display", "preview-display", "active-display", "old-display"}},
		{"ephemeral", true, 0, []string{"preview-display", "active-display"}},
		{"filter_before_limit", true, 1, []string{"preview-display"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, err := client.ListVersions(ctx, "shop", tc.ephemeral, tc.limit)
			require.NoError(t, err)
			var got []string
			for _, v := range res.Versions() {
				got = append(got, v.Version())
			}
			require.Equal(t, tc.want, got)
		})
	}

	res, err := client.ListVersions(ctx, "shop", false, 0)
	require.NoError(t, err)
	for i, want := range []struct {
		id, status, label, ttl string
		hour                   int
	}{
		{"app_version/new", "replaced", "", "", 4},
		{"app_version/preview", "ephemeral", "pr-17", "24h", 3},
		{"app_version/active", "active", "promoted", "48h", 2},
		{"app_version/old", "replaced", "", "", 1},
	} {
		v := res.Versions()[i]
		require.Equal(t, want.id, v.Id())
		require.Equal(t, want.status, v.Status())
		require.Equal(t, want.label, v.EphemeralLabel())
		require.Equal(t, want.ttl, v.EphemeralTtl())
		require.True(t, base.Add(time.Duration(want.hour)*time.Hour).Equal(standard.FromTimestamp(v.CreatedAt())))
		if want.status == "ephemeral" {
			require.True(t, expires.Equal(standard.FromTimestamp(v.ExpiresAt())))
		} else {
			require.False(t, v.HasExpiresAt())
		}
	}
	encoded, err := json.Marshal(res.Versions())
	require.NoError(t, err)
	for _, private := range []string{"private-admin-token", "private-entrypoint", "config_version/private", "admin_token", "config", "environment"} {
		require.NotContains(t, string(encoded), private)
	}

	res, err = client.ListVersions(ctx, "empty", false, 0)
	require.NoError(t, err)
	require.Empty(t, res.Versions())
	_, err = client.ListVersions(ctx, "missing", false, 0)
	require.Error(t, err)

	for _, method := range []rpc.AuthMethod{rpc.AuthMethodWorkload, rpc.AuthMethodOIDC} {
		t.Run(string(method), func(t *testing.T) {
			scoped := rpc.ContextWithIdentity(ctx, &rpc.Identity{
				Method: method, Metadata: map[string]any{"app": "shop", "bound_app": "shop"},
			})
			res, err := client.ListVersions(scoped, "shop", false, 0)
			require.NoError(t, err)
			require.Len(t, res.Versions(), 4)
			for _, target := range []string{"other", "missing", appID.String(), ""} {
				_, err := client.ListVersions(scoped, target, false, 0)
				require.EqualError(t, err, rpc.ErrUnauthorized.Error())
			}
		})
	}

	t.Run("http_binding", func(t *testing.T) {
		mux := http.NewServeMux()
		rpc.RegisterREST(mux, app_v1alpha.AdaptCrud(info))
		for _, tc := range []struct {
			app    string
			status int
		}{
			{"shop", http.StatusOK},
			{"other", http.StatusForbidden},
		} {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/apps/"+tc.app+"/versions?ephemeral=true&limit=1", nil)
			req = req.WithContext(rpc.ContextWithIdentity(ctx, &rpc.Identity{
				Method: rpc.AuthMethodOIDC, Metadata: map[string]any{"bound_app": "shop"},
			}))
			resp := httptest.NewRecorder()
			mux.ServeHTTP(resp, req)
			require.Equal(t, tc.status, resp.Code, resp.Body.String())
			if tc.status == http.StatusOK {
				var out struct {
					Versions []*app_v1alpha.AppVersionSummary `json:"versions"`
				}
				require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &out))
				require.Len(t, out.Versions, 1)
				require.Equal(t, "preview-display", out.Versions[0].Version())
			}
		}
	})

	t.Run("active_version_resolved_on_each_call", func(t *testing.T) {
		require.NoError(t, inmem.Client.Patch(ctx, appID, 0, entity.Ref(core_v1alpha.AppActiveVersionId, "app_version/new")))
		res, err := client.ListVersions(ctx, "shop", false, 0)
		require.NoError(t, err)
		require.Equal(t, "active", res.Versions()[0].Status())
		require.Equal(t, "ephemeral", res.Versions()[2].Status())
	})
}

func TestListVersionsResolvedAppAuthorizationEtcd(t *testing.T) {
	if testing.Short() {
		t.Skip("requires etcd short-ID resolution")
	}
	store, cleanup := testutils.NewEtcdEntityServer(t)
	t.Cleanup(cleanup)
	ctx := t.Context()
	appID, err := store.Client.Create(ctx, "other", &core_v1alpha.App{})
	require.NoError(t, err)
	versionID, err := store.Client.Create(ctx, "version", &core_v1alpha.AppVersion{App: appID, Version: "private-version"})
	require.NoError(t, err)
	var app core_v1alpha.App
	record, err := store.Client.GetByIdWithEntity(ctx, appID, &app)
	require.NoError(t, err)
	alias := record.Entity().ShortId()
	require.NotEmpty(t, alias)
	client := app_v1alpha.NewCrudClient(rpc.LocalClient(app_v1alpha.AdaptCrud(&AppInfo{Log: slog.Default(), EC: store.Client})))
	for _, method := range []rpc.AuthMethod{rpc.AuthMethodWorkload, rpc.AuthMethodOIDC} {
		scoped := rpc.ContextWithIdentity(ctx, &rpc.Identity{
			Method: method, Metadata: map[string]any{"app": alias, "bound_app": alias},
		})
		_, err := client.ListVersions(scoped, alias, false, 0)
		require.EqualError(t, err, rpc.ErrUnauthorized.Error())
	}
	result, err := client.ListVersions(ctx, alias, false, 0)
	require.NoError(t, err)
	require.Len(t, result.Versions(), 1)
	require.Equal(t, "private-version", result.Versions()[0].Version())
	var version core_v1alpha.AppVersion
	record, err = store.Client.GetByIdWithEntity(ctx, versionID, &version)
	require.NoError(t, err)
	_, err = client.ListVersions(ctx, record.Entity().ShortId(), false, 0)
	require.ErrorContains(t, err, "not an app")
	_, err = client.ListVersions(ctx, "missing", false, 0)
	require.ErrorIs(t, err, cond.ErrNotFound{})
	for _, method := range []rpc.AuthMethod{rpc.AuthMethodWorkload, rpc.AuthMethodOIDC} {
		for _, target := range []string{record.Entity().ShortId(), "missing"} {
			scoped := rpc.ContextWithIdentity(ctx, &rpc.Identity{
				Method: method, Metadata: map[string]any{"app": target, "bound_app": target},
			})
			_, err := client.ListVersions(scoped, target, false, 0)
			require.EqualError(t, err, rpc.ErrUnauthorized.Error())
		}
	}
}

func TestListVersionsPreservesBackendFailure(t *testing.T) {
	backend := rpc.NewInterface([]rpc.Method{{
		Name: "get", InterfaceName: "EntityAccess",
		Handler: func(context.Context, rpc.Call) error { return context.DeadlineExceeded },
	}}, struct{}{})
	ec := entityserver.NewClient(slog.Default(), entityserver_v1alpha.NewEntityAccessClient(rpc.LocalClient(backend)))
	client := app_v1alpha.NewCrudClient(rpc.LocalClient(app_v1alpha.AdaptCrud(&AppInfo{Log: slog.Default(), EC: ec})))
	for _, ctx := range []context.Context{
		t.Context(),
		rpc.ContextWithIdentity(t.Context(), &rpc.Identity{
			Method: rpc.AuthMethodWorkload, Metadata: map[string]any{"app": "shop"},
		}),
	} {
		_, err := client.ListVersions(ctx, "shop", false, 0)
		require.ErrorIs(t, err, context.DeadlineExceeded)
	}
}
