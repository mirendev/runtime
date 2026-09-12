package commands

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"miren.dev/runtime/appconfig"
	"miren.dev/runtime/clientconfig"
)

func TestCheckFingerprint(t *testing.T) {
	t.Run("prefix stripped", func(t *testing.T) {
		r := require.New(t)
		r.NoError(checkFingerprint("sha1:abcdef", "abcdef"))
	})

	t.Run("case insensitive", func(t *testing.T) {
		r := require.New(t)
		r.NoError(checkFingerprint("ABCDEF", "abcdef"))
	})

	t.Run("prefix and case insensitive", func(t *testing.T) {
		r := require.New(t)
		r.NoError(checkFingerprint("sha1:ABCDEF", "abcdef"))
	})

	t.Run("mismatch error", func(t *testing.T) {
		r := require.New(t)
		err := checkFingerprint("wrong", "abcdef")
		r.Error(err)
		r.Contains(err.Error(), "abcdef")
		r.Contains(err.Error(), "wrong")
	})

	t.Run("empty expected skips verification", func(t *testing.T) {
		r := require.New(t)
		r.NoError(checkFingerprint("", "abcdef"))
	})
}

// TestLoadClusterFromCluster covers the unified resolution path that handles
// both -C and MIREN_CLUSTER (which now flows through the env:"MIREN_CLUSTER"
// struct tag in production, populating Cluster identically to -C).
func TestLoadClusterFromCluster(t *testing.T) {
	t.Run("known cluster name", func(t *testing.T) {
		r := require.New(t)

		cfg := clientconfig.NewConfig()
		cfg.SetCluster("known-name", &clientconfig.ClusterConfig{
			Hostname: "10.0.0.1:8443",
		})

		cc := ConfigCentric{Cluster: "known-name", cfg: cfg}

		cluster, name, err := cc.LoadCluster()
		r.NoError(err)
		r.Equal("known-name", name)
		r.NotNil(cluster)
		r.Equal("10.0.0.1:8443", cluster.Hostname)
	})

	t.Run("known name with fingerprint suffix still matches", func(t *testing.T) {
		r := require.New(t)

		cfg := clientconfig.NewConfig()
		cfg.SetCluster("known-name;sha1:extra", &clientconfig.ClusterConfig{
			Hostname: "10.0.0.1:8443",
		})

		cc := ConfigCentric{Cluster: "known-name;sha1:extra", cfg: cfg}

		cluster, name, err := cc.LoadCluster()
		r.NoError(err)
		r.Equal("known-name;sha1:extra", name)
		r.NotNil(cluster)
	})

	t.Run("unknown address falls through", func(t *testing.T) {
		r := require.New(t)

		cfg := clientconfig.NewConfig()
		cc := ConfigCentric{Cluster: "unknown-addr:8443", cfg: cfg}

		_, _, err := cc.LoadCluster()
		r.Error(err)
		r.Contains(err.Error(), "unknown-addr:8443")
	})

	t.Run("unknown address with fingerprint uses only address", func(t *testing.T) {
		r := require.New(t)

		cfg := clientconfig.NewConfig()
		cc := ConfigCentric{Cluster: "unknown-addr:8443;sha1:abc", cfg: cfg}

		_, _, err := cc.LoadCluster()
		r.Error(err)
		r.Contains(err.Error(), "unknown-addr:8443")
		// The fingerprint portion should not appear in the address argument of
		// the failed-to-connect clause (it is part of the cluster %q prefix).
		r.Contains(err.Error(), `to "unknown-addr:8443":`)
	})
}

// setupAppDir creates a temp directory with .miren/app.toml and changes into it.
// It also sets HOME so appconfig.SaveAppState/LoadAppState use a temp location.
// Returns a cleanup function that restores the original working directory and HOME.
func setupAppDir(t *testing.T, appName string) string {
	t.Helper()

	dir := t.TempDir()

	// Create .miren/app.toml so LoadAppConfig() finds it
	mirenDir := filepath.Join(dir, ".miren")
	require.NoError(t, os.MkdirAll(mirenDir, 0755))
	require.NoError(t, os.WriteFile(
		filepath.Join(mirenDir, "app.toml"),
		[]byte("name = "+`"`+appName+`"`+"\n"),
		0644,
	))

	// Set HOME so app state goes to our temp dir
	t.Setenv("HOME", dir)

	chdir(t, dir)

	return dir
}

