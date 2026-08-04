package clientconfig

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testCAPEM = "-----BEGIN CERTIFICATE-----\ntest\n-----END CERTIFICATE-----\n"

// sandboxEnv sets up the environment and mounted files the sandbox controller
// injects, and returns the token path.
func sandboxEnv(t *testing.T, withCA bool) string {
	t.Helper()

	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "identity-token")
	require.NoError(t, os.WriteFile(tokenPath, []byte("token-abc"), 0644))

	t.Setenv(EnvInCluster, "1")
	t.Setenv(EnvAPIAddress, "10.8.0.1:8443")
	t.Setenv(EnvIdentityTokenPath, tokenPath)

	if withCA {
		caPath := filepath.Join(dir, "ca.crt")
		require.NoError(t, os.WriteFile(caPath, []byte(testCAPEM), 0644))
		t.Setenv(EnvCACertPath, caPath)
	} else {
		t.Setenv(EnvCACertPath, "")
	}

	return tokenPath
}

func TestInCluster(t *testing.T) {
	tokenPath := sandboxEnv(t, true)

	cfg, err := InCluster()
	require.NoError(t, err)

	assert.Equal(t, InClusterName, cfg.ActiveCluster())

	cluster, err := cfg.GetCluster(InClusterName)
	require.NoError(t, err)
	assert.Equal(t, "10.8.0.1:8443", cluster.Hostname)
	assert.Equal(t, tokenPath, cluster.IdentityTokenPath)
	assert.Equal(t, testCAPEM, cluster.CACert)
	assert.Equal(t, APIServerName, cluster.TLSServerName)
}

// Outside a sandbox InCluster must decline with ErrNoConfig, which is what lets
// LoadConfig fall through to a config file.
func TestInCluster_NotInSandbox(t *testing.T) {
	t.Setenv(EnvInCluster, "")

	_, err := InCluster()
	require.ErrorIs(t, err, ErrNoConfig)
}

// A sandbox missing half its injected environment is a bug in the controller,
// not a cue to silently fall back to some other cluster's credentials.
func TestInCluster_IncompleteEnvironment(t *testing.T) {
	tests := map[string]string{
		"no API address": EnvAPIAddress,
		"no token path":  EnvIdentityTokenPath,
	}

	for name, unset := range tests {
		t.Run(name, func(t *testing.T) {
			sandboxEnv(t, true)
			t.Setenv(unset, "")

			_, err := InCluster()
			require.Error(t, err)
			assert.NotErrorIs(t, err, ErrNoConfig, "an incomplete sandbox must not look like no sandbox")
		})
	}
}

func TestInCluster_MissingTokenFile(t *testing.T) {
	sandboxEnv(t, true)
	t.Setenv(EnvIdentityTokenPath, filepath.Join(t.TempDir(), "absent"))

	_, err := InCluster()
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrNoConfig)
}

// Without a CA there is nothing to verify against, so no server name is pinned
// either. The sandbox controller injects both or neither.
func TestInCluster_NoCA(t *testing.T) {
	sandboxEnv(t, false)

	cfg, err := InCluster()
	require.NoError(t, err)

	cluster, err := cfg.GetCluster(InClusterName)
	require.NoError(t, err)
	assert.Empty(t, cluster.CACert)
	assert.Empty(t, cluster.TLSServerName)
}

// The token is read fresh each call rather than cached. It expires hourly and
// the sandbox controller rewrites the file in place -- same path, same inode --
// so a re-read always sees the current token. Caching it would break every
// workload that outlives its first hour, which is the whole point of this path.
// pkg/rpc covers the other half: that the func is consulted per request.
func TestReadIdentityToken(t *testing.T) {
	tokenPath := filepath.Join(t.TempDir(), "identity-token")
	require.NoError(t, os.WriteFile(tokenPath, []byte("token-abc"), 0644))

	token, err := readIdentityToken(tokenPath)
	require.NoError(t, err)
	assert.Equal(t, "token-abc", token)

	// Rewrite in place, as controllers/sandbox/token_refresh.go does.
	require.NoError(t, os.WriteFile(tokenPath, []byte("token-refreshed\n"), 0644))

	token, err = readIdentityToken(tokenPath)
	require.NoError(t, err)
	assert.Equal(t, "token-refreshed", token, "must re-read, and trim the trailing newline")
}

func TestReadIdentityToken_Missing(t *testing.T) {
	_, err := readIdentityToken(filepath.Join(t.TempDir(), "absent"))
	require.Error(t, err)
}

// The in-cluster branch must win over the identity/cert/insecure paths and
// produce usable dial options.
func TestRPCOptions_InClusterBranch(t *testing.T) {
	sandboxEnv(t, true)

	cfg, err := InCluster()
	require.NoError(t, err)

	opts, err := cfg.RPCOptions(t.Context())
	require.NoError(t, err)
	require.NotEmpty(t, opts)

	// The options must build a usable client state: bearer-only, verifying
	// against the injected CA under the pinned server name.
	_, err = cfg.State(t.Context())
	require.NoError(t, err)
}

// LoadConfig prefers a real config file: a developer must be able to point the
// CLI at another cluster from inside a sandbox.
func TestLoadConfig_ExplicitConfigBeatsInCluster(t *testing.T) {
	sandboxEnv(t, true)

	dir := t.TempDir()
	configPath := filepath.Join(dir, "clientconfig.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte(`
active_cluster: elsewhere
clusters:
  elsewhere:
    hostname: other.example.com:8443
`), 0644))
	t.Setenv(EnvConfigPath, configPath)

	cfg, err := LoadConfig()
	require.NoError(t, err)
	assert.Equal(t, "elsewhere", cfg.ActiveCluster())
}

// With no config file, a sandbox falls back to its own identity, so `m` works
// out of the box inside a container.
func TestLoadConfig_FallsBackToInCluster(t *testing.T) {
	sandboxEnv(t, true)
	t.Setenv(EnvConfigPath, filepath.Join(t.TempDir(), "absent.yaml"))

	cfg, err := LoadConfig()
	require.NoError(t, err)
	assert.Equal(t, InClusterName, cfg.ActiveCluster())
}

// Outside a sandbox with no config file, the original ErrNoConfig still
// surfaces rather than an in-cluster error.
func TestLoadConfig_NoConfigNoSandbox(t *testing.T) {
	t.Setenv(EnvInCluster, "")
	t.Setenv(EnvConfigPath, filepath.Join(t.TempDir(), "absent.yaml"))

	_, err := LoadConfig()
	require.True(t, errors.Is(err, ErrNoConfig), "got %v", err)
}
