package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"miren.dev/runtime/api/app/app_v1alpha"
	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/testutils"
	"miren.dev/runtime/pkg/rpc"
	appserver "miren.dev/runtime/servers/app"
	sandboxserver "miren.dev/runtime/servers/sandbox"
)

func TestAppPoolSizeSelectsOnlyActiveService(t *testing.T) {
	store, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)
	appID, err := store.Client.Create(t.Context(), "shop", &core_v1alpha.App{ActiveVersion: "app_version/current"})
	require.NoError(t, err)
	for _, name := range []string{"current", "old"} {
		store.AddEntity(entity.New(entity.DBId, entity.Id("app_version/"+name), (&core_v1alpha.AppVersion{App: appID}).Encode))
	}
	for _, item := range []struct{ id, service, version string }{
		{"pool/web", "web", "current"}, {"pool/worker", "worker", "current"}, {"pool/old", "web", "old"},
	} {
		require.NoError(t, store.Client.UpdateAttrs(t.Context(), entity.Id(item.id), (&compute_v1alpha.SandboxPool{
			App: appID, Service: item.service, DesiredInstances: 2, CurrentInstances: 1, ReadyInstances: 1,
			SandboxSpec: compute_v1alpha.SandboxSpec{Version: entity.Id("app_version/" + item.version)},
		}).Encode(), entity.Ref(entity.DBId, entity.Id(item.id))))
	}
	server, err := rpc.NewState(t.Context(), rpc.WithBindAddr("localhost:0"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, server.Close()) })
	server.Server().ExposeValue("dev.miren.runtime/app", app_v1alpha.AdaptCrud(&appserver.AppInfo{Log: slog.Default(), EC: store.Client}))
	server.Server().ExposeValue("dev.miren.runtime/sandbox-pools", compute_v1alpha.AdaptSandboxPools(sandboxserver.NewPoolServer(store.Client)))
	var stdout bytes.Buffer
	ctx := &Context{Context: t.Context(), Log: slog.Default(), Stdout: &stdout, Stderr: io.Discard}
	ctx.Config.ServerAddress = server.ListenAddr()
	run := func(size, service string, json bool) error {
		stdout.Reset()
		return AppPoolSize(ctx, struct {
			AppCentric
			FormatOptions
			Size    string `position:"0" usage:"Desired instance count (omit to show current size)"`
			Service string `short:"s" long:"service" description:"Service to scale" default:"web"`
		}{AppCentric: AppCentric{App: "shop"}, FormatOptions: FormatOptions{JSON: json}, Size: size, Service: service})
	}
	require.NoError(t, run("5", "web", true))
	var result map[string]any
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &result))
	require.Equal(t, "pool/web", result["id"])
	require.EqualValues(t, 5, result["desired_instances"])
	for id, want := range map[string]int64{"pool/web": 5, "pool/worker": 2, "pool/old": 2} {
		var pool compute_v1alpha.SandboxPool
		require.NoError(t, store.Client.GetById(t.Context(), entity.Id(id), &pool))
		require.Equal(t, want, pool.DesiredInstances)
	}
	require.NoError(t, run("", "web", false))
	for _, text := range []string{"ID", "APP", "SERVICE", "DESIRED", "CURRENT", "READY", "shop", "web", "5"} {
		require.Contains(t, stdout.String(), text)
	}
	require.ErrorContains(t, run("3", "missing", true), "no active pool")
	require.ErrorContains(t, run("-1", "web", true), "non-negative")
	require.Error(t, run("999999999999999999999999", "web", true))
}

func TestSandboxPoolCommandsLegacyEntities(t *testing.T) {
	for _, oldService := range []bool{false, true} {
		t.Run(fmt.Sprint("old-service=", oldService), func(t *testing.T) {
			testSandboxPoolCommandsLegacy(t, oldService)
		})
	}
}

