package commands

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/api/exec/exec_v1alpha"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/testutils"
	"miren.dev/runtime/pkg/entity/types"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/ui"
	sandboxserver "miren.dev/runtime/servers/sandbox"
)

type sandboxCommandAuthorizer struct {
	t    *testing.T
	deny bool
}

func (a sandboxCommandAuthorizer) Authorize(_ context.Context, _ *rpc.Identity, resource, _ string) error {
	if resource == "entityaccess" {
		a.t.Error("migrated sandbox command attempted EntityAccess")
	}
	if a.deny || (resource != "sandboxes" && resource != "sandboxexec") {
		return fmt.Errorf("access denied: %s", resource)
	}
	return nil
}

func sandboxCommandServer(t *testing.T, deny bool) (*Context, *testutils.InMemEntityServer, *rpc.State) {
	t.Helper()
	store, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)
	server, err := rpc.NewState(t.Context(), rpc.WithBindAddr("localhost:0"),
		rpc.WithAuthenticator(&rpc.NoOpAuthenticator{}), rpc.WithAuthorizer(sandboxCommandAuthorizer{t: t, deny: deny}))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, server.Close()) })
	server.Server().ExposeValue("dev.miren.runtime/sandboxes", compute_v1alpha.AdaptSandboxes(sandboxserver.NewServer(testutils.TestLogger(t), store.Client)))
	// Expose the raw capability as well, but deny it. Any fallback must fail.
	server.Server().ExposeValue("entities", entityserver_v1alpha.AdaptEntityAccess(store.Server))
	ctx := &Context{Context: t.Context(), Log: testutils.TestLogger(t), Stdout: &bytes.Buffer{}, Stderr: io.Discard}
	ctx.Config.ServerAddress = server.ListenAddr()
	t.Cleanup(func() { require.NoError(t, ctx.Close()) })
	return ctx, store, server
}

func TestSelectAppSandboxUsesServiceRPC(t *testing.T) {
	ctx, store, _ := sandboxCommandServer(t, false)
	appID, err := store.Client.Create(ctx, "shop", &core_v1alpha.App{})
	require.NoError(t, err)
	old, err := store.Client.Create(ctx, "old", &core_v1alpha.AppVersion{App: appID})
	require.NoError(t, err)
	active, err := store.Client.Create(ctx, "active", &core_v1alpha.AppVersion{App: appID})
	require.NoError(t, err)
	require.NoError(t, store.Client.UpdateAttrs(ctx, appID, entity.Ref(core_v1alpha.AppActiveVersionId, active)))
	pool := func(service string) entity.Id {
		id, err := store.Client.Create(ctx, service, &compute_v1alpha.SandboxPool{App: appID, Service: service})
		require.NoError(t, err)
		return id
	}
	web, worker := pool("web"), pool("worker")
	create := func(name string, pool, version entity.Id, scheduled bool) entity.Id {
		id, err := store.Client.Create(ctx, name, &compute_v1alpha.Sandbox{Status: compute_v1alpha.RUNNING,
			Spec: compute_v1alpha.SandboxSpec{Version: version}}, entityserver.WithLabels(types.LabelSet("pool", pool.String())))
		require.NoError(t, err)
		if scheduled {
			require.NoError(t, store.Client.UpdateAttrs(ctx, id, (&compute_v1alpha.Schedule{Key: compute_v1alpha.Key{Node: "node/one"}}).Encode()))
		}
		return id
	}
	oldWeb := create("old-web", web, old, true)
	activeWorker := create("active-worker", worker, active, true)
	create("ephemeral", "", active, true)
	create("unscheduled-web", web, active, false)
	for _, tc := range []struct {
		service string
		want    entity.Id
	}{{"", oldWeb}, {"worker", activeWorker}} {
		chosen, count, err := selectAppSandbox(ctx, "shop", tc.service)
		require.NoError(t, err)
		require.Equal(t, tc.want, chosen.ID)
		require.Equal(t, 1, count)
		require.NotEmpty(t, chosen.Brief)
	}
	activeWeb := create("active-web", web, active, true)
	chosen, _, err := selectAppSandbox(ctx, "shop", "")
	require.NoError(t, err)
	require.Equal(t, activeWeb, chosen.ID)
	require.NoError(t, store.Client.UpdateAttrs(ctx, activeWeb, entity.Ref(compute_v1alpha.SandboxStatusId, compute_v1alpha.SandboxStatusStoppedId)))
	chosen, _, err = selectAppSandbox(ctx, "shop", "")
	require.NoError(t, err)
	require.Equal(t, oldWeb, chosen.ID)
	require.NoError(t, store.Client.UpdateAttrs(ctx, oldWeb, entity.Ref(compute_v1alpha.SandboxStatusId, compute_v1alpha.SandboxStatusStoppedId)))
	for _, tc := range []struct{ app, service, summary, detail string }{
		{"shop", "", `no running web instance for app "shop"`, "miren sandbox exec -a shop --service worker"},
		{"shop", "web", `no running sandbox for service "web" of app "shop"`, "Nothing is running and placed"},
		{"shop", "typo", `app "shop" has no service named "typo"`, "Its services are: web, worker."},
		{"missing", "", `no app named "missing"`, "miren app list"},
	} {
		_, _, err := selectAppSandbox(ctx, tc.app, tc.service)
		require.ErrorContains(t, err, tc.summary)
		var diagnostic *ui.Diagnostic
		require.ErrorAs(t, err, &diagnostic)
		var output bytes.Buffer
		diagnostic.WriteForTerminal(&output)
		require.Contains(t, output.String(), tc.detail)
	}
}

