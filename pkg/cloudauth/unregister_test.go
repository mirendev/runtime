package cloudauth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newUnregisterTestServer stands up a cloud stub that answers the service
// account handshake and hands the unregister request to unregisterHandler.
func newUnregisterTestServer(t *testing.T, unregisterHandler http.HandlerFunc) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/auth/service-account/begin":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(BeginAuthResponse{
				Envelope:  "test-envelope",
				Challenge: "dGVzdC1jaGFsbGVuZ2U=",
			})

		case "/auth/service-account/complete":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(CompleteAuthResponse{
				Token:     "test-jwt-token",
				ExpiresIn: 3600,
			})

		case "/api/v1/self/cluster":
			unregisterHandler(w, r)

		default:
			t.Errorf("Unexpected request to %s", r.URL.Path)
		}
	}))
}

func TestUnregisterSelf(t *testing.T) {
	var method, authToken string

	ts := newUnregisterTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		method = r.Method
		authToken = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusNoContent)
	})
	defer ts.Close()

	keyPair, err := GenerateKeyPair()
	require.NoError(t, err)

	authClient, err := NewAuthClient(ts.URL, keyPair)
	require.NoError(t, err)

	require.NoError(t, authClient.UnregisterSelf(context.Background()))

	assert.Equal(t, http.MethodDelete, method)
	assert.Equal(t, "Bearer test-jwt-token", authToken)
}

// A cluster that was already removed reports a distinct error, so unregister
// can finish clearing local state instead of failing a retry.
func TestUnregisterSelf_AlreadyGone(t *testing.T) {
	ts := newUnregisterTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	defer ts.Close()

	keyPair, err := GenerateKeyPair()
	require.NoError(t, err)

	authClient, err := NewAuthClient(ts.URL, keyPair)
	require.NoError(t, err)

	err = authClient.UnregisterSelf(context.Background())
	assert.ErrorIs(t, err, ErrClusterNotRegistered)
}

func TestUnregisterSelf_ServerError(t *testing.T) {
	ts := newUnregisterTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "database unavailable"})
	})
	defer ts.Close()

	keyPair, err := GenerateKeyPair()
	require.NoError(t, err)

	authClient, err := NewAuthClient(ts.URL, keyPair)
	require.NoError(t, err)

	err = authClient.UnregisterSelf(context.Background())
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrClusterNotRegistered)
	assert.Contains(t, err.Error(), "database unavailable")
}