func testSandboxPoolCommandsLegacy(t *testing.T, oldService bool) {
	store, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)
	server, err := rpc.NewState(t.Context(), rpc.WithBindAddr("localhost:0"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, server.Close()) })
	server.Server().ExposeValue("entities", entityserver_v1alpha.AdaptEntityAccess(store.Server))
	if oldService {
		server.Server().ExposeValue("dev.miren.runtime/sandbox-pools", rpc.NewInterface([]rpc.Method{{
			Name: "legacy", InterfaceName: "SandboxPools", Handler: func(context.Context, rpc.Call) error { return nil },
		}}, nil))
	}
	id := entity.Id("pool/legacy")
	require.NoError(t, store.Client.UpdateAttrs(t.Context(), id, (&compute_v1alpha.SandboxPool{Service: "web", DesiredInstances: 3}).Encode(), entity.Ref(entity.DBId, id)))
	args := []string{"--config", filepath.Join(t.TempDir(), "config.yaml"), "--server-address", server.ListenAddr()}
	out, err := RunCommand(SandboxPoolList, append(args, "--format", "json")...)
	require.NoError(t, err)
	require.Contains(t, out.Stdout.String(), `"desired_instances": 3`)
	out, err = RunCommand(SandboxPoolSetDesired, append(args, "legacy", "+2")...)
	require.NoError(t, err)
	require.Contains(t, out.Stdout.String(), "New desired_instances: 5")
	var pool compute_v1alpha.SandboxPool
	require.NoError(t, store.Client.GetById(t.Context(), id, &pool))
	require.EqualValues(t, 5, pool.DesiredInstances)
	_, err = RunCommand(SandboxPoolSetDesired, append(args, "legacy", "9223372036854775807")...)
	require.ErrorContains(t, err, "maximum pool size")
}

