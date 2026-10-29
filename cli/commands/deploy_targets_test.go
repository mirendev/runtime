package commands

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"miren.dev/runtime/appconfig"
	"miren.dev/runtime/clientconfig"
	"miren.dev/runtime/pkg/rpc"
)

func TestDeployTerminal(t *testing.T) {
	if scenario := os.Getenv("MIREN_TEST_DEPLOY_TERMINAL"); scenario != "" {
		t.Setenv("HOME", t.TempDir())
		t.Setenv("MIREN_APP", "")
		t.Setenv("MIREN_RUNTIME_APP", "")
		t.Setenv("MIREN_CLUSTER", "")
		t.Setenv("CI", "")
		if scenario == "unattended" {
			t.Setenv("CI", "1")
		}
		dir := t.TempDir()
		chdir(t, dir)
		cfg := clientconfig.NewConfig()
		cfg.SetIdentity("cloud", &clientconfig.IdentityConfig{Type: clientconfig.IdentityKeypair})
		cmd := Infer("deploy", "test", Deploy)
		args := []string{}
		if scenario == "delete" {
			cmd = Infer("app delete", "test", AppDelete)
		} else {
			writeAppToml(t, dir, `name = "myapp"`)
		}
		switch scenario {
		case "clusters", "targets", "parent", "explicit", "sole":
			server, err := rpc.NewState(t.Context(), rpc.WithBindAddr("localhost:0"), rpc.WithAuthenticator(&rpc.NoOpAuthenticator{}))
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, server.Close()) })
			// No build service: selection can proceed, but no deployment can start.
			cfg.SetCluster("a-staging", &clientconfig.ClusterConfig{Hostname: server.ListenAddr(), Insecure: true})
			if scenario != "sole" {
				cfg.SetCluster("z-prod", &clientconfig.ClusterConfig{Hostname: server.ListenAddr(), Insecure: true})
				require.NoError(t, cfg.SetActiveCluster("z-prod"))
			}
			if scenario == "targets" || scenario == "parent" {
				writeDeployToml(t, dir, "[[targets]]\nname = 'dev'\ncluster = 'a-staging'\n[[targets]]\nname = 'production'\ncluster = 'z-prod'\n")
			}
			if scenario == "parent" {
				child := filepath.Join(dir, "child")
				require.NoError(t, os.Mkdir(child, 0755))
				chdir(t, child)
			}
			if scenario == "explicit" {
				args = append(args, "-C", "a-staging")
			}
		case "json":
			args = append(args, "--json")
		case "format-json", "jsonl":
			format := "json"
			if scenario == "jsonl" {
				format = "jsonl"
			}
			args = append(args, "--format", format)
		}
		configPath := filepath.Join(t.TempDir(), "clientconfig.yaml")
		require.NoError(t, cfg.SaveTo(configPath))
		require.NoError(t, cmd.fs.Parse(append(args, "--config", configPath)))
		err := cmd.Invoke()
		if scenario == "parent" || scenario == "explicit" {
			require.NoError(t, err, "declining a deploy exits successfully")
		} else {
			require.Error(t, err)
		}
		fmt.Printf("COMMAND_RESULT: %v\n", err)
		if scenario == "delete" {
			_, err := os.Stat(filepath.Join(dir, ".miren"))
			require.True(t, os.IsNotExist(err), "delete must not initialize an app")
		}
		return
	}
	if runtime.GOOS != "linux" {
		t.Skip("terminal regression test uses util-linux script")
	}
	script, err := exec.LookPath("script")
	if err != nil {
		t.Skip("terminal regression test requires script")
	}
	binary, err := os.Executable()
	require.NoError(t, err)
	for _, tc := range []struct {
		name    string
		prompts []string
		keys    []string
		want    string
	}{
		{"onboarding", []string{"Would you like to add one now?"}, []string{"n"}, "COMMAND_RESULT: no clusters configured"},
		{"clusters", []string{"Select a cluster:"}, []string{"\r"}, "Deploying: myapp → a-staging"},
		{"targets", []string{"Select a cluster:"}, []string{"\r"}, "Deploying: myapp → a-staging"},
		{"parent", []string{"Select a cluster:", "Deploy app 'myapp' to cluster 'a-staging'?"}, []string{"\r", "n"}, "deployment cancelled"},
		{"explicit", []string{"Deploy app 'myapp' to cluster 'a-staging'?"}, []string{"n"}, "deployment cancelled"},
		{"sole", nil, nil, "Deploying: myapp → a-staging"},
		{"json", nil, nil, "no clusters configured"},
		{"format-json", nil, nil, "no clusters configured"},
		{"jsonl", nil, nil, "no clusters configured"},
		{"unattended", nil, nil, "no clusters configured"},
		{"delete", nil, nil, "app is required; pass -a <name>"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, script, "-qefc", fmt.Sprintf("%q -test.run=^TestDeployTerminal$ -test.v", binary), "/dev/null")
			cmd.Env = append(os.Environ(), "MIREN_TEST_DEPLOY_TERMINAL="+tc.name, "TERM=dumb", "NO_COLOR=1")
			input, err := cmd.StdinPipe()
			require.NoError(t, err)
			defer input.Close()
			output, err := cmd.StdoutPipe()
			require.NoError(t, err)
			cmd.Stderr = cmd.Stdout
			require.NoError(t, cmd.Start())
			var transcript strings.Builder
			next := 0
			buf := make([]byte, 4096)
			for {
				n, err := output.Read(buf)
				transcript.Write(buf[:n])
				if next < len(tc.prompts) && strings.Contains(transcript.String(), tc.prompts[next]) {
					_, writeErr := io.WriteString(input, tc.keys[next])
					require.NoError(t, writeErr)
					next++
				}
				if err != nil {
					require.ErrorIs(t, err, io.EOF)
					break
				}
			}
			text := transcript.String()
			t.Log(text)
			require.NoError(t, cmd.Wait(), text)
			require.Equal(t, len(tc.prompts), next)
			require.Contains(t, text, tc.want)
			require.Contains(t, text, "COMMAND_RESULT:")
			if tc.name == "clusters" || tc.name == "targets" || tc.name == "sole" {
				require.Contains(t, text, "Deploying")
				require.NotContains(t, text, "Deploy app '")
			} else if len(tc.prompts) == 0 {
				require.NotContains(t, text, "Would you like")
				require.NotContains(t, text, "Run 'miren init'")
			}
		})
	}
}

