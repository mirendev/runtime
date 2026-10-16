package commands

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/api/app/app_v1alpha"
	"miren.dev/runtime/pkg/addon"
	"miren.dev/runtime/pkg/oidcauth"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/workloadroles"
	appserver "miren.dev/runtime/servers/app"
)

type addonCLIAuthenticator struct{ role string }

func (a addonCLIAuthenticator) Authenticate(context.Context, *rpc.Credentials) (*rpc.Identity, error) {
	return &rpc.Identity{Subject: "catalog-reader", Method: rpc.AuthMethodWorkload,
		Metadata: map[string]any{"role": a.role}}, nil
}

func addonCLIServer(t *testing.T, registry *addon.Registry, role string) string {
	t.Helper()
	server, err := rpc.NewState(t.Context(), rpc.WithBindAddr("localhost:0"),
		rpc.WithAuthenticator(addonCLIAuthenticator{role}),
		rpc.WithAuthorizer(oidcauth.NewCompositeAuthorizer(nil)))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, server.Close()) })
	// No entities capability or entity client: catalog commands must work
	// solely through the public catalog API, with no raw-entity fallback.
	server.Server().ExposeValue("dev.miren.runtime/addons", app_v1alpha.AdaptAddons(
		appserver.NewAddonsServer(slog.Default(), nil, registry, nil)))
	return server.ListenAddr()
}

func runAddonCLI(t *testing.T, address string, command func(*Context) error) (string, error) {
	t.Helper()
	r, w, err := os.Pipe()
	require.NoError(t, err)
	t.Cleanup(func() { _ = r.Close() })
	stdout := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = stdout }()
	ctx := &Context{Context: t.Context(), Log: slog.Default(), Stdout: w, Stderr: io.Discard}
	ctx.Config.ServerAddress = address
	commandErr := command(ctx)
	require.NoError(t, w.Close())
	var output bytes.Buffer
	_, err = output.ReadFrom(r)
	require.NoError(t, err)
	return output.String(), commandErr
}

func TestAddonCatalogCLI(t *testing.T) {
	registry := addon.NewRegistry()
	registry.Register("database", nil, addon.AddonDefinition{
		Name: "database", DisplayName: "Database", Description: "Managed database",
		DefaultVariant: "large", DefaultVersion: "17", BaseImage: "private/internal",
		Variants: []addon.VariantDefinition{
			{Name: "small", Description: "Shared", Details: map[string]string{"storage": "10 GB"},
				Config: map[string]string{"password": "private-password"}},
			{Name: "large", Description: "Dedicated"},
		},
	})
	address := addonCLIServer(t, registry, workloadroles.RoleClusterReadonly)

	commands := []struct {
		name string
		call func(*Context, FormatOptions) error
		json string
		text []string
	}{
		{
			name: "list-available",
			call: func(ctx *Context, format FormatOptions) error {
				return AddonListAvailable(ctx, struct {
					FormatOptions
					ConfigCentric
				}{FormatOptions: format})
			},
			json: `[{"name":"database","display_name":"Database","description":"Managed database","default_variant":"large","default_version":"17","variants":["small","large"]}]`,
			text: []string{"ADDON", "DESCRIPTION", "VARIANTS", "DEFAULT VARIANT", "DEFAULT VERSION", "database", "Managed database", "small, large", "17"},
		},
		{
			name: "variants",
			call: func(ctx *Context, format FormatOptions) error {
				return AddonVariants(ctx, struct {
					FormatOptions
					ConfigCentric
					Addon string `position:"0" usage:"Addon name (e.g., miren-postgresql)" required:"true"`
				}{FormatOptions: format, Addon: "database"})
			},
			json: `[{"name":"small","description":"Shared","details":{"storage":"10 GB"}},{"name":"large","description":"Dedicated","default":true}]`,
			text: []string{"Variants for Database:\n", "VARIANT", "DESCRIPTION", "DEFAULT", "small", "Shared", "large", "Dedicated", "yes", "\nsmall:\n  storage: 10 GB\n"},
		},
	}
	for _, cmd := range commands {
		t.Run(cmd.name, func(t *testing.T) {
			for _, format := range []FormatOptions{{Format: "json"}, {JSON: true}, {Format: "text"}} {
				output, err := runAddonCLI(t, address, func(ctx *Context) error { return cmd.call(ctx, format) })
				require.NoError(t, err)
				if format.IsJSON() {
					assert.JSONEq(t, cmd.json, output)
				} else {
					for _, text := range cmd.text {
						assert.Contains(t, output, text)
					}
				}
				assert.NotContains(t, output, "private")
			}

			deniedAddress := addonCLIServer(t, registry, workloadroles.RoleAppReadonly)
			output, err := runAddonCLI(t, deniedAddress, func(ctx *Context) error { return cmd.call(ctx, FormatOptions{JSON: true}) })
			require.ErrorContains(t, err, "403")
			assert.Empty(t, output)

			emptyAddress := addonCLIServer(t, addon.NewRegistry(), workloadroles.RoleClusterReadonly)
			if cmd.name == "variants" {
				// A definition with no variants is distinct from a missing addon.
				output, err = runAddonCLI(t, emptyAddress, func(ctx *Context) error { return cmd.call(ctx, FormatOptions{}) })
				require.ErrorContains(t, err, `addon "database" not found`)
				assert.Empty(t, output)
				emptyRegistry := addon.NewRegistry()
				emptyRegistry.Register("database", nil, addon.AddonDefinition{Name: "database", DisplayName: "Database"})
				emptyAddress = addonCLIServer(t, emptyRegistry, workloadroles.RoleClusterReadonly)
			}
			output, err = runAddonCLI(t, emptyAddress, func(ctx *Context) error { return cmd.call(ctx, FormatOptions{JSON: true}) })
			require.NoError(t, err)
			assert.Equal(t, "null\n", output)
			output, err = runAddonCLI(t, emptyAddress, func(ctx *Context) error { return cmd.call(ctx, FormatOptions{}) })
			require.NoError(t, err)
			if cmd.name == "variants" {
				assert.Equal(t, "Variants for Database:\n\nNo variants available\n", output)
			} else {
				assert.Equal(t, "No addons available\n", output)
			}
		})
	}
}