func TestSandboxPoolCommandsWithoutEntityAccess(t *testing.T) {
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)
	ec := inmem.Client
	appID, err := ec.Create(t.Context(), "shop", &core_v1alpha.App{})
	require.NoError(t, err)
	versionID, err := ec.Create(t.Context(), "shop-v1", &core_v1alpha.AppVersion{App: appID, Config: core_v1alpha.Config{
		Services: []core_v1alpha.Services{{Name: "web", ServiceConcurrency: core_v1alpha.ServiceConcurrency{Mode: "fixed"}}},
	}})
	require.NoError(t, err)
	const secret = "pool-cli-secret-canary"
	poolID := entity.Id("pool/pool-shop-web")
	err = ec.UpdateAttrs(t.Context(), poolID, (&compute_v1alpha.SandboxPool{
		App: appID, Service: "web", DesiredInstances: 3, CurrentInstances: 2, ReadyInstances: 1,
		SandboxSpec: compute_v1alpha.SandboxSpec{Version: versionID, Container: []compute_v1alpha.SandboxSpecContainer{{Image: "private-image", Env: []string{"DATABASE_URL=" + secret}}}},
	}).Encode(), entity.Ref(entity.DBId, poolID), entity.String(entity.DBShortId, "pool-cli-short"))
	require.NoError(t, err)
	var pool compute_v1alpha.SandboxPool
	ent, err := ec.GetByIdWithEntity(t.Context(), poolID, &pool)
	require.NoError(t, err)
	var version core_v1alpha.AppVersion
	verEnt, err := ec.GetByIdWithEntity(t.Context(), versionID, &version)
	require.NoError(t, err)
	server, err := rpc.NewState(t.Context(), rpc.WithBindAddr("localhost:0"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, server.Close()) })
	// Deliberately expose no EntityAccess capability: both commands must work
	// exclusively through the safe operation-specific boundary.
	server.Server().ExposeValue("dev.miren.runtime/sandbox-pools", compute_v1alpha.AdaptSandboxPools(sandboxserver.NewPoolServer(ec)))
	var stdout bytes.Buffer
	ctx := &Context{Context: t.Context(), Log: slog.Default(), Stdout: &stdout, Stderr: io.Discard}
	ctx.Config.ServerAddress = server.ListenAddr()
	list := func(format FormatOptions) error {
		stdout.Reset()
		return SandboxPoolList(ctx, struct {
			FormatOptions
			ConfigCentric
		}{FormatOptions: format})
	}
	setDesired := func(id, desired string, raw bool) error {
		stdout.Reset()
		return SandboxPoolSetDesired(ctx, struct {
			RawID   bool   `long:"raw-id" description:"Use the provided ID as-is without adding the pool/ prefix"`
			PoolID  string `position:"0" usage:"Pool ID (e.g., pool-CUSkT8J58BmgkDeGyPP2e or pool/pool-CUSkT8J58BmgkDeGyPP2e)" required:"true"`
			Desired string `position:"1" usage:"Desired instance count (absolute number, +N to increase, or -N to decrease)" required:"true"`
			ConfigCentric
		}{PoolID: id, Desired: desired, RawID: raw})
	}
	require.NoError(t, list(FormatOptions{}))
	for _, text := range []string{"ID", "VERSION", "SERVICE", "MODE", "DESIRED", "CURRENT", "READY", "CREATED", "UPDATED", ent.Entity().ShortId(), verEnt.Entity().ShortId(), "web", "fixed"} {
		require.Contains(t, stdout.String(), text)
	}
	for _, format := range []FormatOptions{{JSON: true}, {Format: "json"}} {
		require.NoError(t, list(format))
		var items []map[string]any
		require.NoError(t, json.Unmarshal(stdout.Bytes(), &items))
		require.Len(t, items, 1)
		require.Equal(t, poolID.String(), items[0]["id"])
		require.Equal(t, "shop", items[0]["app"])
		require.Equal(t, "web", items[0]["service"])
		require.Equal(t, "fixed", items[0]["scaling_mode"])
		require.Equal(t, float64(3), items[0]["desired_instances"])
		require.Equal(t, float64(2), items[0]["current_instances"])
		require.Equal(t, float64(1), items[0]["ready_instances"])
		require.Equal(t, ent.Entity().GetCreatedAt().UTC().Format("2006-01-02T15:04:05Z07:00"), items[0]["created_at"])
		for _, excluded := range []string{secret, "private-image", "SandboxSpec", "sandbox_spec", "DATABASE_URL", `"env"`} {
			require.NotContains(t, stdout.String(), excluded)
		}
	}
	for _, tc := range []struct {
		input         string
		before, after int64
	}{
		{" 5 ", 3, 5}, {"+2", 5, 7}, {"-4", 7, 3},
	} {
		require.NoError(t, setDesired(strings.TrimPrefix(poolID.String(), "pool/"), tc.input, false))
		updated, err := ec.GetByIdWithEntity(t.Context(), poolID, &pool)
		require.NoError(t, err)
		require.Equal(t, tc.after, pool.DesiredInstances)
		require.Equal(t, fmt.Sprintf("Successfully updated sandbox pool:\n  Pool ID: %s\n  Service: web\n  Previous desired_instances: %d\n  New desired_instances: %d\n  Current instances: 2\n  Ready instances: 1\n  Revision: %d\n", ent.Entity().ShortId(), tc.before, tc.after, updated.Revision()), stdout.String())
	}
	for _, input := range []string{"3", "+0"} {
		before, err := ec.GetByIdWithEntity(t.Context(), poolID, &pool)
		require.NoError(t, err)
		require.NoError(t, setDesired(poolID.String(), input, false))
		require.Equal(t, fmt.Sprintf("Pool %s (service=web) already has desired_instances=3\n", ent.Entity().ShortId()), stdout.String())
		after, err := ec.GetByIdWithEntity(t.Context(), poolID, &pool)
		require.NoError(t, err)
		require.Equal(t, before.Revision(), after.Revision())
	}
	for _, input := range []string{"-4", "bad", "+bad", "9223372036854775808", "21", "+18", "+50"} {
		require.Error(t, setDesired(poolID.String(), input, false))
		require.Empty(t, stdout.String())
	}
	err = setDesired(poolID.String(), "-5", false)
	require.ErrorContains(t, err, "relative adjustment would result in negative count: 3 + -5 = -2")
	require.NotContains(t, err.Error(), "failed to patch")
	err = setDesired("pool-missing", "1", false)
	require.ErrorContains(t, err, "sandbox pool not found: pool-missing")
	require.NotContains(t, err.Error(), "failed to patch")
	require.NoError(t, ec.GetById(t.Context(), poolID, &pool))
	require.EqualValues(t, 3, pool.DesiredInstances)
	// An unprefixed stored ID distinguishes --raw-id from normalization in
	// both the CLI and server.
	rawID := entity.Id("pool-special")
	require.NoError(t, ec.UpdateAttrs(t.Context(), rawID, (&compute_v1alpha.SandboxPool{App: appID, Service: "worker"}).Encode(), entity.Ref(entity.DBId, rawID)))
	require.Error(t, setDesired(rawID.String(), "1", false))
	require.NoError(t, setDesired(rawID.String(), "1", true))
	require.NoError(t, ec.GetById(t.Context(), rawID, &pool))
	require.EqualValues(t, 1, pool.DesiredInstances)
	require.NoError(t, ec.Delete(t.Context(), rawID))
	require.NoError(t, ec.Delete(t.Context(), poolID))
	require.NoError(t, list(FormatOptions{}))
	require.Equal(t, "No sandbox pools found\n", stdout.String())
	require.NoError(t, list(FormatOptions{JSON: true}))
	require.Equal(t, "[]\n", stdout.String())
}