func writeDeployToml(t *testing.T, dir, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".miren"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".miren", "deploy.toml"), []byte(content), 0644))
}

func TestDeployOptsValidateTarget(t *testing.T) {
	dir := t.TempDir()
	writeAppToml(t, dir, `name = "myapp"`)
	writeDeployToml(t, dir, `
[[targets]]
name = "staging"
cluster = "miren-staging"

[[targets]]
name = "prod"
cluster = "miren-prod"
cluster_id = "cluster-prod-id"
`)

	cfg := clientconfig.NewConfig()
	cfg.SetCluster("miren-staging", &clientconfig.ClusterConfig{
		Hostname: "staging.example.com:8443",
	})
	cfg.SetCluster("evans-prod", &clientconfig.ClusterConfig{
		Hostname: "prod.example.com:8443",
		XID:      "cluster-prod-id",
	})

	t.Run("multiple targets require explicit selection", func(t *testing.T) {
		t.Setenv("CI", "1")
		opts := deployOpts{AppCentric: AppCentric{Dir: dir, ConfigCentric: ConfigCentric{cfg: cfg}}}
		require.ErrorContains(t, opts.Validate(&GlobalFlags{}), "pass -C")
	})

	t.Run("named target selects its cluster", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		opts := deployOpts{
			AppCentric: AppCentric{Dir: dir, ConfigCentric: ConfigCentric{cfg: cfg}},
			Target:     "prod",
		}
		require.NoError(t, opts.Validate(&GlobalFlags{}))
		require.Empty(t, opts.Cluster)
		require.Equal(t, "evans-prod", opts.targetCluster)
		cluster, name, err := opts.LoadCluster()
		require.NoError(t, err)
		require.NotNil(t, cluster)
		require.Equal(t, "evans-prod", name)
		state, err := appconfig.LoadAppState("myapp")
		require.NoError(t, err)
		require.Nil(t, state)
	})

	t.Run("explicit cluster must be a target", func(t *testing.T) {
		opts := deployOpts{AppCentric: AppCentric{Dir: dir, ConfigCentric: ConfigCentric{Cluster: "other", cfg: cfg}}}
		require.ErrorContains(t, opts.Validate(&GlobalFlags{}), "not a deploy target")
	})

	t.Run("different app still uses source directory targets", func(t *testing.T) {
		opts := deployOpts{
			AppCentric: AppCentric{App: "other-app", Dir: dir, ConfigCentric: ConfigCentric{cfg: cfg}},
			Target:     "staging",
		}
		require.NoError(t, opts.Validate(&GlobalFlags{}))
		_, name, err := opts.LoadCluster()
		require.NoError(t, err)
		require.Equal(t, "miren-staging", name)
		require.Equal(t, "other-app", opts.App)

		opts.Target = ""
		opts.Cluster = "outside"
		require.ErrorContains(t, opts.Validate(&GlobalFlags{}), "not a deploy target")
	})

	t.Run("target and explicit cluster conflict", func(t *testing.T) {
		opts := deployOpts{
			AppCentric: AppCentric{Dir: dir, ConfigCentric: ConfigCentric{Cluster: "other"}},
			Target:     "prod",
		}
		require.EqualError(t, opts.Validate(&GlobalFlags{}), "--target cannot be combined with --cluster or MIREN_CLUSTER")
	})
}

