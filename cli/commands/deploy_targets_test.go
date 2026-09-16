package commands

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"miren.dev/runtime/appconfig"
	"miren.dev/runtime/clientconfig"
)

func writeDeployToml(t *testing.T, dir, content string) {
	t.Helper()
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
	cfg.SetCluster("evans-prod", &clientconfig.ClusterConfig{
		Hostname: "prod.example.com:8443",
		XID:      "cluster-prod-id",
	})

	t.Run("first target is the default", func(t *testing.T) {
		opts := deployOpts{AppCentric: AppCentric{Dir: dir}}
		require.NoError(t, opts.Validate(&GlobalFlags{}))
		require.Equal(t, "miren-staging", opts.Cluster)
	})

	t.Run("named target selects its cluster", func(t *testing.T) {
		opts := deployOpts{
			AppCentric: AppCentric{Dir: dir, ConfigCentric: ConfigCentric{cfg: cfg}},
			Target:     "prod",
		}
		require.NoError(t, opts.Validate(&GlobalFlags{}))
		require.Equal(t, "evans-prod", opts.Cluster)
	})

	t.Run("explicit cluster overrides the default target", func(t *testing.T) {
		opts := deployOpts{AppCentric: AppCentric{Dir: dir, ConfigCentric: ConfigCentric{Cluster: "other"}}}
		require.NoError(t, opts.Validate(&GlobalFlags{}))
		require.Equal(t, "other", opts.Cluster)
	})

	t.Run("target and explicit cluster conflict", func(t *testing.T) {
		opts := deployOpts{
			AppCentric: AppCentric{Dir: dir, ConfigCentric: ConfigCentric{Cluster: "other"}},
			Target:     "prod",
		}
		require.EqualError(t, opts.Validate(&GlobalFlags{}), "--target and --cluster cannot be combined")
	})
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
