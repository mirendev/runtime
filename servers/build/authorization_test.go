package build

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"miren.dev/runtime/api/app"
	"miren.dev/runtime/api/build/build_v1alpha"
	"miren.dev/runtime/pkg/oidcauth"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/rpc/stream"
	"miren.dev/runtime/pkg/saga"
	"miren.dev/runtime/pkg/workloadroles"
)

type buildPolicy struct {
	create bool
	app    string
	owner  string
}

func (*buildPolicy) Authorize(context.Context, *rpc.Identity, string, string) error { return nil }
func (p *buildPolicy) AuthorizeResource(_ context.Context, _ *rpc.Identity, r rpc.ResourceRequest) error {
	if r.Action == "create" && p.create || r.Action == "deploy" && r.Instance == p.app && r.Owner == p.owner {
		return nil
	}
	return rpc.ErrUnauthorized
}

func TestPrepareUploadAuthorizesBeforeWriting(t *testing.T) {
	b := newDeployTestBuilder(t)
	_, _, err := app.EnsureApp(context.Background(), b.EAS, "existing", "owner")
	require.NoError(t, err)
	client := &build_v1alpha.BuilderClient{Client: rpc.LocalClient(build_v1alpha.AdaptBuilder(&SagaBuilder{inner: b}))}
	for _, tc := range []struct {
		name, target, grant string
		create              bool
		denied              bool
	}{
		{"wrong scope", "existing", "other", false, true},
		{"allowed existing", "existing", "existing", false, false},
		{"deploy cannot create", "missing", "missing", false, true},
		{"create and deploy", "missing", "missing", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			owner := "owner"
			if tc.target == "missing" {
				owner = "creator"
			}
			policy := &buildPolicy{app: tc.grant, create: tc.create, owner: owner}
			ctx := rpc.ContextWithAuthorization(context.Background(), policy, "builder", "prepareupload")
			ctx = rpc.ContextWithIdentity(ctx, &rpc.Identity{Subject: "creator", Method: rpc.AuthMethodJWT})
			_, err := client.PrepareUpload(ctx, tc.target, nil)
			if tc.denied {
				require.ErrorIs(t, err, rpc.ErrUnauthorized)
			} else {
				// The authorized request reaches configuration validation, before any disk write.
				require.ErrorContains(t, err, "data path not configured")
				require.False(t, errors.Is(err, rpc.ErrUnauthorized))
			}
			_, err = b.EAS.Get(context.Background(), "app/missing")
			require.Error(t, err, "authorization must never create the app")
		})
	}
}

func TestPreparedUploadDenialDoesNotConsumeSession(t *testing.T) {
	b := newDeployTestBuilder(t)
	sess := &buildSession{appName: "existing", subject: "owner", cancelFunc: func() { t.Fatal("denied caller canceled session") }}
	b.sessions.Store("session", sess)
	client := &build_v1alpha.BuilderClient{Client: rpc.LocalClient(build_v1alpha.AdaptBuilder(&SagaBuilder{inner: b}))}
	ctx := rpc.ContextWithAuthorization(context.Background(), &buildPolicy{app: "existing"}, "builder", "buildfromprepared")
	ctx = rpc.ContextWithIdentity(ctx, &rpc.Identity{Subject: "other", Method: rpc.AuthMethodJWT})
	_, err := client.BuildFromPrepared(ctx, "session", nil, nil, nil, "", "", nil)
	require.ErrorIs(t, err, rpc.ErrUnauthorized)
	ctx = rpc.ContextWithIdentity(ctx, &rpc.Identity{Subject: "owner", Method: rpc.AuthMethodJWT})
	_, err = client.BuildFromPrepared(ctx, "session", nil, nil, nil, "", "", nil)
	require.ErrorIs(t, err, rpc.ErrUnauthorized, "session must still exist: its owner reaches the missing create-permission check")
}

func TestRebuildAuthorizesBeforeReturningAppState(t *testing.T) {
	b := newDeployTestBuilder(t)
	_, _, err := app.EnsureApp(t.Context(), b.EAS, "existing", "owner")
	require.NoError(t, err)
	client := build_v1alpha.NewBuilderClient(rpc.LocalClient(build_v1alpha.AdaptBuilder(&SagaBuilder{inner: b})))
	for _, tc := range []struct {
		name, target, grant string
		denied              bool
	}{
		{"denied existing", "existing", "other", true},
		{"denied missing", "missing", "other", true},
		{"allowed existing", "existing", "existing", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := rpc.ContextWithAuthorization(t.Context(), &buildPolicy{app: tc.grant, owner: "owner"}, "builder", "rebuild")
			ctx = rpc.ContextWithIdentity(ctx, &rpc.Identity{Subject: "caller", Method: rpc.AuthMethodJWT})
			_, err := client.Rebuild(ctx, tc.target, nil, nil)
			if tc.denied {
				require.ErrorIs(t, err, rpc.ErrUnauthorized)
			} else {
				require.ErrorContains(t, err, "no deployed version")
			}
		})
	}
}