func TestDeployOptsExplicitClusterRejectsInvalidDeployConfig(t *testing.T) {
	dir := t.TempDir()
	writeAppToml(t, dir, `name = "myapp"`)
	writeDeployToml(t, dir, `not valid toml =`)

	opts := deployOpts{AppCentric: AppCentric{Dir: dir, ConfigCentric: ConfigCentric{Cluster: "other"}}}
	require.ErrorContains(t, opts.Validate(&GlobalFlags{}), "error loading .miren/deploy.toml")
}

func TestDeployOptsValidateTargetWithUnknownClusterID(t *testing.T) {
	dir := t.TempDir()
	writeAppToml(t, dir, `name = "myapp"`)
	writeDeployToml(t, dir, `
[[targets]]
name = "prod"
cluster = "miren-prod"
cluster_id = "cluster-missing"
`)

	opts := deployOpts{
		AppCentric: AppCentric{Dir: dir, ConfigCentric: ConfigCentric{cfg: clientconfig.NewConfig()}},
		Target:     "prod",
	}
	err := opts.Validate(&GlobalFlags{})
	require.EqualError(t, err, `cluster id "cluster-missing" for deploy target "prod" is not configured; run 'miren cluster add'`)
}

func TestDeployOptsValidateTargetWithUnknownClusterName(t *testing.T) {
	dir := t.TempDir()
	writeAppToml(t, dir, `name = "myapp"`)
	writeDeployToml(t, dir, `
[[targets]]
name = "staging"
cluster = "miren-staging"
`)

	opts := deployOpts{
		AppCentric: AppCentric{Dir: dir, ConfigCentric: ConfigCentric{cfg: clientconfig.NewConfig()}},
		Target:     "staging",
	}
	err := opts.Validate(&GlobalFlags{})
	require.EqualError(t, err, `cluster "miren-staging" for deploy target "staging" is not configured; run 'miren cluster add'`)
}

