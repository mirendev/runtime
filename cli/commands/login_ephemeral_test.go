package commands

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"

	"miren.dev/runtime/clientconfig"
)

// mockCloud is a fake miren.cloud implementing just enough of the device flow,
// key registration, and cluster listing for the login flow to run end-to-end.
type mockCloud struct {
	keyBeginHits    atomic.Int32
	keyCompleteHits atomic.Int32
	withRefresh     bool
}

func freshLoginJWT(t *testing.T) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"exp": float64(time.Now().Add(time.Hour).Unix()),
		"sub": "user@example.com",
	})
	s, err := tok.SignedString([]byte("secret"))
	require.NoError(t, err)
	return s
}

func (m *mockCloud) server(t *testing.T) *httptest.Server {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/v1/device/code", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"device_code":      "dev-code",
			"user_code":        "ABCD-1234",
			"verification_uri": "https://example.com/verify",
			"expires_in":       600,
			"polling_interval": 1,
		})
	})

	mux.HandleFunc("/api/v1/device/token", func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"status":       "authorized",
			"access_token": freshLoginJWT(t),
			"token_type":   "Bearer",
			"expires_in":   3600,
		}
		if m.withRefresh {
			resp["refresh_token"] = freshLoginJWT(t)
		}
		_ = json.NewEncoder(w).Encode(resp)
	})

	mux.HandleFunc("/api/v1/users/keys/begin", func(w http.ResponseWriter, r *http.Request) {
		m.keyBeginHits.Add(1)
		// 409 is treated as "already registered" success by registerPublicKey,
		// which keeps the persistent-key test from needing a signing round-trip.
		w.WriteHeader(http.StatusConflict)
	})
	mux.HandleFunc("/api/v1/users/keys/complete", func(w http.ResponseWriter, r *http.Request) {
		m.keyCompleteHits.Add(1)
		w.WriteHeader(http.StatusOK)
	})

	mux.HandleFunc("/api/v1/users/clusters", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"clusters": []any{}})
	})

	return httptest.NewServer(mux)
}

// TestLoginEphemeralDefault is the MIR-1385 acceptance test: a default login must
// store a token identity and never touch the key-registration endpoints.
func TestLoginEphemeralDefault(t *testing.T) {
	mc := &mockCloud{withRefresh: true}
	srv := mc.server(t)
	defer srv.Close()

	dir := t.TempDir()
	t.Setenv("MIREN_CONFIG", dir)

	ctx := newTestContext()
	err := login(ctx, srv.URL, "cloud", "miren-cli", false, false, false)
	require.NoError(t, err)

	// The whole point: no persistent key registered.
	require.Zero(t, mc.keyBeginHits.Load(), "ephemeral login must not begin key registration")
	require.Zero(t, mc.keyCompleteHits.Load(), "ephemeral login must not complete key registration")

	// Identity leaf is a token identity carrying both tokens, at mode 0600.
	leaf := filepath.Join(dir, "clientconfig.d", "identity-cloud.yaml")
	info, err := os.Stat(leaf)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0600), info.Mode().Perm())

	cfg, err := clientconfig.LoadConfig()
	require.NoError(t, err)
	id, err := cfg.GetIdentity("cloud")
	require.NoError(t, err)
	require.Equal(t, "token", id.Type)
	require.NotEmpty(t, id.Token)
	require.NotEmpty(t, id.RefreshToken)

	// No key file should have been written.
	_, err = os.Stat(filepath.Join(dir, "clientconfig.d", "key-miren-cli.yaml"))
	require.True(t, os.IsNotExist(err), "ephemeral login must not write a key file")
}

// TestLoginPersistentKeyFlag verifies --persistent-key still registers a key and
// stores a keypair identity.
func TestLoginPersistentKeyFlag(t *testing.T) {
	mc := &mockCloud{withRefresh: true}
	srv := mc.server(t)
	defer srv.Close()

	dir := t.TempDir()
	t.Setenv("MIREN_CONFIG", dir)

	ctx := newTestContext()
	err := login(ctx, srv.URL, "cloud", "miren-cli", false, false, true /* persistentKey */)
	require.NoError(t, err)

	require.Equal(t, int32(1), mc.keyBeginHits.Load(), "persistent-key login must register a key")

	cfg, err := clientconfig.LoadConfig()
	require.NoError(t, err)
	id, err := cfg.GetIdentity("cloud")
	require.NoError(t, err)
	require.Equal(t, "keypair", id.Type)
	require.Empty(t, id.Token)

	_, err = os.Stat(filepath.Join(dir, "clientconfig.d", "key-miren-cli.yaml"))
	require.NoError(t, err, "persistent-key login must write a key file")
}

// TestLoginFallsBackWhenNoRefreshToken verifies that if the cloud returns no
// refresh token, a default login falls back to the persistent-key flow rather
// than storing an unusable token identity.
func TestLoginFallsBackWhenNoRefreshToken(t *testing.T) {
	mc := &mockCloud{withRefresh: false}
	srv := mc.server(t)
	defer srv.Close()

	dir := t.TempDir()
	t.Setenv("MIREN_CONFIG", dir)

	ctx := newTestContext()
	err := login(ctx, srv.URL, "cloud", "miren-cli", false, false, false)
	require.NoError(t, err)

	require.Equal(t, int32(1), mc.keyBeginHits.Load(), "should fall back to key registration")

	cfg, err := clientconfig.LoadConfig()
	require.NoError(t, err)
	id, err := cfg.GetIdentity("cloud")
	require.NoError(t, err)
	require.Equal(t, "keypair", id.Type)
}