func TestBoundOIDCAnalyzeSource(t *testing.T) {
	b := newDeployTestBuilder(t)
	b.TempDir = t.TempDir()
	identity := &rpc.Identity{Subject: "pipeline", Method: rpc.AuthMethodOIDC, Metadata: map[string]any{"bound_app": "mine"}}
	auth := oidcauth.NewCompositeAuthorizer(nil)
	ctx := rpc.ContextWithIdentity(t.Context(), identity)
	ctx = rpc.ContextWithAuthorization(ctx, auth, "builder", "analyzeapp")
	require.NoError(t, auth.Authorize(ctx, identity, "builder", "analyzeapp"))
	client := build_v1alpha.BuilderClient{Client: rpc.LocalClient(build_v1alpha.AdaptBuilder(&SagaBuilder{inner: b}))}
	result, err := client.AnalyzeApp(ctx, stream.ServeReader(ctx, makeTar(t, map[string]string{
		"Dockerfile.miren": "FROM scratch\n", ".miren/app.toml": "name = 'source-preview'\n",
	})))
	require.NoError(t, err)
	require.Equal(t, "dockerfile", result.Result().Stack())
	require.Equal(t, "source-preview", result.Result().AppName())
}

func TestLegacyFirstBuildRetainsAppConfinement(t *testing.T) {
	for _, tc := range []struct {
		name, role string
		method     rpc.AuthMethod
		bound      string
	}{
		{"oidc", "", rpc.AuthMethodOIDC, "mine"},
		{"app deployer", workloadroles.RoleAppDeployer, rpc.AuthMethodWorkload, "mine"},
		{"app admin", workloadroles.RoleAppAdmin, rpc.AuthMethodWorkload, "mine"},
		{"cluster deployer", workloadroles.RoleClusterDeployer, rpc.AuthMethodWorkload, ""},
		{"certificate", "", rpc.AuthMethodCert, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := newDeployTestBuilder(t)
			identity := &rpc.Identity{Subject: "pipeline", Method: tc.method, Metadata: map[string]any{"bound_app": tc.bound, "app": tc.bound, "role": tc.role}}
			auth := oidcauth.NewCompositeAuthorizer(nil)
			ctx := rpc.ContextWithIdentity(t.Context(), identity)
			ctx = rpc.ContextWithAuthorization(ctx, auth, "builder", "buildfromtar")
			require.NoError(t, auth.Authorize(ctx, identity, "builder", "buildfromtar"))
			client := build_v1alpha.BuilderClient{Client: rpc.LocalClient(build_v1alpha.AdaptBuilder(&SagaBuilder{inner: b}))}
			request := &build_v1alpha.DeployRequest{}
			request.SetMessage("preview message")
			// Validation follows authorization but precedes staging. A missing app
			// must not be rejected for lacking a new create privilege.
			_, err := client.BuildFromTar(ctx, "mine", nil, nil, nil, "preview", "", request)
			require.ErrorContains(t, err, "deployment message is not supported")
			_, err = client.BuildFromTar(ctx, "other", nil, nil, nil, "preview", "", request)
			if tc.bound != "" {
				require.ErrorIs(t, err, rpc.ErrUnauthorized)
				_, err = b.EAS.Get(t.Context(), "app/other")
				require.Error(t, err, "denial must not create another app")
			} else {
				require.ErrorContains(t, err, "deployment message is not supported")
			}
		})
	}
}

func TestLegacyPreparedUploadHandoff(t *testing.T) {
	for _, role := range []string{"certificate", workloadroles.RoleAppDeployer, workloadroles.RoleClusterDeployer} {
		t.Run(role, func(t *testing.T) {
			b := newDeployTestBuilder(t)
			b.TempDir = t.TempDir()
			s := NewSagaBuilder(b, saga.NewMemoryStorage(), b.Log)
			b.sessions.Store("session", &buildSession{appName: "mine", subject: "initiator", dir: t.TempDir(), cancelFunc: func() {}})
			identity := &rpc.Identity{Subject: "consumer", Method: rpc.AuthMethodWorkload, Metadata: map[string]any{"role": role, "app": "mine"}}
			switch role {
			case "certificate":
				identity.Method = rpc.AuthMethodCert
			case workloadroles.RoleClusterDeployer:
				identity.Metadata["app"] = ""
			}
			auth := oidcauth.NewCompositeAuthorizer(nil)
			ctx := rpc.ContextWithIdentity(t.Context(), identity)
			ctx = rpc.ContextWithAuthorization(ctx, auth, "builder", "buildfromprepared")
			require.NoError(t, auth.Authorize(ctx, identity, "builder", "buildfromprepared"))
			client := build_v1alpha.BuilderClient{Client: rpc.LocalClient(build_v1alpha.AdaptBuilder(s))}
			_, err := client.BuildFromPrepared(ctx, "session", stream.ServeReader(ctx, strings.NewReader("invalid archive")),
				stream.StreamRecv(func(*build_v1alpha.Status) error { return nil }), nil, "", "", nil)
			require.ErrorContains(t, err, "error extracting changed files", "handoff must reach source processing, not a new subject gate")
			_, exists := b.sessions.Load("session")
			require.False(t, exists)
		})
	}
}
