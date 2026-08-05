package rpc

import (
	"fmt"
	"net/http/httptest"
	"testing"
)

func newBearerClient(t *testing.T, opts ...StateOption) *NetworkClient {
	t.Helper()

	s, err := NewState(t.Context(), opts...)
	if err != nil {
		t.Fatalf("NewState: %v", err)
	}
	return &NetworkClient{State: s}
}

// The bearer token func is consulted per request, not captured once. Workload
// identity tokens expire hourly and are rewritten in place by the sandbox
// controller, so a client that cached the first value would start failing after
// an hour -- which is exactly the long-running workload this exists for.
func TestAddBearerToken_FuncIsReReadPerRequest(t *testing.T) {
	token := "first-token"
	c := newBearerClient(t, WithBearerTokenFunc(func() (string, error) {
		return token, nil
	}))

	req := httptest.NewRequest("POST", "/_rpc/call/test/method", nil)
	c.addBearerToken(req)
	if got := req.Header.Get("Authorization"); got != "Bearer first-token" {
		t.Fatalf("Authorization = %q, want %q", got, "Bearer first-token")
	}

	token = "refreshed-token"

	req = httptest.NewRequest("POST", "/_rpc/call/test/method", nil)
	c.addBearerToken(req)
	if got := req.Header.Get("Authorization"); got != "Bearer refreshed-token" {
		t.Errorf("Authorization = %q, want the refreshed token", got)
	}
}

func TestAddBearerToken_FuncTakesPrecedence(t *testing.T) {
	c := newBearerClient(t,
		WithBearerToken("static"),
		WithBearerTokenFunc(func() (string, error) { return "dynamic", nil }),
	)

	req := httptest.NewRequest("POST", "/_rpc/call/test/method", nil)
	c.addBearerToken(req)
	if got := req.Header.Get("Authorization"); got != "Bearer dynamic" {
		t.Errorf("Authorization = %q, want the func's token", got)
	}
}

// A token that cannot be read right now sends the request unauthenticated: the
// server rejects it and the caller's retry sees a token that has since been
// refreshed. The sandbox controller rewrites the token file in place, so a read
// can briefly observe a torn write.
func TestAddBearerToken_FuncFailureSendsUnauthenticated(t *testing.T) {
	tests := map[string]func() (string, error){
		"error":       func() (string, error) { return "", fmt.Errorf("token file missing") },
		"empty token": func() (string, error) { return "", nil },
	}

	for name, fn := range tests {
		t.Run(name, func(t *testing.T) {
			c := newBearerClient(t, WithBearerTokenFunc(fn))

			req := httptest.NewRequest("POST", "/_rpc/call/test/method", nil)
			c.addBearerToken(req)
			if got := req.Header.Get("Authorization"); got != "" {
				t.Errorf("Authorization = %q, want no header", got)
			}
		})
	}
}

func TestWithTLSServerName(t *testing.T) {
	s, err := NewState(t.Context(), WithTLSServerName("api.miren"))
	if err != nil {
		t.Fatalf("NewState: %v", err)
	}
	if got := s.clientTlsCfg.ServerName; got != "api.miren" {
		t.Errorf("ServerName = %q, want %q", got, "api.miren")
	}
	if s.clientTlsCfg.InsecureSkipVerify {
		t.Error("overriding the server name must not disable verification")
	}
}
