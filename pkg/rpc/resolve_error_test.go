package rpc

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/quic-go/quic-go"
)

// The failure shapes below were reproduced against a live server before being
// written down. In particular, quic-go returns the *same* IdleTimeoutError for
// a connection that never completed a handshake (server down: ~5s) and one
// that completed and later went quiet (server frozen mid-request: ~30s), which
// is why classification depends on the handshake record rather than the error.
func TestClassifyTransportError(t *testing.T) {
	idle := &quic.IdleTimeoutError{}

	tests := []struct {
		name     string
		err      error
		reached  bool
		wantKind ResolveErrorKind
	}{
		{
			name:     "idle timeout before any handshake means unreachable",
			err:      idle,
			reached:  false,
			wantKind: ResolveUnreachableError,
		},
		{
			name:     "idle timeout after a handshake means it went silent",
			err:      idle,
			reached:  true,
			wantKind: ResolveWentSilentError,
		},
		{
			// http3 wraps the post-handshake case as "http3: parsing frame
			// failed: timeout: no recent network activity". Classification
			// must survive that wrapping, which string matching would not.
			name:     "wrapped idle timeout still classifies",
			err:      fmt.Errorf("http3: parsing frame failed: %w", idle),
			reached:  true,
			wantKind: ResolveWentSilentError,
		},
		{
			name:     "handshake timeout before connecting means unreachable",
			err:      &quic.HandshakeTimeoutError{},
			reached:  false,
			wantKind: ResolveUnreachableError,
		},
		{
			name:     "our own deadline on a healthy connection means no answer",
			err:      context.DeadlineExceeded,
			reached:  true,
			wantKind: ResolveNoAnswerError,
		},
		{
			// A deadline that fires before we ever reached the server is still
			// an unreachable server, not a silent one.
			name:     "our own deadline without a handshake is still unreachable",
			err:      context.DeadlineExceeded,
			reached:  false,
			wantKind: ResolveUnreachableError,
		},
		{
			name:     "non-timeout failures keep the generic kind",
			err:      errors.New("connection reset by peer"),
			reached:  true,
			wantKind: ResolveHTTPError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := classifyTransportError("entities", "localhost:8443", 5*time.Second, tt.err, tt.reached)

			re, ok := errors.AsType[*ResolveError](err)
			if !ok {
				t.Fatalf("classify returned %T, want *ResolveError", err)
			}
			if re.Kind != tt.wantKind {
				t.Fatalf("Kind = %s, want %s", re.Kind, tt.wantKind)
			}
			if !errors.Is(err, tt.err) {
				t.Error("underlying error is no longer reachable via errors.Is")
			}
		})
	}
}

// Every classified failure has to name what was being looked up and where, so
// the message stands on its own. This is the regression the original bug was:
// "error performing http request: timeout: no recent network activity" named
// neither, even though both were in scope at the call site.
func TestClassifiedErrorsCarryContext(t *testing.T) {
	for _, reached := range []bool{true, false} {
		err := classifyTransportError("entities", "localhost:8443", 5*time.Second, &quic.IdleTimeoutError{}, reached)

		re, ok := errors.AsType[*ResolveError](err)
		if !ok {
			t.Fatalf("classify returned %T, want *ResolveError", err)
		}
		if re.Name != "entities" {
			t.Errorf("Name = %q, want %q", re.Name, "entities")
		}
		if re.Remote != "localhost:8443" {
			t.Errorf("Remote = %q, want %q", re.Remote, "localhost:8443")
		}
		if re.Elapsed != 5*time.Second {
			t.Errorf("Elapsed = %s, want 5s", re.Elapsed)
		}
		for _, want := range []string{"entities", "localhost:8443"} {
			if !strings.Contains(re.Error(), want) {
				t.Errorf("message %q does not mention %q", re.Error(), want)
			}
		}
	}
}

// The sentinels are only useful if they distinguish kinds. They used to match
// any *ResolveError, which made errors.Is(err, ErrResolveLookup) true for
// transport failures too.
func TestResolveErrorSentinelsDistinguishKinds(t *testing.T) {
	unreachable := NewResolveUnreachableError("entities", "localhost:8443", time.Second, nil)

	if !errors.Is(unreachable, ErrResolveUnreachable) {
		t.Error("unreachable error does not match its own sentinel")
	}
	if errors.Is(unreachable, ErrResolveLookup) {
		t.Error("unreachable error wrongly matches the lookup sentinel")
	}
}
