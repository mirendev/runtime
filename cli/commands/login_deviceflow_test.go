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

// deviceTokenServerRaw serves a scripted sequence of /api/v1/device/token
// responses with explicit status codes and raw bodies, so pollForToken can be
// exercised against the kinds of responses intermediaries (CDN/WAF/reverse
// proxy/maintenance pages) emit during transient outages: non-200 status with
// non-JSON bodies.
func deviceTokenServerRaw(t *testing.T, responses []rawTokenResponse) *httptest.Server {
	t.Helper()
	var idx atomic.Int32
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/v1/device/token", r.URL.Path)
		i := int(idx.Add(1)) - 1
		if i >= len(responses) {
			i = len(responses) - 1 // repeat the last response
		}
		resp := responses[i]
		if resp.contentType != "" {
			w.Header().Set("Content-Type", resp.contentType)
		}
		w.WriteHeader(resp.status)
		_, _ = w.Write([]byte(resp.body))
	}))
}

// rawTokenResponse is a single scripted response for deviceTokenServerRaw.
type rawTokenResponse struct {
	status      int
	contentType string
	body        string
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

	// Non-200 with a non-JSON body (e.g. a 502 HTML page from an intermediary
	// reverse proxy / CDN / WAF / maintenance page) must be treated as a
	// transient failure within the polling window, not as a hard parse error.
	t.Run("non-200 HTML then authorized continues polling", func(t *testing.T) {
		srv := deviceTokenServerRaw(t, []rawTokenResponse{
			{status: http.StatusBadGateway, contentType: "text/html; charset=utf-8", body: "<html><head><title>502 Bad Gateway</title></head><body>nginx</body></html>"},
			{status: http.StatusOK, contentType: "application/json", body: `{"status":"authorized","access_token":"access-jwt"}`},
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
		require.GreaterOrEqual(t, pendings.Load(), int32(1))
	})

	// 200 + non-JSON is a genuine origin contract violation (the origin
	// deliberately returned 200 but a malformed body); it must stay a hard
	// error surfaced immediately, not silently retried for the rest of the
	// window. Guards against the fix over-correcting by swallowing all
	// non-JSON responses.
	t.Run("200 with non-JSON body aborts", func(t *testing.T) {
		srv := deviceTokenServerRaw(t, []rawTokenResponse{
			{status: http.StatusOK, contentType: "text/plain; charset=utf-8", body: "Internal Server Error (not JSON)"},
		})
		defer srv.Close()

		_, err := pollForToken(context.Background(), srv.URL, "dev-code", interval, 500*time.Millisecond, func(string) {})
		require.Error(t, err)
		require.ErrorContains(t, err, "failed to parse response")
	})
}