func TestSandboxLifecycleCommandsUseRPC(t *testing.T) {
	ctx, store, server := sandboxCommandServer(t, false)
	id, err := store.Client.Create(ctx, "target", &compute_v1alpha.Sandbox{Status: compute_v1alpha.RUNNING})
	require.NoError(t, err)
	config := filepath.Join(t.TempDir(), "clientconfig.yaml")
	for range 2 {
		out, err := RunCommand(SandboxStop, "--config", config, "--server-address", server.ListenAddr(), "target")
		require.NoError(t, err)
		require.Equal(t, "Stopped sandbox target\n", out.Stdout.String())
	}
	var sb compute_v1alpha.Sandbox
	require.NoError(t, store.Client.GetById(ctx, id, &sb))
	require.Equal(t, compute_v1alpha.STOPPED, sb.Status)
	out, err := RunCommand(SandboxDelete, "--config", config, "--server-address", server.ListenAddr(), "--force", "target")
	require.ErrorContains(t, err, "must be dead")
	require.Empty(t, out.Stdout.String())
	require.NoError(t, store.Client.UpdateAttrs(ctx, id, entity.Ref(compute_v1alpha.SandboxStatusId, compute_v1alpha.SandboxStatusDeadId)))
	out, err = RunCommand(SandboxDelete, "--config", config, "--server-address", server.ListenAddr(), "--force", "target")
	require.NoError(t, err)
	require.Equal(t, "Deleted sandbox target\n", out.Stdout.String())
	require.Error(t, store.Client.GetById(ctx, id, &sb))
}

func TestSandboxCommandsDoNotFallbackAfterDenial(t *testing.T) {
	ctx, _, server := sandboxCommandServer(t, true)
	_, _, err := selectAppSandbox(ctx, "shop", "")
	var denial cond.ErrRemote
	require.ErrorAs(t, err, &denial)
	require.Equal(t, "permission", denial.Category)
	require.Equal(t, "forbidden", denial.Code)
	for _, tc := range []struct {
		command any
		flags   []string
	}{{SandboxStop, nil}, {SandboxDelete, []string{"--force"}}} {
		args := []string{"--config", filepath.Join(t.TempDir(), "clientconfig.yaml"), "--server-address", server.ListenAddr()}
		args = append(args, tc.flags...)
		out, err := RunCommand(tc.command, append(args, "target")...)
		require.ErrorAs(t, err, &denial)
		require.Equal(t, "permission", denial.Category)
		require.Equal(t, "forbidden", denial.Code)
		require.Empty(t, out.Stdout.String())
	}
}

func TestSandboxCommandsFallbackWhenAPIIsUnavailable(t *testing.T) {
	for _, oldService := range []bool{false, true} {
		t.Run(fmt.Sprint("old-service=", oldService), func(t *testing.T) {
			testSandboxCommandsLegacy(t, oldService)
		})
	}
}

