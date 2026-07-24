package commands

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"miren.dev/runtime/clientconfig"
)

// writeTokenIdentity writes a token identity leaf into a MIREN_CONFIG dir and
// returns the access token it stored. The access token is a genuinely fresh JWT
// so logout's revoke path can use it directly instead of trying to refresh.
func writeTokenIdentity(t *testing.T, dir, issuer string) string {
	t.Helper()
	accessToken := freshLoginJWT(t)
	cd := clientconfig.ConfigData{
		Identities: map[string]*clientconfig.IdentityConfig{
			"cloud": {
				Type:         "token",
				Issuer:       issuer,
				Token:        accessToken,
				RefreshToken: "refresh-jwt",
			},
		},
	}
	data, err := yaml.Marshal(&cd)
	require.NoError(t, err)
	leafDir := filepath.Join(dir, "clientconfig.d")
	require.NoError(t, os.MkdirAll(leafDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(leafDir, "identity-cloud.yaml"), data, 0600))
	return accessToken
}

func runLogout(t *testing.T) error {
	t.Helper()
	return Logout(newTestContext(), struct {
		ConfigCentric
		IdentityName string `short:"i" long:"identity" description:"Name of the identity to remove"`
	}{IdentityName: "cloud"})
}

// revokeCapture records what the fake revoke endpoint saw. The handler runs on
// the server's goroutine while the test body reads, so it needs a mutex.
type revokeCapture struct {
	mu      sync.Mutex
	auth    string
	refresh string
	reason  string
}

func (c *revokeCapture) record(auth, refresh, reason string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.auth, c.refresh, c.reason = auth, refresh, reason
}

func (c *revokeCapture) get() (auth, refresh, reason string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.auth, c.refresh, c.reason
}

func TestLogoutRevokesRefreshToken(t *testing.T) {
	var revokeHits atomic.Int32
	var capture revokeCapture

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth/revoke/refresh" {
			revokeHits.Add(1)
			var body struct {
				RefreshToken string `json:"refresh_token"`
				Reason       string `json:"reason"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			capture.record(r.Header.Get("Authorization"), body.RefreshToken, body.Reason)
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	dir := t.TempDir()
	t.Setenv("MIREN_CONFIG", dir)
	accessToken := writeTokenIdentity(t, dir, srv.URL)

	require.NoError(t, runLogout(t))

	require.Equal(t, int32(1), revokeHits.Load(), "logout should revoke the refresh token")
	auth, refresh, reason := capture.get()
	require.Equal(t, "Bearer "+accessToken, auth, "revoke must authenticate with a live access token")
	require.Equal(t, "refresh-jwt", refresh)
	require.Equal(t, "logout", reason)

	// The identity file must be gone regardless.
	_, err := os.Stat(filepath.Join(dir, "clientconfig.d", "identity-cloud.yaml"))
	require.True(t, os.IsNotExist(err))
}

func TestLogoutSucceedsWhenRevokeFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError) // revoke fails
	}))
	defer srv.Close()

	dir := t.TempDir()
	t.Setenv("MIREN_CONFIG", dir)
	writeTokenIdentity(t, dir, srv.URL)

	// A failed revoke must not block logout.
	require.NoError(t, runLogout(t))
	_, err := os.Stat(filepath.Join(dir, "clientconfig.d", "identity-cloud.yaml"))
	require.True(t, os.IsNotExist(err), "identity file should still be removed")
}
