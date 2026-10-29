package commands

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"miren.dev/runtime/api/app/app_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/testutils"
	"miren.dev/runtime/pkg/oidcauth"
	"miren.dev/runtime/pkg/rpc"
	appserver "miren.dev/runtime/servers/app"
)

type versionHistoryIdentity struct {
	method rpc.AuthMethod
}

func (a versionHistoryIdentity) Authenticate(context.Context, *rpc.Credentials) (*rpc.Identity, error) {
	return &rpc.Identity{
		Method: a.method,
		Metadata: map[string]any{
			"app": "shop", "bound_app": "shop", "role": "app-readonly",
		},
	}, nil
}

func TestAppVersionsRPC(t *testing.T) {
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()
	appID, err := inmem.Client.Create(t.Context(), "shop", &core_v1alpha.App{ActiveVersion: "app_version/active"})
	require.NoError(t, err)
	_, err = inmem.Client.Create(t.Context(), "other", &core_v1alpha.App{})
	require.NoError(t, err)
	created := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	for i, v := range []core_v1alpha.AppVersion{
		{ID: "app_version/active", Version: "z-active", EphemeralLabel: "promoted", EphemeralTtl: "48h"},
		{ID: "app_version/preview", Version: "a-preview", EphemeralLabel: "pr-17", EphemeralTtl: "1h", EphemeralExpiresAt: created.Add(time.Hour)},
		{ID: "app_version/rolling", Version: "m-rolling"},
	} {
		v.App = appID
		v.AdminToken = "never-show-this-token"
		v.Config.Entrypoint = "never-show-this-config"
		inmem.AddEntity(entity.New(entity.DBId, v.ID, v.Encode,
			entity.Time(entity.CreatedAt, created.Add(time.Duration(i)*time.Minute))))
	}
	want := []map[string]any{
		{"id": "app_version/rolling", "version": "m-rolling", "status": "replaced", "created_at": "2026-01-02T03:06:05Z"},
		{"id": "app_version/preview", "version": "a-preview", "status": "ephemeral", "created_at": "2026-01-02T03:05:05Z", "ephemeral_label": "pr-17", "ephemeral_ttl": "1h", "expires_at": "2026-01-02T04:04:05Z"},
		{"id": "app_version/active", "version": "z-active", "status": "active", "created_at": "2026-01-02T03:04:05Z", "ephemeral_label": "promoted", "ephemeral_ttl": "48h"},
	}
	for _, mode := range []string{"workload", "oidc", "old-service", "missing-service"} {
		t.Run(mode, func(t *testing.T) {
			legacy := mode == "old-service" || mode == "missing-service"
			var auth rpc.Authenticator = versionHistoryIdentity{method: rpc.AuthMethod(mode)}
			if legacy {
				auth = &rpc.NoOpAuthenticator{}
			}
			server, err := rpc.NewState(t.Context(), rpc.WithBindAddr("localhost:0"),
				rpc.WithAuthenticator(auth),
				rpc.WithAuthorizer(oidcauth.NewCompositeAuthorizer(nil)))
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, server.Close()) })
			if legacy {
				server.Server().ExposeValue("entities", entityserver_v1alpha.AdaptEntityAccess(inmem.Server))
				if mode == "old-service" {
					server.Server().ExposeValue("dev.miren.runtime/app", rpc.NewInterface([]rpc.Method{{
						Name: "list", InterfaceName: "Crud",
						Handler: func(context.Context, rpc.Call) error { return nil },
					}}, struct{}{}))
				}
			} else {
				// Modern servers need no EntityAccess capability.
				server.Server().ExposeValue("dev.miren.runtime/app", app_v1alpha.AdaptCrud(&appserver.AppInfo{
					Log: slog.Default(), EC: inmem.Client,
				}))
			}

			run := func(app string, ephemeral bool, limit int, format string) (string, error) {
				t.Helper()
				out, err := os.CreateTemp(t.TempDir(), "stdout")
				require.NoError(t, err)
				defer out.Close()
				original := os.Stdout
				os.Stdout = out
				defer func() { os.Stdout = original }()
				ctx := &Context{Context: t.Context(), Log: slog.Default(), Stdout: out, Stderr: io.Discard}
				ctx.Config.ServerAddress = server.ListenAddr()
				cmdErr := AppVersions(ctx, struct {
					AppCentric
					FormatOptions
					Ephemeral bool `long:"ephemeral" description:"Show only ephemeral versions"`
					Limit     int  `short:"n" long:"limit" description:"Max versions to show" default:"20"`
				}{AppCentric: AppCentric{App: app}, FormatOptions: FormatOptions{Format: format}, Ephemeral: ephemeral, Limit: limit})
				_, err = out.Seek(0, io.SeekStart)
				require.NoError(t, err)
				printed, err := io.ReadAll(out)
				require.NoError(t, err)
				return string(printed), cmdErr
			}
			for _, tc := range []struct {
				name      string
				ephemeral bool
				limit     int
				want      []map[string]any
			}{
				{"unlimited_zero", false, 0, want},
				{"unlimited_negative", false, -2, want},
				{"limited", false, 1, want[:1]},
				{"ephemeral", true, 20, want[1:]},
				{"filter_before_limit", true, 1, want[1:2]},
			} {
				t.Run(tc.name, func(t *testing.T) {
					printed, err := run("shop", tc.ephemeral, tc.limit, "json")
					require.NoError(t, err)
					var got []map[string]any
					require.NoError(t, json.Unmarshal([]byte(printed), &got))
					require.Equal(t, tc.want, got)
				})
			}
			t.Run("tables", func(t *testing.T) {
				printed, err := run("shop", false, 20, "text")
				require.NoError(t, err)
				for _, text := range []string{"VERSION", "STATUS", "CREATED", "LABEL", "m-rolling", "replaced", "a-preview", "ephemeral", "z-active", "active", "pr-17 (expired)", "promoted (expired)"} {
					require.Contains(t, printed, text)
				}
				printed, err = run("shop", true, 20, "text")
				require.NoError(t, err)
				for _, text := range []string{"VERSION", "LABEL", "CREATED", "EXPIRES", "a-preview", "pr-17", created.Add(time.Hour).Local().Format("2006-01-02 15:04:05"), "z-active", "promoted", "-"} {
					require.Contains(t, printed, text)
				}
				require.NotContains(t, printed, "m-rolling")
			})
			t.Run("cross_app_denial", func(t *testing.T) {
				if legacy {
					return
				}
				printed, err := run("other", false, 0, "json")
				require.Error(t, err)
				require.Empty(t, printed)
			})
		})
	}
}