func testSandboxCommandsLegacy(t *testing.T, oldService bool) {
	server, err := rpc.NewState(t.Context(), rpc.WithBindAddr("localhost:0"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, server.Close()) })
	store, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)
	server.Server().ExposeValue("entities", entityserver_v1alpha.AdaptEntityAccess(store.Server))
	if oldService {
		// A real capability with no lifecycle/selection methods simulates an
		// older service without relying on a mocked client error.
		server.Server().ExposeValue("dev.miren.runtime/sandboxes", entityserver_v1alpha.AdaptEntityAccess(store.Server))
	}
	ctx := &Context{Context: t.Context(), Log: testutils.TestLogger(t), Stdout: &bytes.Buffer{}, Stderr: io.Discard}
	ctx.Config.ServerAddress = server.ListenAddr()
	t.Cleanup(func() { require.NoError(t, ctx.Close()) })
	appID, err := store.Client.Create(ctx, "shop", &core_v1alpha.App{})
	require.NoError(t, err)
	poolID, err := store.Client.Create(ctx, "web-pool", &compute_v1alpha.SandboxPool{App: appID, Service: "web"})
	require.NoError(t, err)
	id, err := store.Client.Create(ctx, "target", &compute_v1alpha.Sandbox{Status: compute_v1alpha.RUNNING}, entityserver.WithLabels(types.LabelSet("pool", poolID.String())))
	require.NoError(t, err)
	require.NoError(t, store.Client.UpdateAttrs(ctx, id, (&compute_v1alpha.Schedule{Key: compute_v1alpha.Key{Node: "node/one"}}).Encode()))
	chosen, count, err := selectAppSandbox(ctx, "shop", "")
	require.NoError(t, err)
	require.Equal(t, id, chosen.ID)
	require.Equal(t, 1, count)
	for _, tc := range []struct {
		command any
		flags   []string
	}{{SandboxStop, nil}, {SandboxDelete, []string{"--force"}}} {
		args := []string{"--config", filepath.Join(t.TempDir(), "clientconfig.yaml"), "--server-address", server.ListenAddr()}
		args = append(args, tc.flags...)
		out, err := RunCommand(tc.command, append(args, "target")...)
		require.NoError(t, err)
		require.NotEmpty(t, out.Stdout.String())
		if len(tc.flags) == 0 {
			require.NoError(t, store.Client.UpdateAttrs(ctx, id, entity.Ref(compute_v1alpha.SandboxStatusId, compute_v1alpha.SandboxStatusDeadId)))
		}
	}
}

type selectedExecServer struct{ exec_v1alpha.SandboxExec }

func (*selectedExecServer) Exec(_ context.Context, state *exec_v1alpha.SandboxExecExec) error {
	if state.Args().Category() != "id" || state.Args().Value() != "sandbox/selected" || state.Args().Command() != "echo hello" {
		return fmt.Errorf("unexpected exec target or command")
	}
	state.Results().SetCode(7)
	return nil
}

type selectedSandboxServer struct{ compute_v1alpha.Sandboxes }

func (*selectedSandboxServer) SelectForApp(_ context.Context, state *compute_v1alpha.SandboxesSelectForApp) error {
	if state.Args().App() != "shop" || state.Args().Service() != "worker" {
		return fmt.Errorf("unexpected app or service")
	}
	selection := new(compute_v1alpha.SandboxSelection)
	selection.SetId("sandbox/selected")
	selection.SetShortId("sel")
	selection.SetService("worker")
	selection.SetCandidateCount(3)
	state.Results().SetSelection(selection)
	return nil
}

func TestSandboxExecUsesSelectedTarget(t *testing.T) {
	ctx, _, server := sandboxCommandServer(t, false)
	server.Server().ExposeValue("dev.miren.runtime/sandboxes", compute_v1alpha.AdaptSandboxes(&selectedSandboxServer{}))
	server.Server().ExposeValue("dev.miren.runtime/exec", exec_v1alpha.AdaptSandboxExec(&selectedExecServer{}))
	require.NoError(t, SandboxExec(ctx, sandboxExecOptions{App: "shop", Service: "worker", Args: []string{"echo", "hello"}}))
	require.Equal(t, 7, ctx.exitCode)
}

