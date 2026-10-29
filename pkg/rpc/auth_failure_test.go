package rpc

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/quic-go/quic-go/http3"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/pkg/cond"
)

type disclosableTestError struct{ msg string }

func (e *disclosableTestError) Error() string         { return e.msg }
func (e *disclosableTestError) AuthErrorCode() string { return AuthErrorOIDCBindingMismatch }

func TestWriteAuthFailure_DisclosesOptedInReason(t *testing.T) {
	w := httptest.NewRecorder()
	writeAuthFailure(w, &disclosableTestError{msg: "OIDC token did not match any CI binding (subject=repo:acme/app)"})

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
	if got := w.Header().Get("rpc-status"); got != AuthErrorOIDCBindingMismatch {
		t.Errorf("rpc-status = %q, want %q", got, AuthErrorOIDCBindingMismatch)
	}
	if got := w.Header().Get("rpc-error"); got == "" {
		t.Error("rpc-error should carry the reason the caller was rejected")
	}
}

// Anything that didn't opt in stays a bare 401. An authenticator has to say
// explicitly that its failure is safe to hand back.
func TestWriteAuthFailure_OrdinaryErrorStaysOpaque(t *testing.T) {
	w := httptest.NewRecorder()
	writeAuthFailure(w, fmt.Errorf("signing key 4f2a not found in keyring"))

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
	if got := w.Header().Get("rpc-status"); got != "" {
		t.Errorf("rpc-status = %q, want empty", got)
	}
	if got := w.Header().Get("rpc-error"); got != "" {
		t.Errorf("rpc-error = %q, want empty; internal auth detail must not leak", got)
	}
}

func TestNewResolveStatusError_CarriesServerDetail(t *testing.T) {
	err := NewResolveStatusErrorWithReason("entities", "localhost:8443", 401, AuthErrorOIDCBindingMismatch, "OIDC token did not match any CI binding")

	resolveErr, ok := errors.AsType[*ResolveError](err)
	if !ok {
		t.Fatalf("expected a *ResolveError, got %T", err)
	}
	if resolveErr.Code != AuthErrorOIDCBindingMismatch {
		t.Errorf("Code = %q, want %q", resolveErr.Code, AuthErrorOIDCBindingMismatch)
	}
	if resolveErr.Detail != "OIDC token did not match any CI binding" {
		t.Errorf("Detail = %q", resolveErr.Detail)
	}
	if resolveErr.StatusCode != 401 {
		t.Errorf("StatusCode = %d, want 401", resolveErr.StatusCode)
	}
}

func TestHTTPCallPreservesTimestampDenialReason(t *testing.T) {
	ss, err := NewState(t.Context(), WithSkipVerify)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ss.Close() })
	ss.Server().ExposeValue("test", NewInterface([]Method{{Name: "test", InterfaceName: "test"}}, nil))

	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	server := &http3.Server{
		TLSConfig: testServerTLSConfig(t),
		Handler: http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			if strings.HasPrefix(req.URL.Path, "/_rpc/call/") {
				req.Header.Set("rpc-timestamp", time.Now().Add(-time.Hour).Format(time.RFC3339Nano))
			}
			ss.Server().ServeHTTP(w, req)
		}),
	}
	go func() { _ = server.Serve(conn) }()
	t.Cleanup(func() { _ = server.Close() })

	cs, err := NewState(t.Context(), WithSkipVerify)
	require.NoError(t, err)
	t.Cleanup(func() { _ = cs.Close() })
	client, err := cs.Connect(conn.LocalAddr().String(), "test")
	require.NoError(t, err, "lookup succeeds without checking the timestamp")
	err = client.Call(t.Context(), "test", nil, nil)
	require.Equal(t, cond.ErrRemote{Category: "permission", Code: "forbidden", Message: "timestamp too old"}, err)
}