type deniedPoolServer struct{}

func (deniedPoolServer) List(context.Context, *compute_v1alpha.SandboxPoolsList) error {
	return rpc.ErrUnauthorized
}

func (deniedPoolServer) SetDesired(context.Context, *compute_v1alpha.SandboxPoolsSetDesired) error {
	return rpc.ErrUnauthorized
}

func TestSandboxPoolCommandsDoNotFallback(t *testing.T) {
	for _, available := range []bool{false, true} {
		t.Run(fmt.Sprint("available=", available), func(t *testing.T) {
			server, err := rpc.NewState(t.Context(), rpc.WithBindAddr("localhost:0"))
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, server.Close()) })
			if available {
				server.Server().ExposeValue("dev.miren.runtime/sandbox-pools", compute_v1alpha.AdaptSandboxPools(deniedPoolServer{}))
			}
			var stdout bytes.Buffer
			ctx := &Context{Context: t.Context(), Log: slog.Default(), Stdout: &stdout, Stderr: io.Discard}
			ctx.Config.ServerAddress = server.ListenAddr()
			err = SandboxPoolList(ctx, struct {
				FormatOptions
				ConfigCentric
			}{})
			require.Error(t, err)
			if available {
				require.ErrorContains(t, err, "unauthorized")
			} else {
				require.ErrorContains(t, err, "entities")
			}
			require.Empty(t, stdout.String())
			err = SandboxPoolSetDesired(ctx, struct {
				RawID   bool   `long:"raw-id" description:"Use the provided ID as-is without adding the pool/ prefix"`
				PoolID  string `position:"0" usage:"Pool ID (e.g., pool-CUSkT8J58BmgkDeGyPP2e or pool/pool-CUSkT8J58BmgkDeGyPP2e)" required:"true"`
				Desired string `position:"1" usage:"Desired instance count (absolute number, +N to increase, or -N to decrease)" required:"true"`
				ConfigCentric
			}{PoolID: "pool-1", Desired: "+2"})
			require.Error(t, err)
			if available {
				require.ErrorContains(t, err, "unauthorized")
			} else {
				require.ErrorContains(t, err, "entities")
			}
			require.Empty(t, stdout.String())
		})
	}
}