func TestConfigCentricPerAppState(t *testing.T) {
	t.Run("resolves per-app cluster in app directory", func(t *testing.T) {
		r := require.New(t)
		setupAppDir(t, "my-app")

		// Save per-app state pointing to a specific cluster
		r.NoError(appconfig.SaveAppState("my-app", &appconfig.AppState{Cluster: "app-cluster"}))

		cfg := clientconfig.NewConfig()
		cfg.SetCluster("app-cluster", &clientconfig.ClusterConfig{Hostname: "10.0.0.1:8443"})
		cfg.SetCluster("global-cluster", &clientconfig.ClusterConfig{Hostname: "10.0.0.2:8443"})
		r.NoError(cfg.SetActiveCluster("global-cluster"))

		cc := ConfigCentric{cfg: cfg}
		cluster, name, err := cc.LoadCluster()
		r.NoError(err)
		r.Equal("app-cluster", name)
		r.NotNil(cluster)
		r.Equal("10.0.0.1:8443", cluster.Hostname)
	})

	t.Run("falls back to global default without app directory", func(t *testing.T) {
		r := require.New(t)

		// Use a temp dir without .miren/app.toml
		chdir(t, t.TempDir())

		cfg := clientconfig.NewConfig()
		cfg.SetCluster("global-cluster", &clientconfig.ClusterConfig{Hostname: "10.0.0.2:8443"})
		r.NoError(cfg.SetActiveCluster("global-cluster"))

		cc := ConfigCentric{cfg: cfg}
		cluster, name, err := cc.LoadCluster()
		r.NoError(err)
		r.Equal("global-cluster", name)
		r.NotNil(cluster)
		r.Equal("10.0.0.2:8443", cluster.Hostname)
	})

	t.Run("unknown cluster in app state falls through to global", func(t *testing.T) {
		r := require.New(t)
		setupAppDir(t, "my-app")

		// Save per-app state with a cluster that doesn't exist in config
		r.NoError(appconfig.SaveAppState("my-app", &appconfig.AppState{Cluster: "deleted-cluster"}))

		cfg := clientconfig.NewConfig()
		cfg.SetCluster("global-cluster", &clientconfig.ClusterConfig{Hostname: "10.0.0.2:8443"})
		r.NoError(cfg.SetActiveCluster("global-cluster"))

		cc := ConfigCentric{cfg: cfg}
		cluster, name, err := cc.LoadCluster()
		r.NoError(err)
		r.Equal("global-cluster", name)
		r.NotNil(cluster)
	})

	t.Run("-C flag overrides per-app state", func(t *testing.T) {
		r := require.New(t)
		setupAppDir(t, "my-app")

		r.NoError(appconfig.SaveAppState("my-app", &appconfig.AppState{Cluster: "app-cluster"}))

		cfg := clientconfig.NewConfig()
		cfg.SetCluster("app-cluster", &clientconfig.ClusterConfig{Hostname: "10.0.0.1:8443"})
		cfg.SetCluster("flag-cluster", &clientconfig.ClusterConfig{Hostname: "10.0.0.3:8443"})

		cc := ConfigCentric{Cluster: "flag-cluster", cfg: cfg}
		cluster, name, err := cc.LoadCluster()
		r.NoError(err)
		r.Equal("flag-cluster", name)
		r.NotNil(cluster)
		r.Equal("10.0.0.3:8443", cluster.Hostname)
	})

	t.Run("MIREN_CLUSTER env overrides per-app state", func(t *testing.T) {
		r := require.New(t)
		setupAppDir(t, "my-app")

		r.NoError(appconfig.SaveAppState("my-app", &appconfig.AppState{Cluster: "app-cluster"}))

		cfg := clientconfig.NewConfig()
		cfg.SetCluster("app-cluster", &clientconfig.ClusterConfig{Hostname: "10.0.0.1:8443"})
		cfg.SetCluster("env-cluster", &clientconfig.ClusterConfig{Hostname: "10.0.0.4:8443"})

		// In production, mflags reads env:"MIREN_CLUSTER" into Cluster during
		// flag parsing. Simulate that here by populating Cluster directly.
		cc := ConfigCentric{Cluster: "env-cluster", cfg: cfg}

		cluster, name, err := cc.LoadCluster()
		r.NoError(err)
		r.Equal("env-cluster", name)
		r.NotNil(cluster)
		r.Equal("10.0.0.4:8443", cluster.Hostname)
	})
}
