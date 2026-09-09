package rpc

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
)

type drainAuthenticator struct{ identity *Identity }

func (a drainAuthenticator) Authenticate(context.Context, *Credentials) (*Identity, error) {
	return a.identity, nil
}

func TestHTTPDrainRequiresIdentityWhenAuthenticationIsEnabled(t *testing.T) {
	for _, tc := range []struct {
		name   string
		auth   Authenticator
		status int
	}{
		{"no credentials", drainAuthenticator{}, http.StatusUnauthorized},
		{"anonymous identity", drainAuthenticator{&Identity{Method: AuthMethodAnonymous}}, http.StatusUnauthorized},
		{"authenticated identity", drainAuthenticator{&Identity{Method: AuthMethodJWT, Subject: "caller"}}, http.StatusOK},
		{"authentication disabled", &NoOpAuthenticator{}, http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &Server{state: &State{StateCommon: &StateCommon{top: t.Context(), log: slog.Default(), authenticator: tc.auth}}}
			s.setupMux()
			ctx, cancel := context.WithCancel(t.Context())
			cancel() // Let an accepted watch return after writing its headers.
			req := httptest.NewRequestWithContext(ctx, http.MethodGet, drainPath, nil)
			w := httptest.NewRecorder()
			s.ServeHTTP(w, req)
			require.Equal(t, tc.status, w.Code)
			if tc.status == http.StatusUnauthorized {
				require.Empty(t, w.Header().Get(drainVersionHeader))
			}
		})
	}
}

func TestHTTPDrainWatchExpiresWithoutServerShutdown(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := &Server{state: &State{StateCommon: &StateCommon{top: t.Context(), authenticator: &NoOpAuthenticator{}}}}
		w := httptest.NewRecorder()
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, drainPath, nil)
		start := time.Now()
		s.handleDrain(w, req)
		require.Equal(t, drainWatchMaxAge, time.Since(start))
		require.NoError(t, s.state.top.Err())
		require.Equal(t, http.StatusOK, w.Code)
		require.Equal(t, "1", w.Header().Get(drainVersionHeader))
		require.Empty(t, w.Body.String())
	})
}
