package rpc

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
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
