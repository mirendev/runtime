package commands

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"miren.dev/runtime/clientconfig"
)

// writeTokenIdentity writes a token identity leaf into a MIREN_CONFIG dir.
func writeTokenIdentity(t *testing.T, dir, issuer string) {
	t.Helper()
	cd := clientconfig.ConfigData{
		Identities: map[string]*clientconfig.IdentityConfig{
			"cloud": {
				Type:         "token",
				Issuer:       issuer,
				Token:        "access-jwt",
				RefreshToken: "refresh-jwt",
			},
		},
	}
	data, err := yaml.Marshal(&cd)
	require.NoError(t, err)
	leafDir := filepath.Join(dir, "clientconfig.d")
	require.NoError(t, os.MkdirAll(leafDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(leafDir, "identity-cloud.yaml"), data, 0600))
}

func runLogout(t *testing.T) error {
	t.Helper()
	return Logout(newTestContext(), struct {
		ConfigCentric
		IdentityName string `short:"i" long:"identity" description:"Name of the identity to remove"`
	}{IdentityName: "cloud"})
}

func TestLogoutRevokesRefreshToken(t *testing.T) {
	var revokeHits atomic.Int32
	var gotAuth, gotRefresh string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth/revoke/refresh" {
			revokeHits.Add(1)
			gotAuth = r.Header.Get("Authorization")
			var body struct {
				RefreshToken string `json:"refresh_token"`
				Reason       string `json:"reason"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			gotRefresh = body.RefreshToken
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	dir := t.TempDir()
	t.Setenv("MIREN_CONFIG", dir)
	writeTokenIdentity(t, dir, srv.URL)

	require.NoError(t, runLogout(t))

	require.Equal(t, int32(1), revokeHits.Load(), "logout should revoke the refresh token")
	require.Equal(t, "Bearer access-jwt", gotAuth)
	require.Equal(t, "refresh-jwt", gotRefresh)

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
