package commands

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"

	"miren.dev/runtime/clientconfig"
	"miren.dev/runtime/pkg/cloudauth"
)

// mockCloud is a fake miren.cloud implementing just enough of the device flow,
// key registration, and cluster listing for the login flow to run end-to-end.
type mockCloud struct {
	keyBeginHits    atomic.Int32
	keyCompleteHits atomic.Int32
	deviceCodeHits  atomic.Int32
	withRefresh     bool
	// held keeps the device flow pending until it is cleared, standing in for
	// a person who hasn't approved the login yet.
	held atomic.Bool
	// revoked holds access tokens the cloud no longer accepts, though they
	// haven't expired locally.
	revoked sync.Map
	// noMe makes the cloud predate the /api/v1/me endpoint.
	noMe bool
	// keys maps each registered key's fingerprint to "active" or "revoked".
	// Like the real cloud, revoking keeps the fingerprint, and registering a
	// fingerprint again fails on its unique index.
	keysMu sync.Mutex
	keys   map[string]string
	// keyTokens are the access tokens issued for each key, revoked with it.
	keyTokens map[string][]string
	// failKeyRegister makes key registration fail with a server error.
	failKeyRegister bool
}

var jtiCounter atomic.Int64

func freshLoginJWT(t *testing.T) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"exp": float64(time.Now().Add(time.Hour).Unix()),
		"sub": "user@example.com",
		// Unique per token, so tokens minted in the same second differ.
		"jti": fmt.Sprint(jtiCounter.Add(1)),
	})
	s, err := tok.SignedString([]byte("secret"))
	require.NoError(t, err)
	return s
}

// revokeKeys revokes every registered key, as the cloud does manually or
// once a user exceeds their active key limit.
func (m *mockCloud) revokeKeys() {
	m.keysMu.Lock()
	defer m.keysMu.Unlock()
	for fp := range m.keys {
		m.keys[fp] = "revoked"
		for _, tok := range m.keyTokens[fp] {
			m.revoked.Store(tok, true)
		}
	}
}

func publicKeyFingerprint(t *testing.T, pemData string) string {
	pub, err := cloudauth.PublicKeyFromPEM(pemData)
	require.NoError(t, err)
	return (&cloudauth.KeyPair{PublicKey: pub.(ed25519.PublicKey)}).Fingerprint()
}

