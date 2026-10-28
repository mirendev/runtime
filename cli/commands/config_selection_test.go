package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"miren.dev/runtime/api/app/app_v1alpha"
	"miren.dev/runtime/api/deployment/deployment_v1alpha"
	"miren.dev/runtime/appconfig"
	"miren.dev/runtime/clientconfig"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/ui"
)

func TestAppClusterSelection(t *testing.T) {
	t.Setenv("CI", "1")
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	writeAppToml(t, dir, `name = "myapp"`)
	cfg := clientconfig.NewConfig()
	cfg.SetCluster("staging", &clientconfig.ClusterConfig{Hostname: "staging"})
	cfg.SetCluster("prod", &clientconfig.ClusterConfig{Hostname: "prod", XID: "prod-id"})
	require.NoError(t, cfg.SetActiveCluster("prod"))
	require.NoError(t, appconfig.SaveAppState("myapp", &appconfig.AppState{Cluster: "staging"}))
	statePath := filepath.Join(os.Getenv("HOME"), ".config", "miren", "app-state.toml")
	before, err := os.ReadFile(statePath)
	require.NoError(t, err)

	app := AppCentric{App: "myapp", Dir: dir, ConfigCentric: ConfigCentric{cfg: cfg}}
	_, name, err := app.LoadCluster()
	require.NoError(t, err)
	require.Equal(t, "prod", name, "legacy pin must be ignored")

	writeDeployToml(t, dir, "[[targets]]\nname = 'dev'\ncluster = 'staging'\n")
	_, name, err = app.LoadCluster()
	require.NoError(t, err)
	require.Equal(t, "staging", name, "sole target overrides global active")

	writeDeployToml(t, dir, "[[targets]]\nname = 'dev'\ncluster = 'staging'\n[[targets]]\nname = 'production'\ncluster = 'old-prod-name'\ncluster_id = 'prod-id'\n")
	_, _, err = app.LoadCluster()
	require.ErrorContains(t, err, "pass -C")
	app.Cluster = "production"
	warning := captureStderr(t, func() {
		_, name, err = app.LoadCluster()
	})
	require.NoError(t, err)
	require.Equal(t, "prod", name, "-C and MIREN_CLUSTER accept target names")
	require.Empty(t, warning)
	app.Cluster = "prod"
	warning = captureStderr(t, func() {
		_, name, err = app.LoadCluster()
	})
	require.NoError(t, err)
	require.Equal(t, "prod", name, "configured alias resolves target by cloud ID")
	require.Contains(t, warning, "deprecated")
	app.Cluster = "staging"
	_, name, err = app.LoadCluster()
	require.NoError(t, err)
	require.Equal(t, "staging", name)
	app.Cluster = "outside;sha1:abcd"
	_, _, err = app.LoadCluster()
	require.ErrorContains(t, err, "not a deploy target")

	after, err := os.ReadFile(statePath)
	require.NoError(t, err)
	require.Equal(t, before, after, "cluster selection must not write app-state")
	clusterWide := ConfigCentric{cfg: cfg}
	_, name, err = clusterWide.LoadCluster()
	require.NoError(t, err)
	require.Equal(t, "prod", name, "cluster-wide selection ignores deploy targets")
}

