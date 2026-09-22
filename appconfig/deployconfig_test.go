package appconfig

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadDeployConfigUnder(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".miren"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, DeployConfigPath), []byte(`
[[targets]]
name = "staging"
cluster = "miren-staging"

[[targets]]
name = "prod"
cluster = "miren-prod"
cluster_id = "cluster-abc123"
`), 0644))

	dc, err := LoadDeployConfigUnder(dir)
	require.NoError(t, err)
	require.Len(t, dc.Targets, 2)

	defaultTarget, err := dc.Target("")
	require.NoError(t, err)
	require.Equal(t, DeployTarget{Name: "staging", Cluster: "miren-staging"}, *defaultTarget)

	prod, err := dc.Target("prod")
	require.NoError(t, err)
	require.Equal(t, "miren-prod", prod.Cluster)
	require.Equal(t, "cluster-abc123", prod.ClusterID)
}

func TestDeployConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		config  DeployConfig
		wantErr string
	}{
		{name: "no targets", wantErr: "at least one deploy target is required"},
		{name: "missing name", config: DeployConfig{Targets: []DeployTarget{{Cluster: "prod"}}}, wantErr: "name is required"},
		{name: "missing cluster", config: DeployConfig{Targets: []DeployTarget{{Name: "prod"}}}, wantErr: "cluster is required"},
		{
			name: "duplicate name",
			config: DeployConfig{Targets: []DeployTarget{
				{Name: "prod", Cluster: "prod-1"},
				{Name: "prod", Cluster: "prod-2"},
			}},
			wantErr: `duplicate name "prod"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestDeployConfigTargetNotFound(t *testing.T) {
	dc := DeployConfig{Targets: []DeployTarget{{Name: "prod", Cluster: "miren-prod"}}}
	_, err := dc.Target("staging")
	require.EqualError(t, err, `deploy target "staging" not found`)
}

func TestSaveDeployConfigUnder(t *testing.T) {
	dir := t.TempDir()
	dc := &DeployConfig{Targets: []DeployTarget{{
		Name:      "prod",
		Cluster:   "evans-prod",
		ClusterID: "cluster-prod-id",
	}}}

	require.NoError(t, SaveDeployConfigUnder(dir, dc))
	loaded, err := LoadDeployConfigUnder(dir)
	require.NoError(t, err)
	require.Equal(t, dc, loaded)

	require.NoError(t, RemoveDeployConfigUnder(dir))
	loaded, err = LoadDeployConfigUnder(dir)
	require.NoError(t, err)
	require.Nil(t, loaded)
}

func TestDeployConfigTargetMaintenance(t *testing.T) {
	dc := &DeployConfig{Targets: []DeployTarget{{Name: "staging", Cluster: "staging"}}}

	require.NoError(t, dc.AddTarget(DeployTarget{Name: "prod", Cluster: "prod"}, false))
	require.Equal(t, []string{"staging", "prod"}, []string{dc.Targets[0].Name, dc.Targets[1].Name})
	require.EqualError(t, dc.AddTarget(DeployTarget{Name: "prod", Cluster: "other"}, false), `deploy target "prod" already exists`)

	require.NoError(t, dc.SetDefaultTarget("prod"))
	require.Equal(t, []string{"prod", "staging"}, []string{dc.Targets[0].Name, dc.Targets[1].Name})

	require.NoError(t, dc.RemoveTarget("prod"))
	require.Equal(t, []DeployTarget{{Name: "staging", Cluster: "staging"}}, dc.Targets)
}