func TestNoDefaultServiceError(t *testing.T) {
	var buf bytes.Buffer
	noDefaultServiceError("myapp", []string{"worker", "cron"}).(*ui.Diagnostic).WriteForTerminal(&buf)

	out := buf.String()
	require.Contains(t, out, `no running web instance for app "myapp"`)
	require.Contains(t, out, "worker, cron")
	require.Contains(t, out, "miren sandbox exec -a myapp --service worker")
}

// With nothing else running there is no alternative service to offer, and the
// message must not invent one.
func TestNoDefaultServiceErrorWithNothingElseRunning(t *testing.T) {
	var buf bytes.Buffer
	noDefaultServiceError("myapp", nil).(*ui.Diagnostic).WriteForTerminal(&buf)

	out := buf.String()
	require.Contains(t, out, `no running web instance for app "myapp"`)
	require.NotContains(t, out, "Still running")
	require.NotContains(t, out, "exec -a myapp --service")
}

func TestSandboxExecOptionsValidate(t *testing.T) {
	tests := []struct {
		name    string
		opts    sandboxExecOptions
		wantErr string
	}{
		{name: "id alone", opts: sandboxExecOptions{Id: "sandbox/abc"}},
		{name: "app alone", opts: sandboxExecOptions{App: "myapp"}},
		{name: "app with service", opts: sandboxExecOptions{App: "myapp", Service: "worker"}},
		{name: "neither, id comes from a positional", opts: sandboxExecOptions{}},
		{
			name:    "app and id both name a sandbox",
			opts:    sandboxExecOptions{App: "myapp", Id: "sandbox/abc"},
			wantErr: "--app and --id",
		},
		{
			name:    "service without app has nothing to select among",
			opts:    sandboxExecOptions{Id: "sandbox/abc", Service: "worker"},
			wantErr: "it needs --app",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.opts.validate()
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestExecChoiceNotice(t *testing.T) {
	chosen := sandboxCandidate{ID: "sandbox/myapp-web-abc123", Brief: "abc", Service: "web"}

	t.Run("silent when there was no choice", func(t *testing.T) {
		require.Empty(t, execChoiceNotice(chosen, 1))
	})

	t.Run("names the sandbox when there was", func(t *testing.T) {
		require.Equal(t, "abc (web), picked from 3 running", execChoiceNotice(chosen, 3))
	})
}

// A failure against a sandbox we picked has to name the app the caller asked
// for, since the sandbox id is one they never typed.
func TestChosenSandboxExecError(t *testing.T) {
	cause := errors.New("failed to get entity sandbox/myapp-web-abc123: not found")
	err := chosenSandboxExecError("myapp", sandboxCandidate{Brief: "abc", Service: "web"}, cause)

	require.ErrorContains(t, err, `"myapp"`)
	require.ErrorIs(t, err, cause)

	var buf bytes.Buffer
	err.(*ui.Diagnostic).WriteForTerminal(&buf)
	require.Contains(t, buf.String(), "abc (web)")
	require.Contains(t, buf.String(), cause.Error())
}

func TestSandboxExecFlagParsing(t *testing.T) {
	t.Run("app takes every positional as the command", func(t *testing.T) {
		cmd := Infer("sandbox exec", "test", SandboxExec)
		require.NoError(t, cmd.fs.Parse([]string{"-a", "myapp", "--", "ls", "-la", "/app"}))

		opts := cmd.opts.Elem().Interface().(sandboxExecOptions)
		require.Equal(t, "myapp", opts.App)
		require.Empty(t, opts.Id)
		require.Equal(t, []string{"ls", "-la", "/app"}, opts.Args)
	})

	t.Run("without app the first positional is the id", func(t *testing.T) {
		cmd := Infer("sandbox exec", "test", SandboxExec)
		require.NoError(t, cmd.fs.Parse([]string{"sb_abc123", "--", "echo", "hi"}))

		opts := cmd.opts.Elem().Interface().(sandboxExecOptions)
		require.Empty(t, opts.App)
		require.Equal(t, []string{"sb_abc123", "echo", "hi"}, opts.Args)
	})

	t.Run("service is long-only", func(t *testing.T) {
		cmd := Infer("sandbox exec", "test", SandboxExec)
		require.NoError(t, cmd.fs.Parse([]string{"-a", "myapp", "--service", "worker"}))

		opts := cmd.opts.Elem().Interface().(sandboxExecOptions)
		require.Equal(t, "worker", opts.Service)
	})
}