func TestExplicitOtherAppIgnoresLocalTargets(t *testing.T) {
	t.Setenv("CI", "1")
	t.Setenv("MIREN_APP", "")
	t.Setenv("MIREN_CLUSTER", "")
	t.Setenv("MIREN_RUNTIME_APP", "")
	dir := t.TempDir()
	writeAppToml(t, dir, `name = "local-app"`)
	writeDeployToml(t, dir, "[[targets]]\nname = 'dev'\ncluster = 'staging'\n")
	server, err := rpc.NewState(t.Context(), rpc.WithBindAddr("localhost:0"), rpc.WithAuthenticator(&rpc.NoOpAuthenticator{}))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, server.Close()) })
	server.Server().ExposeValue("dev.miren.runtime/app", app_v1alpha.AdaptCrud(&rfdCrudRPC{}))
	server.Server().ExposeValue("dev.miren.runtime/deployment", deployment_v1alpha.AdaptDeployment(&rfdDeploymentRPC{}))
	server.Server().ExposeValue(rpcAppStatus, app_v1alpha.AdaptAppStatus(&rfdOutputRPC{}))
	cfg := clientconfig.NewConfig()
	for _, name := range []string{"staging", "dev", "outside"} {
		cfg.SetCluster(name, &clientconfig.ClusterConfig{Hostname: server.ListenAddr(), Insecure: true})
	}
	require.NoError(t, cfg.SetActiveCluster("outside"))
	path := filepath.Join(t.TempDir(), "clientconfig.yaml")
	require.NoError(t, cfg.SaveTo(path))
	for _, tc := range []struct {
		name string
		args []string
		env  string
		want string
	}{
		{name: "implicit local app", want: "staging"},
		{name: "explicit local app", args: []string{"-a", "local-app"}, want: "staging"},
		{name: "other app uses active cluster", args: []string{"-a", "other-app"}, want: "outside"},
		{name: "other app accepts unrelated cluster", args: []string{"-a", "other-app", "-C", "outside"}, want: "outside"},
		{name: "other app uses cluster not target name", args: []string{"-a", "other-app", "-C", "dev"}, want: "dev"},
		{name: "environment selects other app", env: "other-app", want: "outside"},
		{name: "flag overrides environment", args: []string{"-a", "local-app"}, env: "other-app", want: "staging"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("MIREN_APP", tc.env)
			cmd := Infer("app status", "test", AppStatus)
			require.NoError(t, cmd.fs.Parse(append([]string{"--config", path, "-d", dir, "--json"}, tc.args...)))
			output := captureOutputJSON(t, func() { require.NoError(t, cmd.Invoke()) })
			var status struct {
				Cluster string `json:"cluster"`
			}
			require.NoError(t, json.Unmarshal([]byte(output), &status))
			require.Equal(t, tc.want, status.Cluster)
		})
	}

	writeDeployToml(t, dir, "invalid toml =")
	cmd := Infer("app status", "test", AppStatus)
	require.NoError(t, cmd.fs.Parse([]string{"--config", filepath.Join(t.TempDir(), "missing"), "-d", dir, "-a", "other-app", "--server-address", server.ListenAddr(), "--json"}))
	output := captureOutputJSON(t, func() {
		require.NoError(t, cmd.Invoke(), "another app's deploy config must not be read, even with a direct server address")
	})
	var status struct {
		App string `json:"app"`
	}
	require.NoError(t, json.Unmarshal([]byte(output), &status))
	require.Equal(t, "other-app", status.App)
}

func TestDeployWithoutTargetsSelection(t *testing.T) {
	t.Setenv("CI", "1")
	dir := t.TempDir()
	writeAppToml(t, dir, `name = "myapp"`)
	cfg := clientconfig.NewConfig()
	cfg.SetCluster("one", &clientconfig.ClusterConfig{})
	require.NoError(t, cfg.SetActiveCluster("one"))
	opts := deployOpts{AppCentric: AppCentric{Dir: dir, ConfigCentric: ConfigCentric{cfg: cfg}}}
	require.NoError(t, opts.Validate(&GlobalFlags{}))
	require.Equal(t, "one", opts.targetCluster)
	cfg.SetCluster("two", &clientconfig.ClusterConfig{})
	require.ErrorContains(t, opts.Validate(&GlobalFlags{}), "pass -C", "active cluster is not deploy consent")
	opts.Cluster = "two"
	require.NoError(t, opts.Validate(&GlobalFlags{}))
	require.Equal(t, "two", opts.targetCluster)
	opts.Cluster = "server:8443;sha1:abcd"
	require.NoError(t, opts.Validate(&GlobalFlags{}))
	require.Equal(t, opts.Cluster, opts.targetCluster)
}

func TestClusterSelectionWithoutActive(t *testing.T) {
	t.Setenv("CI", "1")
	opts := ConfigCentric{cfg: clientconfig.NewConfig()}
	_, _, err := opts.LoadCluster()
	require.ErrorContains(t, err, "miren cluster add")
	opts.cfg.SetCluster("one", &clientconfig.ClusterConfig{})
	_, _, err = opts.LoadCluster()
	require.ErrorContains(t, err, "pass -C")
}

func TestDirectAddressDoesNotBypassTargetSelection(t *testing.T) {
	t.Setenv("CI", "1")
	t.Setenv("MIREN_CLUSTER", "")
	t.Setenv("MIREN_CONFIG", filepath.Join(t.TempDir(), "missing"))
	dir := t.TempDir()
	writeAppToml(t, dir, `name = "shop"`)
	writeDeployToml(t, dir, "[[targets]]\nname = 'dev'\ncluster = 'staging'\n[[targets]]\nname = 'production'\ncluster = 'prod'\n")
	for _, args := range [][]string{nil, {"-C", "outside"}} {
		_, err := RunCommand(AppStatus, append([]string{"-d", dir, "-a", "shop", "--server-address", "127.0.0.1:1"}, args...)...)
		diagnostic, ok := errors.AsType[*ui.Diagnostic](err)
		require.True(t, ok, "%v", err)
		require.Equal(t, "cluster selection failed", diagnostic.Summary)
		if len(args) == 0 {
			require.ErrorContains(t, err, "pass -C")
		} else {
			require.ErrorContains(t, err, "not a deploy target")
			require.NotContains(t, diagnostic.Detail, "reachable address")
		}
	}
}