func (m *mockCloud) server(t *testing.T) *httptest.Server {
	mux := http.NewServeMux()
	m.keys = map[string]string{}
	m.keyTokens = map[string][]string{}

	mux.HandleFunc("/api/v1/device/code", func(w http.ResponseWriter, r *http.Request) {
		m.deviceCodeHits.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"device_code":      "dev-code",
			"user_code":        "ABCD-1234",
			"verification_uri": "https://example.com/verify",
			"expires_in":       600,
			"polling_interval": 1,
		})
	})

	mux.HandleFunc("/api/v1/device/token", func(w http.ResponseWriter, r *http.Request) {
		if m.held.Load() {
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "pending"})
			return
		}
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

	// Exercise the real begin -> sign -> complete round trip rather than the 409
	// "already registered" shortcut, so tests can prove a key was fully
	// registered and not merely that begin was called.
	mux.HandleFunc("/api/v1/users/keys/begin", func(w http.ResponseWriter, r *http.Request) {
		if m.failKeyRegister {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		var req BeginKeyRegistrationRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		m.keyBeginHits.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"envelope":  publicKeyFingerprint(t, req.PublicKey),
			"challenge": base64.StdEncoding.EncodeToString([]byte("challenge-bytes")),
		})
	})
	mux.HandleFunc("/api/v1/users/keys/complete", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Envelope  string `json:"envelope"`
			Signature string `json:"signature"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		require.NotEmpty(t, req.Signature, "complete must carry a signature over the challenge")
		m.keyCompleteHits.Add(1)
		m.keysMu.Lock()
		defer m.keysMu.Unlock()
		if _, exists := m.keys[req.Envelope]; exists {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to complete key registration"})
			return
		}
		m.keys[req.Envelope] = "active"
		w.WriteHeader(http.StatusOK)
	})

	// Every refresh token is treated as revoked, so an identity whose access
	// token has expired can no longer authenticate.
	mux.HandleFunc("/auth/refresh", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})

	mux.HandleFunc("/auth/user-key/begin", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Fingerprint string `json:"fingerprint"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		m.keysMu.Lock()
		status := m.keys[req.Fingerprint]
		m.keysMu.Unlock()
		if status != "active" {
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "unknown key"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"envelope":  req.Fingerprint,
			"challenge": base64.URLEncoding.EncodeToString([]byte("challenge-bytes")),
		})
	})
	mux.HandleFunc("/auth/user-key/complete", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Envelope string `json:"envelope"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		tok := freshLoginJWT(t)
		m.keysMu.Lock()
		m.keyTokens[req.Envelope] = append(m.keyTokens[req.Envelope], tok)
		m.keysMu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"token": tok})
	})

	mux.HandleFunc("/api/v1/me", func(w http.ResponseWriter, r *http.Request) {
		if m.noMe {
			http.NotFound(w, r)
			return
		}
		if _, ok := m.revoked.Load(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")); ok {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"user": map[string]any{"email": "user@example.com"}})
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
	err := testLogin(ctx, srv.URL, false, false)
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
	require.Equal(t, clientconfig.IdentityToken, id.Type)
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
	err := testLogin(ctx, srv.URL, false, true)
	require.NoError(t, err)

	require.Equal(t, int32(1), mc.keyBeginHits.Load(), "persistent-key login must register a key")
	require.Equal(t, int32(1), mc.keyCompleteHits.Load(), "key registration must complete")

	cfg, err := clientconfig.LoadConfig()
	require.NoError(t, err)
	id, err := cfg.GetIdentity("cloud")
	require.NoError(t, err)
	require.Equal(t, clientconfig.IdentityKeypair, id.Type)
	require.Empty(t, id.Token)

	_, err = os.Stat(filepath.Join(dir, "clientconfig.d", "key-miren-cli.yaml"))
	require.NoError(t, err, "persistent-key login must write a key file")
}

// TestLoginNoSavePersistentKeyRegisters guards a regression: --no-save with
// --persistent-key printed a freshly generated private key without ever
// registering the public half, so the key it handed the user could not
// authenticate anything.
func TestLoginNoSavePersistentKeyRegisters(t *testing.T) {
	mc := &mockCloud{withRefresh: true}
	srv := mc.server(t)
	defer srv.Close()

	dir := t.TempDir()
	t.Setenv("MIREN_CONFIG", dir)

	ctx := newTestContext()
	err := testLogin(ctx, srv.URL, true, true)
	require.NoError(t, err)

	require.Equal(t, int32(1), mc.keyBeginHits.Load(),
		"a printed key must be registered with the cloud or it cannot authenticate")
	require.Equal(t, int32(1), mc.keyCompleteHits.Load(),
		"registration must actually complete, not just begin")

	// --no-save must still persist nothing.
	_, err = os.Stat(filepath.Join(dir, "clientconfig.d"))
	require.True(t, os.IsNotExist(err), "--no-save must not write any config")
}

// TestLoginNoSaveEphemeralSkipsKeyRegistration verifies the token path stays
// registration-free under --no-save.
func TestLoginNoSaveEphemeralSkipsKeyRegistration(t *testing.T) {
	mc := &mockCloud{withRefresh: true}
	srv := mc.server(t)
	defer srv.Close()

	dir := t.TempDir()
	t.Setenv("MIREN_CONFIG", dir)

	ctx := newTestContext()
	err := testLogin(ctx, srv.URL, true, false)
	require.NoError(t, err)

	require.Zero(t, mc.keyBeginHits.Load(), "ephemeral --no-save must not register a key")
	require.Zero(t, mc.keyCompleteHits.Load(), "ephemeral --no-save must not complete registration")
	_, err = os.Stat(filepath.Join(dir, "clientconfig.d"))
	require.True(t, os.IsNotExist(err), "--no-save must not write any config")
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
	err := testLogin(ctx, srv.URL, false, false)
	require.NoError(t, err)

	require.Equal(t, int32(1), mc.keyBeginHits.Load(), "should fall back to key registration")
	require.Equal(t, int32(1), mc.keyCompleteHits.Load(), "fallback key registration must complete")

	cfg, err := clientconfig.LoadConfig()
	require.NoError(t, err)
	id, err := cfg.GetIdentity("cloud")
	require.NoError(t, err)
	require.Equal(t, clientconfig.IdentityKeypair, id.Type)
}

// testLogin runs a login that waits for approval, as at a terminal, without
// the identity picker a terminal would show.
func testLogin(ctx *Context, cloudURL string, noSave, persistentKey bool) error {
	return login(ctx, loginRequest{
		CloudURL: cloudURL, Identity: "cloud", KeyName: defaultKeyName,
		NoSave: noSave, PersistentKey: persistentKey, Wait: 10 * time.Second,
	})
}
