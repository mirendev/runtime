package clientconfig

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestTokenIdentityRoundTrip verifies a "token" identity persists its access
// and refresh tokens through a save/reload cycle and lands at mode 0600.
func TestTokenIdentityRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(EnvConfigPath, dir)

	cfg := NewConfig()
	cfg.SetLeafConfig("identity-cloud", &ConfigData{
		Identities: map[string]*IdentityConfig{
			"cloud": {
				Type:         "token",
				Issuer:       "https://miren.cloud",
				Token:        "access.jwt.value",
				RefreshToken: "refresh.jwt.value",
			},
		},
	})
	require.NoError(t, cfg.SaveToHome())

	leafPath := filepath.Join(dir, "clientconfig.d", "identity-cloud.yaml")
	info, err := os.Stat(leafPath)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0600), info.Mode().Perm())

	reloaded, err := LoadConfig()
	require.NoError(t, err)

	id, err := reloaded.GetIdentity("cloud")
	require.NoError(t, err)
	require.Equal(t, IdentityToken, id.Type)
	require.Equal(t, "https://miren.cloud", id.Issuer)
	require.Equal(t, "access.jwt.value", id.Token)
	require.Equal(t, "refresh.jwt.value", id.RefreshToken)
}
