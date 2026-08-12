package cloudauth

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

// Every request this client makes carries the service account bearer token, and
// net/http preserves Authorization across a same-host redirect, so a redirect
// off TLS would put the token on the wire in the clear.
func TestRefuseDowngrade(t *testing.T) {
	tests := []struct {
		name    string
		from    string
		to      string
		wantErr bool
	}{
		{name: "https to http is refused", from: "https://api.example/a", to: "http://api.example/b", wantErr: true},
		{name: "https to https is allowed", from: "https://api.example/a", to: "https://api.example/b"},
		{name: "http to http is allowed", from: "http://miren.host:3001/a", to: "http://miren.host:3001/b"},
		{name: "http to https is allowed", from: "http://api.example/a", to: "https://api.example/b"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			from, err := url.Parse(tt.from)
			require.NoError(t, err)
			to, err := url.Parse(tt.to)
			require.NoError(t, err)

			err = refuseDowngrade(&http.Request{URL: to}, []*http.Request{{URL: from}})
			if tt.wantErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), "plaintext")
				return
			}
			require.NoError(t, err)
		})
	}
}

// End to end through the client: a cloud that answers the handshake with a
// downgrade redirect must not get the token.
func TestAuthClientRefusesDowngradeRedirect(t *testing.T) {
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("credentialed request reached the plaintext server at %s", r.URL.Path)
	}))
	defer plain.Close()

	tls := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, plain.URL+r.URL.Path, http.StatusTemporaryRedirect)
	}))
	defer tls.Close()

	keyPair, err := GenerateKeyPair()
	require.NoError(t, err)
	client, err := NewAuthClient(tls.URL, keyPair)
	require.NoError(t, err)
	client.httpClient.Transport = tls.Client().Transport

	_, err = client.Authenticate(t.Context())
	require.Error(t, err)
	require.Contains(t, err.Error(), "plaintext")
}
