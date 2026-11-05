package commands

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"miren.dev/runtime/api/admin/admin_v1alpha"
	"miren.dev/runtime/api/app/app_v1alpha"
	"miren.dev/runtime/api/deployment/deployment_v1alpha"
	"miren.dev/runtime/api/usage/usage_v1alpha"
	"miren.dev/runtime/clientconfig"
	"miren.dev/runtime/pkg/rpc"
)

func captureOutputJSON(t *testing.T, run func()) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "stdout")
	require.NoError(t, err)
	prev := os.Stdout
	os.Stdout = f
	defer func() { os.Stdout = prev; require.NoError(t, f.Close()) }()
	run()
	_, err = f.Seek(0, io.SeekStart)
	require.NoError(t, err)
	b, err := io.ReadAll(f)
	require.NoError(t, err)
	return string(b)
}

func captureHumanOutput(t *testing.T, name, output string) {
	t.Helper()
	t.Logf("%s:\n%s", name, output)
}

func TestRFD120OutputFlags(t *testing.T) {
	for _, command := range []any{Version, Admin, AdminList, AppWatch, AppStatus, AppTop} {
		for _, args := range [][]string{{"--json"}, {"--format", "JSON"}} {
			cmd := Infer("test", "test", command)
			require.NoError(t, cmd.fs.Parse(args))
		}
	}
	cmd := Infer("admin", "test", Admin)
	require.NoError(t, cmd.fs.Parse([]string{"-j"}))
	require.True(t, cmd.opts.Interface().(*AdminOptions).LegacyJSON)
}

func TestRFD120VersionJSON(t *testing.T) {
	for _, args := range [][]string{{"--json"}, {"--format", "JSON"}} {
		text := captureOutputJSON(t, func() {
			_, err := RunCommand(Version, args...)
			require.NoError(t, err)
		})
		require.True(t, json.Valid([]byte(text)), text)
	}
}

func TestRFD120RouteSetSelection(t *testing.T) {
	t.Setenv("MIREN_APP", "")
	t.Setenv("MIREN_RUNTIME_APP", "")
	dir := t.TempDir()
	writeAppToml(t, dir, `name = "local-app"`)
	chdir(t, dir)
	for _, tc := range []struct {
		args    []string
		env     string
		runtime string
		want    string
		wantErr bool
	}{
		{args: []string{"example.com"}, want: "local-app"},
		{args: []string{"example.com", "legacy"}, want: "legacy"},
		{args: []string{"example.com", "legacy", "--app", "explicit"}, wantErr: true},
		{args: []string{"example.com", "--app", "explicit"}, want: "explicit"},
		{args: []string{"example.com", "legacy"}, env: "other", wantErr: true},
		{args: []string{"example.com", "legacy"}, env: "legacy", runtime: "legacy", want: "legacy"},
	} {
		t.Setenv("MIREN_APP", tc.env)
		t.Setenv("MIREN_RUNTIME_APP", tc.runtime)
		cmd := Infer("route set", "test", RouteSet)
		require.NoError(t, cmd.fs.Parse(tc.args))
		opts := cmd.opts.Interface().(*RouteSetOptions)
		if tc.wantErr {
			require.ErrorContains(t, opts.Validate(&GlobalFlags{}), "disagrees with --app or MIREN_APP")
			continue
		}
		require.NoError(t, opts.Validate(&GlobalFlags{}))
		require.Equal(t, tc.want, opts.App)
	}
}