func TestDeployOptsValidateTargetWithoutClientConfig(t *testing.T) {
	dir := t.TempDir()
	writeAppToml(t, dir, `name = "myapp"`)
	writeDeployToml(t, dir, `
[[targets]]
name = "prod"
cluster = "miren-prod"
cluster_id = "cluster-prod-id"
`)

	opts := deployOpts{
		AppCentric: AppCentric{Dir: dir, ConfigCentric: ConfigCentric{Config: filepath.Join(dir, "missing.yaml")}},
		Target:     "prod",
	}
	err := opts.Validate(&GlobalFlags{})
	require.EqualError(t, err, "no client configuration available; run 'miren login' to authenticate and 'miren cluster add' to configure the cluster for deploy target \"prod\"")
}

func TestDeployOptsValidateTargetWithoutConfig(t *testing.T) {
	dir := t.TempDir()
	writeAppToml(t, dir, `name = "myapp"`)
	opts := deployOpts{AppCentric: AppCentric{Dir: dir}, Target: "prod"}
	require.EqualError(t, opts.Validate(&GlobalFlags{}), "--target requires .miren/deploy.toml")
}

func TestDeployTargetCommandsMaintainConfig(t *testing.T) {
	dir := t.TempDir()
	writeAppToml(t, dir, `name = "myapp"`)

	cfg := clientconfig.NewConfig()
	cfg.SetCluster("evans-staging", &clientconfig.ClusterConfig{XID: "cluster-staging-id"})
	cfg.SetCluster("evans-prod", &clientconfig.ClusterConfig{XID: "cluster-prod-id"})

	var output bytes.Buffer
	ctx := &Context{Context: context.Background(), Stdout: &output}
	staging := deployTargetAddOpts{
		AppCentric:  AppCentric{Dir: dir, ConfigCentric: ConfigCentric{cfg: cfg}},
		Name:        "staging",
		ClusterName: "evans-staging",
	}
	require.NoError(t, DeployTargetAdd(ctx, staging))

	prod := deployTargetAddOpts{
		AppCentric:  AppCentric{Dir: dir, ConfigCentric: ConfigCentric{cfg: cfg}},
		Name:        "prod",
		ClusterName: "evans-prod",
		Default:     true,
	}
	require.NoError(t, DeployTargetAdd(ctx, prod))

	dc, err := appconfig.LoadDeployConfigUnder(dir)
	require.NoError(t, err)
	require.Equal(t, []appconfig.DeployTarget{
		{Name: "prod", Cluster: "evans-prod", ClusterID: "cluster-prod-id"},
		{Name: "staging", Cluster: "evans-staging", ClusterID: "cluster-staging-id"},
	}, dc.Targets)

	require.NoError(t, DeployTargetSetDefault(ctx, deployTargetNameOpts{AppCentric: AppCentric{Dir: dir}, Name: "staging"}))
	require.NoError(t, DeployTargetRemove(ctx, deployTargetNameOpts{AppCentric: AppCentric{Dir: dir}, Name: "prod"}))
	dc, err = appconfig.LoadDeployConfigUnder(dir)
	require.NoError(t, err)
	require.Equal(t, []appconfig.DeployTarget{{Name: "staging", Cluster: "evans-staging", ClusterID: "cluster-staging-id"}}, dc.Targets)

	require.NoError(t, DeployTargetRemove(ctx, deployTargetNameOpts{AppCentric: AppCentric{Dir: dir}, Name: "staging"}))
	dc, err = appconfig.LoadDeployConfigUnder(dir)
	require.NoError(t, err)
	require.Nil(t, dc)
}

func TestDeployTargetAddRequiresNameNonInteractively(t *testing.T) {
	t.Setenv("CI", "1")
	var output bytes.Buffer
	ctx := &Context{Context: context.Background(), Stdout: &output}
	err := DeployTargetAdd(ctx, deployTargetAddOpts{})
	require.EqualError(t, err, "name is required in non-interactive mode")
}
