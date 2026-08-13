package cloudauth

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newJWKSTestServer stands up a cloud stub that answers the service account
// handshake and hands the publish request to publishHandler.
func newJWKSTestServer(t *testing.T, publishHandler http.HandlerFunc) *httptest.Server {
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

		case "/api/v1/self/cluster/jwks":
			publishHandler(w, r)

		default:
			t.Errorf("Unexpected request to %s", r.URL.Path)
		}
	}))
}

func newTestAuthClient(t *testing.T, serverURL string) *AuthClient {
	t.Helper()

	keyPair, err := GenerateKeyPair()
	require.NoError(t, err)

	authClient, err := NewAuthClient(serverURL, keyPair)
	require.NoError(t, err)

	return authClient
}

func TestPublishJWKS(t *testing.T) {
	const jwks = `{"keys":[{"kty":"OKP","crv":"Ed25519","x":"abc","kid":"kid-1","use":"sig","alg":"EdDSA"}]}`

	var method, authToken, contentType, body string

	ts := newJWKSTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		method = r.Method
		authToken = r.Header.Get("Authorization")
		contentType = r.Header.Get("Content-Type")

		raw, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		body = string(raw)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(PublishJWKSResult{
			Issuer:   "https://api.miren.cloud/identity/cluster-abc",
			JWKSURI:  "https://api.miren.cloud/identity/cluster-abc/.well-known/jwks.json",
			KeyCount: 1,
		})
	})
	defer ts.Close()

	result, err := newTestAuthClient(t, ts.URL).PublishJWKS(context.Background(), []byte(jwks))
	require.NoError(t, err)

	assert.Equal(t, http.MethodPut, method)
	assert.Equal(t, "Bearer test-jwt-token", authToken)
	assert.Equal(t, "application/jwk-set+json", contentType)
	assert.JSONEq(t, jwks, body)

	assert.Equal(t, "https://api.miren.cloud/identity/cluster-abc", result.Issuer)
	assert.Equal(t, 1, result.KeyCount)
}

// Cloud with no anchor configured is not a transient failure and not something
// the cluster can fix, so it gets an error callers can recognize and stop on.
func TestPublishJWKS_DiscoveryUnavailable(t *testing.T) {
	ts := newJWKSTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{"error": "Workload identity discovery is not configured"})
	})
	defer ts.Close()

	_, err := newTestAuthClient(t, ts.URL).PublishJWKS(context.Background(), []byte(`{"keys":[]}`))
	assert.ErrorIs(t, err, ErrDiscoveryUnavailable)
}

// Cloud names the reason a key set was rejected; that reason has to survive to
// the operator, because the alternative is an opaque failure to federate.
func TestPublishJWKS_RejectionSurfacesReason(t *testing.T) {
	ts := newJWKSTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "invalid key set: key 0 carries private key material ([d])",
		})
	})
	defer ts.Close()

	_, err := newTestAuthClient(t, ts.URL).PublishJWKS(context.Background(), []byte(`{"keys":[]}`))
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrDiscoveryUnavailable)
	assert.Contains(t, err.Error(), "private key material")
}

// An accepted publish with no anchor in the response would leave the cluster
// thinking it had one. Treat it as a failure rather than adopting an empty iss.
func TestPublishJWKS_RejectsResponseWithoutIssuer(t *testing.T) {
	ts := newJWKSTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"key_count": 1})
	})
	defer ts.Close()

	_, err := newTestAuthClient(t, ts.URL).PublishJWKS(context.Background(), []byte(`{"keys":[]}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "assigned no issuer")
}