func TestRFD120WhoamiIdentityOnly(t *testing.T) {
	config := clientconfig.NewConfig()
	claims := base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"user-123","email":"person@example.com","name":"Person","organization_id":"hidden-org","group_ids":["hidden-group"],"exp":4102444800}`))
	config.SetIdentity("cloud", &clientconfig.IdentityConfig{Type: clientconfig.IdentityToken, Token: "eyJhbGciOiJIUzI1NiJ9." + claims + ".c2ln"})
	var human bytes.Buffer
	ctx := &Context{Context: context.Background(), Stdout: &human, ClientConfig: config, ClusterName: "dev", ClusterConfig: &clientconfig.ClusterConfig{Hostname: "https://example.com", Identity: "cloud"}}
	opts := struct {
		FormatOptions
		ConfigCentric
	}{}
	require.NoError(t, Whoami(ctx, opts))
	require.Contains(t, human.String(), "person@example.com")
	require.Contains(t, human.String(), "user-123")
	require.NotContains(t, human.String(), "hidden-")
	captureHumanOutput(t, "whoami", human.String())
	opts.JSON = true
	text := captureOutputJSON(t, func() { require.NoError(t, Whoami(ctx, opts)) })
	var data map[string]any
	require.NoError(t, json.Unmarshal([]byte(text), &data))
	require.Equal(t, "user-123", data["user_id"])
	require.NotContains(t, data, "organization_id")
	require.NotContains(t, data, "group_ids")
}

type rfdCrudRPC struct {
	app_v1alpha.Crud
}

type rfdDeploymentRPC struct {
	deployment_v1alpha.Deployment
}

type rfdOutputRPC struct {
	usage_v1alpha.ResourceUsage
	admin_v1alpha.Admin
	statusCalls int
	selectedApp string
	crashed     bool
}

func (f *rfdCrudRPC) GetConfiguration(_ context.Context, call *app_v1alpha.CrudGetConfiguration) error {
	call.Results().SetVersionId("version-full")
	call.Results().SetVersionShortId("ver123")
	return nil
}

func (f *rfdDeploymentRPC) GetActiveDeployment(_ context.Context, _ *deployment_v1alpha.DeploymentGetActiveDeployment) error {
	return nil
}

func (f *rfdDeploymentRPC) ListDeployments(_ context.Context, _ *deployment_v1alpha.DeploymentListDeployments) error {
	return nil
}

func (f *rfdOutputRPC) AppInfo(_ context.Context, call *app_v1alpha.AppStatusAppInfo) error {
	f.statusCalls++
	status := &app_v1alpha.ApplicationStatus{}
	status.SetName("shop")
	status.SetHealth("healthy")
	service := &app_v1alpha.ServiceHealth{}
	service.SetService("web")
	service.SetHealth("healthy")
	service.SetRunning(2)
	if f.crashed {
		service.SetHealth("crashed")
		service.SetRunning(0)
		service.SetDead(3)
		service.SetCrashCount(7)
		service.SetCooldownSeconds(240)
		service.SetLastExitCode(1)
	}
	status.SetServices([]*app_v1alpha.ServiceHealth{service})
	call.Results().SetStatus(status)
	return nil
}

func (f *rfdOutputRPC) ListSandboxes(_ context.Context, call *usage_v1alpha.ResourceUsageListSandboxes) error {
	f.selectedApp = call.Args().Selector().App()
	ref := &usage_v1alpha.SandboxRef{}
	ref.SetApp(f.selectedApp)
	ref.SetSandbox("sandbox/long-id")
	ref.SetSandboxShortId("abc123")
	usage := &usage_v1alpha.SandboxUsage{}
	usage.SetRef(ref)
	usage.SetStale(true)
	call.Results().SetSandboxes([]*usage_v1alpha.SandboxUsage{usage})
	call.Results().SetCluster(&usage_v1alpha.ResourceTotals{})
	return nil
}

func (f *rfdOutputRPC) ListMethods(_ context.Context, call *admin_v1alpha.AdminListMethods) error {
	method := &admin_v1alpha.AdminMethod{}
	method.SetName("hello")
	call.Results().SetMethods([]*admin_v1alpha.AdminMethod{method})
	return nil
}

func (f *rfdOutputRPC) Invoke(_ context.Context, call *admin_v1alpha.AdminInvoke) error {
	result := &admin_v1alpha.AdminCallResult{}
	result.SetResult(`{"ok":true}`)
	call.Results().SetResult(result)
	return nil
}

func TestRFD120AppOutputHandlers(t *testing.T) {
	t.Setenv("MIREN_CONFIG", t.TempDir()+"/no-config")
	t.Setenv("MIREN_CLUSTER", "")
	server, err := rpc.NewState(t.Context(), rpc.WithBindAddr("localhost:0"), rpc.WithAuthenticator(&rpc.NoOpAuthenticator{}))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, server.Close()) })
	fake := &rfdOutputRPC{}
	server.Server().ExposeValue("dev.miren.runtime/app", app_v1alpha.AdaptCrud(&rfdCrudRPC{}))
	server.Server().ExposeValue("dev.miren.runtime/deployment", deployment_v1alpha.AdaptDeployment(&rfdDeploymentRPC{}))
	server.Server().ExposeValue(rpcAppStatus, app_v1alpha.AdaptAppStatus(fake))
	server.Server().ExposeValue(ServiceUsage, usage_v1alpha.AdaptResourceUsage(fake))
	server.Server().ExposeValue("dev.miren.runtime/admin", admin_v1alpha.AdaptAdmin(fake))
	config := clientconfig.NewConfig()
	config.SetCluster("dev", &clientconfig.ClusterConfig{Hostname: server.ListenAddr(), Insecure: true})
	path := filepath.Join(t.TempDir(), "clientconfig.yaml")
	require.NoError(t, config.SaveTo(path))
	args := []string{"--config", path, "-C", "dev", "--app", "shop"}
	// The captured stdout is a file, not a terminal. A live watch must print
	// once without trying to open /dev/tty or waiting for input.
	captureOutputJSON(t, func() {
		out, err := RunCommand(AppWatch, args...)
		require.NoError(t, err)
		require.Contains(t, out.Stdout.String(), "ver123")
		require.Contains(t, out.Stdout.String(), "2 running")
		captureHumanOutput(t, "app-status", out.Stdout.String())
	})
	require.Equal(t, 1, fake.statusCalls)
	out, err := RunCommand(AppStatus, append(append([]string{}, args...), "--graph")...)
	require.NoError(t, err)
	require.Contains(t, out.Stdout.String(), "CPU")
	require.Contains(t, out.Stdout.String(), "Memory")
	captureHumanOutput(t, "app-status-graph", out.Stdout.String())
	fake.crashed = true
	out, err = RunCommand(AppStatus, args...)
	require.NoError(t, err)
	require.Contains(t, out.Stdout.String(), "crash-looping")
	captureHumanOutput(t, "app-status-crashed", out.Stdout.String())
	out, err = RunCommand(AppTop, args...)
	require.NoError(t, err)
	require.Equal(t, "shop", fake.selectedApp)
	require.Contains(t, out.Stdout.String(), "SANDBOX")
	require.Contains(t, out.Stdout.String(), "abc123")
	require.NotContains(t, out.Stdout.String(), "sandbox/long-id")
	captureHumanOutput(t, "app-top", out.Stdout.String())
	for _, flags := range [][]string{{"--json"}, {"--format", "json"}, {"-j"}} {
		text := captureOutputJSON(t, func() {
			_, err := RunCommand(Admin, append(append([]string{}, args...), append(flags, "--no-validate", "hello")...)...)
			require.NoError(t, err)
		})
		require.JSONEq(t, `{"ok":true}`, text)
	}
	text := captureOutputJSON(t, func() {
		_, err := RunCommand(AdminList, append(args, "--json")...)
		require.NoError(t, err)
	})
	require.JSONEq(t, `[{"name":"hello"}]`, text)
}