func TestClusterSelectionDiagnostics(t *testing.T) {
	t.Setenv("CI", "1")
	t.Setenv("MIREN_CLUSTER", "")
	path := filepath.Join(t.TempDir(), "clientconfig.yaml")
	t.Setenv("MIREN_CONFIG", path)
	for _, tc := range []struct {
		name    string
		cfg     *clientconfig.Config
		summary string
		detail  string
	}{
		{name: "missing config", summary: "no clusters configured", detail: "miren cluster add"},
		{name: "empty config", cfg: clientconfig.NewConfig(), summary: "no clusters configured", detail: "miren cluster add"},
		{name: "no active cluster", cfg: clientconfig.NewConfig(), summary: "cluster selection failed", detail: "pass -C"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.cfg != nil {
				if tc.name == "no active cluster" {
					tc.cfg.SetCluster("staging", &clientconfig.ClusterConfig{Hostname: "staging"})
				}
				require.NoError(t, tc.cfg.SaveTo(path))
			}
			_, err := RunCommand(RouteList, "--config", path)
			diagnostic, ok := errors.AsType[*ui.Diagnostic](err)
			require.True(t, ok, "%v", err)
			require.Equal(t, tc.summary, diagnostic.Summary)
			require.Contains(t, diagnostic.Detail, tc.detail)
			require.NotContains(t, diagnostic.Detail, "active cluster we can't resolve")
			if tc.summary == "no clusters configured" {
				require.ErrorIs(t, err, ErrNoConfig)
			}
		})
	}
}

func TestClusterSwitchChangesOnlyGlobal(t *testing.T) {
	dir := setupAppDir(t, "myapp")
	require.NoError(t, appconfig.SaveAppState("myapp", &appconfig.AppState{Cluster: "old"}))
	cfg := clientconfig.NewConfig()
	cfg.SetCluster("new", &clientconfig.ClusterConfig{})
	var stdout bytes.Buffer
	ctx := &Context{Context: context.Background(), Stdout: &stdout}
	require.NoError(t, ClusterSwitch(ctx, struct {
		ConfigCentric
		Cluster string `position:"0" usage:"Name of the cluster to switch to"`
	}{ConfigCentric: ConfigCentric{cfg: cfg}, Cluster: "new"}))
	require.Equal(t, "new", cfg.ActiveCluster())
	state, err := appconfig.LoadAppState("myapp")
	require.NoError(t, err)
	require.Equal(t, "old", state.Cluster)
	_, err = os.Stat(filepath.Join(dir, ".config", "miren", "app-state.toml"))
	require.NoError(t, err)
}

func TestAppClusterSelectionFromParent(t *testing.T) {
	dir := t.TempDir()
	writeAppToml(t, dir, `name = "myapp"`)
	writeDeployToml(t, dir, "[[targets]]\nname = 'dev'\ncluster = 'staging'\n")
	subdir := filepath.Join(dir, "src")
	require.NoError(t, os.Mkdir(subdir, 0755))
	chdir(t, subdir)
	cfg := clientconfig.NewConfig()
	cfg.SetCluster("staging", &clientconfig.ClusterConfig{})
	cfg.SetCluster("outside", &clientconfig.ClusterConfig{})
	require.NoError(t, cfg.SetActiveCluster("outside"))
	app := AppCentric{Dir: ".", ConfigCentric: ConfigCentric{cfg: cfg}}
	require.NoError(t, app.Validate(&GlobalFlags{}))
	_, name, err := app.LoadCluster()
	require.NoError(t, err)
	require.Equal(t, "staging", name)
	require.Equal(t, dir, app.ResolvedDir())

	app.App = "other-app"
	require.NoError(t, app.Validate(&GlobalFlags{}))
	_, name, err = app.LoadCluster()
	require.NoError(t, err)
	require.Equal(t, "outside", name, "another app ignores targets found in a parent directory")
}

func TestTargetSetDefaultWarning(t *testing.T) {
	dir := t.TempDir()
	writeDeployToml(t, dir, "[[targets]]\nname = 'a'\ncluster = 'a'\n[[targets]]\nname = 'b'\ncluster = 'b'\n")
	var stdout bytes.Buffer
	ctx := &Context{Context: context.Background(), Stdout: &stdout}
	warning := captureStderr(t, func() {
		require.NoError(t, DeployTargetSetDefault(ctx, deployTargetNameOpts{AppCentric: AppCentric{Dir: dir}, Name: "b"}))
	})
	require.Contains(t, warning, "deprecated")
	require.NotContains(t, stdout.String(), "deprecated")
	dc, err := appconfig.LoadDeployConfigUnder(dir)
	require.NoError(t, err)
	require.Equal(t, "b", dc.Targets[0].Name, "legacy reordering is preserved")
	stdout.Reset()
	require.NoError(t, DeployTargetList(ctx, deployTargetProjectOpts{AppCentric: AppCentric{Dir: dir}}))
	require.Contains(t, stdout.String(), "LEGACY DEFAULT")
	require.Contains(t, stdout.String(), "order only")
}
