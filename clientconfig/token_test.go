package clientconfig

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
)

// makeToken builds an unsigned JWT with the given claims. tokenFresh parses
// tokens unverified, so the signature is irrelevant to these tests.
func makeToken(t *testing.T, claims jwt.MapClaims) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	s, err := tok.SignedString([]byte("test-secret"))
	require.NoError(t, err)
	return s
}

func TestTokenFresh(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name  string
		token string
		want  bool
	}{
		{
			name:  "valid well beyond buffer",
			token: makeToken(t, jwt.MapClaims{"exp": float64(now.Add(time.Hour).Unix())}),
			want:  true,
		},
		{
			name:  "expiring within buffer",
			token: makeToken(t, jwt.MapClaims{"exp": float64(now.Add(4 * time.Minute).Unix())}),
			want:  false,
		},
		{
			name:  "already expired",
			token: makeToken(t, jwt.MapClaims{"exp": float64(now.Add(-time.Minute).Unix())}),
			want:  false,
		},
		{
			name:  "no exp claim",
			token: makeToken(t, jwt.MapClaims{"sub": "user-1"}),
			want:  false,
		},
		{
			name:  "unparseable garbage",
			token: "not-a-jwt",
			want:  false,
		},
		{
			name:  "empty string",
			token: "",
			want:  false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, tokenFresh(tc.token, tokenExpiryBuffer))
		})
	}
}

// TestTokenFreshBufferBoundary verifies the buffer is honored: a token that
// outlives exp-buffer is fresh, one that doesn't is stale.
func TestTokenFreshBufferBoundary(t *testing.T) {
	now := time.Now()

	// Expires in 10 minutes; with a 5-minute buffer it is still fresh.
	fresh := makeToken(t, jwt.MapClaims{"exp": float64(now.Add(10 * time.Minute).Unix())})
	require.True(t, tokenFresh(fresh, tokenExpiryBuffer))

	// The same token is stale under a 15-minute buffer.
	require.False(t, tokenFresh(fresh, 15*time.Minute))
}

func TestNormalizeIssuerURL(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"miren.cloud", "https://miren.cloud"},
		{"https://miren.cloud", "https://miren.cloud"},
		{"https://miren.cloud/", "https://miren.cloud"},
		{"http://miren.host:3001", "http://miren.host:3001"},
		{"localhost:3001", "http://localhost:3001"},
		{"127.0.0.1:3001", "http://127.0.0.1:3001"},
	}
	for _, tc := range tests {
		require.Equal(t, tc.want, NormalizeIssuerURL(tc.in))
	}
}

func TestRefreshTokenPair(t *testing.T) {
	t.Run("200 rotates and returns new pair", func(t *testing.T) {
		var gotPath string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.Path
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"access_token":"new-access","refresh_token":"new-refresh","token_type":"Bearer","expires_in":3600}`))
		}))
		defer srv.Close()

		pair, err := refreshTokenPair(context.Background(), srv.URL, "old-refresh")
		require.NoError(t, err)
		require.Equal(t, "/auth/refresh", gotPath)
		require.Equal(t, "new-access", pair.AccessToken)
		require.Equal(t, "new-refresh", pair.RefreshToken)
		require.NotEqual(t, "old-refresh", pair.RefreshToken, "server must rotate the refresh token")
		require.Equal(t, 3600, pair.ExpiresIn)
	})

	t.Run("401 means login required", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "Refresh token has been revoked", http.StatusUnauthorized)
		}))
		defer srv.Close()

		_, err := refreshTokenPair(context.Background(), srv.URL, "spent-refresh")
		require.ErrorIs(t, err, ErrLoginRequired)
	})

	t.Run("500 is transient, NOT login required", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "Token validation error", http.StatusInternalServerError)
		}))
		defer srv.Close()

		_, err := refreshTokenPair(context.Background(), srv.URL, "some-refresh")
		require.Error(t, err)
		require.NotErrorIs(t, err, ErrLoginRequired, "a 5xx must not force a logout")
	})

	t.Run("200 with empty access token errors", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"access_token":"","refresh_token":"r","token_type":"Bearer","expires_in":3600}`))
		}))
		defer srv.Close()

		_, err := refreshTokenPair(context.Background(), srv.URL, "some-refresh")
		require.Error(t, err)
		require.NotErrorIs(t, err, ErrLoginRequired)
	})

	t.Run("empty refresh token short-circuits to login required", func(t *testing.T) {
		_, err := refreshTokenPair(context.Background(), "https://miren.cloud", "")
		require.ErrorIs(t, err, ErrLoginRequired)
	})

	t.Run("network failure is transient", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
		url := srv.URL
		srv.Close() // nothing listening now

		_, err := refreshTokenPair(context.Background(), url, "some-refresh")
		require.Error(t, err)
		require.False(t, errors.Is(err, ErrLoginRequired), "a network failure must not force a logout")
	})
}

func TestRevokeRefreshToken(t *testing.T) {
	t.Run("posts bearer and refresh token", func(t *testing.T) {
		var path, auth, gotRefresh, gotReason string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			path = r.URL.Path
			auth = r.Header.Get("Authorization")
			var body struct {
				RefreshToken string `json:"refresh_token"`
				Reason       string `json:"reason"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			gotRefresh, gotReason = body.RefreshToken, body.Reason
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()

		err := RevokeRefreshToken(context.Background(), srv.URL, "access-x", "refresh-y")
		require.NoError(t, err)
		require.Equal(t, "/auth/revoke/refresh", path)
		require.Equal(t, "Bearer access-x", auth)
		require.Equal(t, "refresh-y", gotRefresh)
		require.Equal(t, "logout", gotReason)
	})

	t.Run("no refresh token is a no-op", func(t *testing.T) {
		require.NoError(t, RevokeRefreshToken(context.Background(), "https://x", "access", ""))
	})

	t.Run("missing access token reports failure rather than silent success", func(t *testing.T) {
		// Returning nil here would let the caller claim it revoked a token when
		// no request was ever made.
		err := RevokeRefreshToken(context.Background(), "https://x", "", "refresh-y")
		require.Error(t, err)
		require.Contains(t, err.Error(), "no access token")
	})

	t.Run("non-2xx is an error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer srv.Close()
		require.Error(t, RevokeRefreshToken(context.Background(), srv.URL, "a", "b"))
	})
}
