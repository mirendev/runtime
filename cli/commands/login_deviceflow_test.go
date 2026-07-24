package commands

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// deviceTokenServer serves a scripted sequence of /api/v1/device/token
// responses so pollForToken's state machine can be exercised deterministically.
func deviceTokenServer(t *testing.T, responses []map[string]any) *httptest.Server {
	t.Helper()
	var idx atomic.Int32
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/v1/device/token", r.URL.Path)
		i := int(idx.Add(1)) - 1
		if i >= len(responses) {
			i = len(responses) - 1 // repeat the last response
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(responses[i])
	}))
}

func TestPollForToken(t *testing.T) {
	interval := 5 * time.Millisecond
	timeout := 2 * time.Second

	t.Run("pending then authorized returns access and refresh tokens", func(t *testing.T) {
		srv := deviceTokenServer(t, []map[string]any{
			{"status": "pending"},
			{"status": "pending"},
			{
				"status":        "authorized",
				"access_token":  "access-jwt",
				"refresh_token": "refresh-jwt",
				"token_type":    "Bearer",
				"expires_in":    3600,
			},
		})
		defer srv.Close()

		var pendings atomic.Int32
		tokens, err := pollForToken(context.Background(), srv.URL, "dev-code", interval, timeout, func(status string) {
			if status == "pending" {
				pendings.Add(1)
			}
		})
		require.NoError(t, err)
		require.Equal(t, "access-jwt", tokens.AccessToken)
		require.Equal(t, "refresh-jwt", tokens.RefreshToken)
		require.Equal(t, 3600, tokens.ExpiresIn)
		require.GreaterOrEqual(t, pendings.Load(), int32(2))
	})

	t.Run("authorized without refresh token (older cloud)", func(t *testing.T) {
		srv := deviceTokenServer(t, []map[string]any{
			{"status": "authorized", "access_token": "access-only", "token_type": "Bearer", "expires_in": 3600},
		})
		defer srv.Close()

		tokens, err := pollForToken(context.Background(), srv.URL, "dev-code", interval, timeout, func(string) {})
		require.NoError(t, err)
		require.Equal(t, "access-only", tokens.AccessToken)
		require.Empty(t, tokens.RefreshToken)
	})

	t.Run("denied", func(t *testing.T) {
		srv := deviceTokenServer(t, []map[string]any{{"status": "denied"}})
		defer srv.Close()

		_, err := pollForToken(context.Background(), srv.URL, "dev-code", interval, timeout, func(string) {})
		require.ErrorContains(t, err, "denied")
	})

	t.Run("expired", func(t *testing.T) {
		srv := deviceTokenServer(t, []map[string]any{{"status": "expired"}})
		defer srv.Close()

		_, err := pollForToken(context.Background(), srv.URL, "dev-code", interval, timeout, func(string) {})
		require.ErrorContains(t, err, "expired")
	})

	t.Run("authorized with empty access token errors", func(t *testing.T) {
		srv := deviceTokenServer(t, []map[string]any{{"status": "authorized", "access_token": ""}})
		defer srv.Close()

		_, err := pollForToken(context.Background(), srv.URL, "dev-code", interval, timeout, func(string) {})
		require.Error(t, err)
	})

	t.Run("slow_down then authorized", func(t *testing.T) {
		srv := deviceTokenServer(t, []map[string]any{
			{"status": "error", "error": "slow_down"},
			{"status": "authorized", "access_token": "access-jwt", "refresh_token": "refresh-jwt"},
		})
		defer srv.Close()

		tokens, err := pollForToken(context.Background(), srv.URL, "dev-code", interval, timeout, func(string) {})
		require.NoError(t, err)
		require.Equal(t, "access-jwt", tokens.AccessToken)
	})

	t.Run("unknown error status is fatal", func(t *testing.T) {
		srv := deviceTokenServer(t, []map[string]any{
			{"status": "error", "error": "boom", "error_description": "kaboom"},
		})
		defer srv.Close()

		_, err := pollForToken(context.Background(), srv.URL, "dev-code", interval, timeout, func(string) {})
		require.ErrorContains(t, err, "boom")
	})

	t.Run("times out while pending", func(t *testing.T) {
		srv := deviceTokenServer(t, []map[string]any{{"status": "pending"}})
		defer srv.Close()

		_, err := pollForToken(context.Background(), srv.URL, "dev-code", interval, 40*time.Millisecond, func(string) {})
		require.ErrorContains(t, err, "timed out")
	})
}
