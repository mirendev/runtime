package oidcauth

import (
	"context"
	"crypto/tls"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/pkg/entity/testutils"
	"miren.dev/runtime/pkg/rpc"
)

func TestPeekIssuer(t *testing.T) {
	tests := []struct {
		name    string
		token   string
		want    string
		wantErr bool
	}{
		{
			name:    "not a JWT",
			token:   "not-a-jwt",
			wantErr: true,
		},
		{
			name:    "empty string",
			token:   "",
			wantErr: true,
		},
		{
			name:    "two parts only",
			token:   "header.payload",
			wantErr: true,
		},
		{
			name:    "invalid base64 payload",
			token:   "aaa.!!!invalid!!!.ccc",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := peekIssuer(tt.token)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPeekIssuer_ValidJWT(t *testing.T) {
	ts := newTestOIDCServer(t)
	defer ts.Close()

	token := ts.SignToken(jwt.MapClaims{
		"iss": "https://token.actions.githubusercontent.com",
		"sub": "repo:acme/app:ref:refs/heads/main",
		"aud": "test",
		"exp": time.Now().Add(10 * time.Minute).Unix(),
	})

	got, err := peekIssuer(token)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "https://token.actions.githubusercontent.com" {
		t.Errorf("got %q, want GitHub issuer", got)
	}
}

func TestResolveAppName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"my-app", "my-app"},
		{"app/my-app", "my-app"},
		{"some/deep/path/my-app", "my-app"},
		{"", ""},
	}
	for _, tt := range tests {
		got := resolveAppName(tt.input)
		if got != tt.want {
			t.Errorf("resolveAppName(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestOIDCAuthenticator_NoEAC(t *testing.T) {
	auth := NewOIDCAuthenticator(testutils.TestLogger(t))

	ts := newTestOIDCServer(t)
	defer ts.Close()

	token := ts.SignToken(jwt.MapClaims{
		"iss": ts.URL(),
		"sub": "repo:acme/app:ref:refs/heads/main",
		"aud": "test-host",
		"exp": time.Now().Add(10 * time.Minute).Unix(),
	})

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	identity, err := auth.Authenticate(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if identity != nil {
		t.Error("expected nil identity when EAC not set")
	}
}

func TestOIDCAuthenticator_NoBearerToken(t *testing.T) {
	auth := NewOIDCAuthenticator(testutils.TestLogger(t))

	req := httptest.NewRequest("GET", "/", nil)
	identity, err := auth.Authenticate(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if identity != nil {
		t.Error("expected nil identity for request without bearer token")
	}
}

func TestOIDCAuthenticator_NonJWTBearer(t *testing.T) {
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	auth := NewOIDCAuthenticator(testutils.TestLogger(t))
	auth.SetEAC(inmem.EAC)

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer not-a-jwt-token")

	identity, err := auth.Authenticate(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if identity != nil {
		t.Error("expected nil identity for non-JWT bearer token")
	}
}

func TestOIDCAuthenticator_NoMatchingBindings(t *testing.T) {
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	auth := NewOIDCAuthenticator(testutils.TestLogger(t))
	auth.SetEAC(inmem.EAC)

	ts := newTestOIDCServer(t)
	defer ts.Close()

	token := ts.SignToken(jwt.MapClaims{
		"iss": ts.URL(),
		"sub": "repo:acme/app:ref:refs/heads/main",
		"aud": "test-host",
		"exp": time.Now().Add(10 * time.Minute).Unix(),
	})

	req := httptest.NewRequest("GET", "https://test-host/", nil)
	req.Host = "test-host"
	req.Header.Set("Authorization", "Bearer "+token)

	identity, err := auth.Authenticate(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if identity != nil {
		t.Error("expected nil identity when no bindings match issuer")
	}
}

func TestOIDCAuthenticator_MatchingBinding(t *testing.T) {
	ctx := context.Background()
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	ts := newTestOIDCServer(t)
	defer ts.Close()

	// Create an app first
	app := &core_v1alpha.App{}
	_, err := inmem.Client.Create(ctx, "my-app", app)
	if err != nil {
		t.Fatalf("failed to create app: %v", err)
	}

	// Get the app entity ID
	var appRec core_v1alpha.App
	if err := inmem.Client.Get(ctx, "my-app", &appRec); err != nil {
		t.Fatalf("failed to get app: %v", err)
	}

	// Create an OIDC binding pointing at our test OIDC server
	binding := &core_v1alpha.OidcBinding{
		App:            appRec.EntityId(),
		Provider:       "github",
		Issuer:         ts.URL(),
		SubjectPattern: "repo:acme/app:*",
		ClaimConditions: []core_v1alpha.ClaimConditions{
			{Key: "event_name", Pattern: "push,workflow_dispatch"},
		},
	}
	_, err = inmem.Client.Create(ctx, "oidcb-test1", binding)
	if err != nil {
		t.Fatalf("failed to create OIDC binding: %v", err)
	}

	auth := NewOIDCAuthenticator(testutils.TestLogger(t))
	auth.SetEAC(inmem.EAC)

	token := ts.SignToken(jwt.MapClaims{
		"iss":        ts.URL(),
		"sub":        "repo:acme/app:ref:refs/heads/main",
		"aud":        "test-host",
		"exp":        time.Now().Add(10 * time.Minute).Unix(),
		"iat":        time.Now().Unix(),
		"event_name": "push",
	})

	req := httptest.NewRequest("GET", "https://test-host/", nil)
	req.Host = "test-host"
	req.Header.Set("Authorization", "Bearer "+token)

	identity, err := auth.Authenticate(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if identity == nil {
		t.Fatal("expected identity, got nil")
		return
	}
	if identity.Method != rpc.AuthMethodOIDC {
		t.Errorf("method = %q, want %q", identity.Method, rpc.AuthMethodOIDC)
	}
	if identity.Subject != "repo:acme/app:ref:refs/heads/main" {
		t.Errorf("subject = %q, want repo:acme/app:ref:refs/heads/main", identity.Subject)
	}
	if identity.Metadata["provider"] != "github" {
		t.Errorf("provider = %v, want github", identity.Metadata["provider"])
	}
	if identity.Metadata["bound_app"] == nil || identity.Metadata["bound_app"] == "" {
		t.Error("bound_app should be set")
	}
}

// GitHub's immutable subject claims (July 15, 2026) embed numeric IDs in sub,
// so a binding built from owner/repo names can no longer rely on it. Bindings
// created by `miren auth ci add --github` match on the repository claims, which
// that change left alone. These have to accept both subject formats, since a
// cluster will be serving repos on either side of the cutover for a long time.
func TestOIDCAuthenticator_RepositoryClaimBinding(t *testing.T) {
	subjects := map[string]string{
		"legacy subject":    "repo:acme/app:ref:refs/heads/main",
		"immutable subject": "repo:acme@277133432/app@1316584243:ref:refs/heads/main",
	}

	for name, subject := range subjects {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			inmem, cleanup := testutils.NewInMemEntityServer(t)
			defer cleanup()

			ts := newTestOIDCServer(t)
			defer ts.Close()

			app := &core_v1alpha.App{}
			if _, err := inmem.Client.Create(ctx, "my-app", app); err != nil {
				t.Fatalf("failed to create app: %v", err)
			}

			var appRec core_v1alpha.App
			if err := inmem.Client.Get(ctx, "my-app", &appRec); err != nil {
				t.Fatalf("failed to get app: %v", err)
			}

			binding := &core_v1alpha.OidcBinding{
				App:      appRec.EntityId(),
				Provider: "github",
				Issuer:   ts.URL(),
				ClaimConditions: []core_v1alpha.ClaimConditions{
					{Key: "repository", Pattern: "acme/app"},
					{Key: "repository_owner", Pattern: "acme"},
					{Key: "event_name", Pattern: "push,workflow_dispatch"},
				},
			}
			if _, err := inmem.Client.Create(ctx, "oidcb-repo-claim", binding); err != nil {
				t.Fatalf("failed to create OIDC binding: %v", err)
			}

			auth := NewOIDCAuthenticator(testutils.TestLogger(t))
			auth.SetEAC(inmem.EAC)

			token := ts.SignToken(jwt.MapClaims{
				"iss":              ts.URL(),
				"sub":              subject,
				"aud":              "test-host",
				"exp":              time.Now().Add(10 * time.Minute).Unix(),
				"iat":              time.Now().Unix(),
				"event_name":       "push",
				"repository":       "acme/app",
				"repository_owner": "acme",
			})

			req := httptest.NewRequest("GET", "https://test-host/", nil)
			req.Host = "test-host"
			req.Header.Set("Authorization", "Bearer "+token)

			identity, err := auth.Authenticate(ctx, req)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if identity == nil {
				t.Fatal("expected identity, got nil")
			}
			if identity.Subject != subject {
				t.Errorf("subject = %q, want %q", identity.Subject, subject)
			}
		})
	}
}

// The bug this all exists to fix: a binding whose subject pattern was built by
// concatenating owner/repo never matches a token from a repo created, renamed,
// or transferred after the cutover.
func TestOIDCAuthenticator_LegacySubjectPatternRejectsImmutableSubject(t *testing.T) {
	ctx := context.Background()
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	ts := newTestOIDCServer(t)
	defer ts.Close()

	app := &core_v1alpha.App{}
	if _, err := inmem.Client.Create(ctx, "my-app", app); err != nil {
		t.Fatalf("failed to create app: %v", err)
	}

	var appRec core_v1alpha.App
	if err := inmem.Client.Get(ctx, "my-app", &appRec); err != nil {
		t.Fatalf("failed to get app: %v", err)
	}

	binding := &core_v1alpha.OidcBinding{
		App:            appRec.EntityId(),
		Provider:       "github",
		Issuer:         ts.URL(),
		SubjectPattern: "repo:acme/app:*",
	}
	if _, err := inmem.Client.Create(ctx, "oidcb-legacy", binding); err != nil {
		t.Fatalf("failed to create OIDC binding: %v", err)
	}

	auth := NewOIDCAuthenticator(testutils.TestLogger(t))
	auth.SetEAC(inmem.EAC)

	subject := "repo:acme@277133432/app@1316584243:ref:refs/heads/main"
	token := ts.SignToken(jwt.MapClaims{
		"iss":        ts.URL(),
		"sub":        subject,
		"aud":        "test-host",
		"exp":        time.Now().Add(10 * time.Minute).Unix(),
		"iat":        time.Now().Unix(),
		"event_name": "push",
		"repository": "acme/app",
	})

	req := httptest.NewRequest("GET", "https://test-host/", nil)
	req.Host = "test-host"
	req.Header.Set("Authorization", "Bearer "+token)

	identity, err := auth.Authenticate(ctx, req)
	if identity != nil {
		t.Error("expected the immutable-format subject not to match a name-based pattern")
	}

	// The rejection has to explain itself: the received subject and repository
	// are what turn this from a log-spelunking session into an obvious fix.
	var mismatch *BindingMismatchError
	if !errors.As(err, &mismatch) {
		t.Fatalf("expected a BindingMismatchError, got %v", err)
	}
	if mismatch.Subject != subject {
		t.Errorf("Subject = %q, want %q", mismatch.Subject, subject)
	}
	if mismatch.Repository != "acme/app" {
		t.Errorf("Repository = %q, want acme/app", mismatch.Repository)
	}
	if mismatch.AuthErrorCode() != rpc.AuthErrorOIDCBindingMismatch {
		t.Errorf("AuthErrorCode = %q, want %q", mismatch.AuthErrorCode(), rpc.AuthErrorOIDCBindingMismatch)
	}
	if !strings.Contains(mismatch.Error(), subject) {
		t.Errorf("error message should name the rejected subject, got %q", mismatch.Error())
	}
}

func TestOIDCAuthenticator_SubjectMismatch(t *testing.T) {
	ctx := context.Background()
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	ts := newTestOIDCServer(t)
	defer ts.Close()

	app := &core_v1alpha.App{}
	_, err := inmem.Client.Create(ctx, "my-app", app)
	if err != nil {
		t.Fatalf("failed to create app: %v", err)
	}

	var appRec core_v1alpha.App
	if err := inmem.Client.Get(ctx, "my-app", &appRec); err != nil {
		t.Fatalf("failed to get app: %v", err)
	}

	binding := &core_v1alpha.OidcBinding{
		App:            appRec.EntityId(),
		Provider:       "github",
		Issuer:         ts.URL(),
		SubjectPattern: "repo:acme/other-app:*",
	}
	_, err = inmem.Client.Create(ctx, "oidcb-test2", binding)
	if err != nil {
		t.Fatalf("failed to create OIDC binding: %v", err)
	}

	auth := NewOIDCAuthenticator(testutils.TestLogger(t))
	auth.SetEAC(inmem.EAC)

	token := ts.SignToken(jwt.MapClaims{
		"iss": ts.URL(),
		"sub": "repo:acme/app:ref:refs/heads/main",
		"aud": "test-host",
		"exp": time.Now().Add(10 * time.Minute).Unix(),
		"iat": time.Now().Unix(),
	})

	req := httptest.NewRequest("GET", "https://test-host/", nil)
	req.Host = "test-host"
	req.Header.Set("Authorization", "Bearer "+token)

	identity, err := auth.Authenticate(ctx, req)
	if identity != nil {
		t.Error("expected nil identity when subject doesn't match binding pattern")
	}

	var mismatch *BindingMismatchError
	if !errors.As(err, &mismatch) {
		t.Fatalf("expected a BindingMismatchError, got %v", err)
	}
	if mismatch.Subject != "repo:acme/app:ref:refs/heads/main" {
		t.Errorf("expected the rejected subject in the error, got %q", mismatch.Subject)
	}
}

func TestOIDCAuthenticator_ClaimConditionMismatch(t *testing.T) {
	ctx := context.Background()
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	ts := newTestOIDCServer(t)
	defer ts.Close()

	app := &core_v1alpha.App{}
	_, err := inmem.Client.Create(ctx, "my-app", app)
	if err != nil {
		t.Fatalf("failed to create app: %v", err)
	}

	var appRec core_v1alpha.App
	if err := inmem.Client.Get(ctx, "my-app", &appRec); err != nil {
		t.Fatalf("failed to get app: %v", err)
	}

	binding := &core_v1alpha.OidcBinding{
		App:            appRec.EntityId(),
		Provider:       "github",
		Issuer:         ts.URL(),
		SubjectPattern: "repo:acme/app:*",
		ClaimConditions: []core_v1alpha.ClaimConditions{
			{Key: "event_name", Pattern: "push"},
		},
	}
	_, err = inmem.Client.Create(ctx, "oidcb-test3", binding)
	if err != nil {
		t.Fatalf("failed to create OIDC binding: %v", err)
	}

	auth := NewOIDCAuthenticator(testutils.TestLogger(t))
	auth.SetEAC(inmem.EAC)

	// Token has event_name=pull_request, binding requires push
	token := ts.SignToken(jwt.MapClaims{
		"iss":        ts.URL(),
		"sub":        "repo:acme/app:ref:refs/heads/main",
		"aud":        "test-host",
		"exp":        time.Now().Add(10 * time.Minute).Unix(),
		"iat":        time.Now().Unix(),
		"event_name": "pull_request",
	})

	req := httptest.NewRequest("GET", "https://test-host/", nil)
	req.Host = "test-host"
	req.Header.Set("Authorization", "Bearer "+token)

	identity, err := auth.Authenticate(ctx, req)
	if identity != nil {
		t.Error("expected nil identity when claim condition doesn't match")
	}

	var mismatch *BindingMismatchError
	if !errors.As(err, &mismatch) {
		t.Fatalf("expected a BindingMismatchError, got %v", err)
	}
}

func TestOIDCAuthenticator_ExpiredToken(t *testing.T) {
	ctx := context.Background()
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	ts := newTestOIDCServer(t)
	defer ts.Close()

	app := &core_v1alpha.App{}
	_, err := inmem.Client.Create(ctx, "my-app", app)
	if err != nil {
		t.Fatalf("failed to create app: %v", err)
	}

	var appRec core_v1alpha.App
	if err := inmem.Client.Get(ctx, "my-app", &appRec); err != nil {
		t.Fatalf("failed to get app: %v", err)
	}

	binding := &core_v1alpha.OidcBinding{
		App:            appRec.EntityId(),
		Provider:       "github",
		Issuer:         ts.URL(),
		SubjectPattern: "repo:acme/app:*",
	}
	_, err = inmem.Client.Create(ctx, "oidcb-test4", binding)
	if err != nil {
		t.Fatalf("failed to create OIDC binding: %v", err)
	}

	auth := NewOIDCAuthenticator(testutils.TestLogger(t))
	auth.SetEAC(inmem.EAC)

	token := ts.SignToken(jwt.MapClaims{
		"iss": ts.URL(),
		"sub": "repo:acme/app:ref:refs/heads/main",
		"aud": "test-host",
		"exp": time.Now().Add(-10 * time.Minute).Unix(),
		"iat": time.Now().Add(-20 * time.Minute).Unix(),
	})

	req := httptest.NewRequest("GET", "https://test-host/", nil)
	req.Host = "test-host"
	req.Header.Set("Authorization", "Bearer "+token)

	identity, err := auth.Authenticate(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if identity != nil {
		t.Error("expected nil identity for expired token")
	}
}

func TestOIDCAuthenticator_MultipleBindings_FirstMatch(t *testing.T) {
	ctx := context.Background()
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	ts := newTestOIDCServer(t)
	defer ts.Close()

	// Create two apps
	app1 := &core_v1alpha.App{}
	_, err := inmem.Client.Create(ctx, "app-one", app1)
	if err != nil {
		t.Fatalf("failed to create app1: %v", err)
	}
	var appRec1 core_v1alpha.App
	if err := inmem.Client.Get(ctx, "app-one", &appRec1); err != nil {
		t.Fatalf("failed to get app1: %v", err)
	}

	app2 := &core_v1alpha.App{}
	_, err = inmem.Client.Create(ctx, "app-two", app2)
	if err != nil {
		t.Fatalf("failed to create app2: %v", err)
	}
	var appRec2 core_v1alpha.App
	if err := inmem.Client.Get(ctx, "app-two", &appRec2); err != nil {
		t.Fatalf("failed to get app2: %v", err)
	}

	// Binding for app-one: matches repo:acme/app-one:*
	b1 := &core_v1alpha.OidcBinding{
		App:            appRec1.EntityId(),
		Provider:       "github",
		Issuer:         ts.URL(),
		SubjectPattern: "repo:acme/app-one:*",
	}
	_, err = inmem.Client.Create(ctx, "oidcb-multi1", b1)
	if err != nil {
		t.Fatalf("failed to create binding1: %v", err)
	}

	// Binding for app-two: matches repo:acme/app-two:*
	b2 := &core_v1alpha.OidcBinding{
		App:            appRec2.EntityId(),
		Provider:       "github",
		Issuer:         ts.URL(),
		SubjectPattern: "repo:acme/app-two:*",
	}
	_, err = inmem.Client.Create(ctx, "oidcb-multi2", b2)
	if err != nil {
		t.Fatalf("failed to create binding2: %v", err)
	}

	auth := NewOIDCAuthenticator(testutils.TestLogger(t))
	auth.SetEAC(inmem.EAC)

	// Token subject matches app-two's binding
	token := ts.SignToken(jwt.MapClaims{
		"iss": ts.URL(),
		"sub": "repo:acme/app-two:ref:refs/heads/main",
		"aud": "test-host",
		"exp": time.Now().Add(10 * time.Minute).Unix(),
		"iat": time.Now().Unix(),
	})

	req := httptest.NewRequest("GET", "https://test-host/", nil)
	req.Host = "test-host"
	req.Header.Set("Authorization", "Bearer "+token)

	identity, err := auth.Authenticate(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if identity == nil {
		t.Fatal("expected identity, got nil")
		return
	}
	if identity.Method != rpc.AuthMethodOIDC {
		t.Errorf("method = %q, want %q", identity.Method, rpc.AuthMethodOIDC)
	}
	if identity.Subject != "repo:acme/app-two:ref:refs/heads/main" {
		t.Errorf("subject = %q, want repo:acme/app-two:ref:refs/heads/main", identity.Subject)
	}
	boundApp, _ := identity.Metadata["bound_app"].(string)
	if boundApp != "app-two" {
		t.Errorf("bound_app = %q, want app-two", boundApp)
	}
}

func TestOIDCAuthenticator_AudienceFromHost(t *testing.T) {
	ctx := context.Background()
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	ts := newTestOIDCServer(t)
	defer ts.Close()

	app := &core_v1alpha.App{}
	_, err := inmem.Client.Create(ctx, "my-app", app)
	if err != nil {
		t.Fatalf("failed to create app: %v", err)
	}

	var appRec core_v1alpha.App
	if err := inmem.Client.Get(ctx, "my-app", &appRec); err != nil {
		t.Fatalf("failed to get app: %v", err)
	}

	binding := &core_v1alpha.OidcBinding{
		App:            appRec.EntityId(),
		Provider:       "github",
		Issuer:         ts.URL(),
		SubjectPattern: "repo:acme/app:*",
	}
	_, err = inmem.Client.Create(ctx, "oidcb-aud1", binding)
	if err != nil {
		t.Fatalf("failed to create OIDC binding: %v", err)
	}

	auth := NewOIDCAuthenticator(testutils.TestLogger(t))
	auth.SetEAC(inmem.EAC)

	// Token with audience matching the Host header
	token := ts.SignToken(jwt.MapClaims{
		"iss": ts.URL(),
		"sub": "repo:acme/app:ref:refs/heads/main",
		"aud": "my-cluster.example.com",
		"exp": time.Now().Add(10 * time.Minute).Unix(),
		"iat": time.Now().Unix(),
	})

	req := httptest.NewRequest("GET", "https://my-cluster.example.com/", nil)
	req.Host = "my-cluster.example.com"
	req.Header.Set("Authorization", "Bearer "+token)

	identity, err := auth.Authenticate(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if identity == nil {
		t.Fatal("expected identity, got nil")
	}
}

func TestOIDCAuthenticator_AudienceFromTLS(t *testing.T) {
	ctx := context.Background()
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	ts := newTestOIDCServer(t)
	defer ts.Close()

	app := &core_v1alpha.App{}
	_, err := inmem.Client.Create(ctx, "my-app", app)
	if err != nil {
		t.Fatalf("failed to create app: %v", err)
	}

	var appRec core_v1alpha.App
	if err := inmem.Client.Get(ctx, "my-app", &appRec); err != nil {
		t.Fatalf("failed to get app: %v", err)
	}

	binding := &core_v1alpha.OidcBinding{
		App:            appRec.EntityId(),
		Provider:       "github",
		Issuer:         ts.URL(),
		SubjectPattern: "repo:acme/app:*",
	}
	_, err = inmem.Client.Create(ctx, "oidcb-aud2", binding)
	if err != nil {
		t.Fatalf("failed to create OIDC binding: %v", err)
	}

	auth := NewOIDCAuthenticator(testutils.TestLogger(t))
	auth.SetEAC(inmem.EAC)

	// Token with audience matching the TLS ServerName
	token := ts.SignToken(jwt.MapClaims{
		"iss": ts.URL(),
		"sub": "repo:acme/app:ref:refs/heads/main",
		"aud": "tls-server.example.com",
		"exp": time.Now().Add(10 * time.Minute).Unix(),
		"iat": time.Now().Unix(),
	})

	req := httptest.NewRequest("GET", "https://tls-server.example.com/", nil)
	req.Host = "" // Empty Host, should fall back to TLS ServerName
	req.TLS = &tls.ConnectionState{ServerName: "tls-server.example.com"}
	req.Header.Set("Authorization", "Bearer "+token)

	identity, err := auth.Authenticate(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if identity == nil {
		t.Fatal("expected identity, got nil")
	}
}

func TestOIDCAuthenticator_WrongAudience(t *testing.T) {
	ctx := context.Background()
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	ts := newTestOIDCServer(t)
	defer ts.Close()

	app := &core_v1alpha.App{}
	_, err := inmem.Client.Create(ctx, "my-app", app)
	if err != nil {
		t.Fatalf("failed to create app: %v", err)
	}

	var appRec core_v1alpha.App
	if err := inmem.Client.Get(ctx, "my-app", &appRec); err != nil {
		t.Fatalf("failed to get app: %v", err)
	}

	binding := &core_v1alpha.OidcBinding{
		App:            appRec.EntityId(),
		Provider:       "github",
		Issuer:         ts.URL(),
		SubjectPattern: "repo:acme/app:*",
	}
	_, err = inmem.Client.Create(ctx, "oidcb-wrongaud", binding)
	if err != nil {
		t.Fatalf("failed to create OIDC binding: %v", err)
	}

	auth := NewOIDCAuthenticator(testutils.TestLogger(t))
	auth.SetEAC(inmem.EAC)

	// Token has wrong audience
	token := ts.SignToken(jwt.MapClaims{
		"iss": ts.URL(),
		"sub": "repo:acme/app:ref:refs/heads/main",
		"aud": "wrong-audience",
		"exp": time.Now().Add(10 * time.Minute).Unix(),
		"iat": time.Now().Unix(),
	})

	req := httptest.NewRequest("GET", "https://test-host/", nil)
	req.Host = "test-host"
	req.Header.Set("Authorization", "Bearer "+token)

	identity, err := auth.Authenticate(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if identity != nil {
		t.Error("expected nil identity for wrong audience")
	}
}
