package build

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"miren.dev/runtime/api/app"
	"miren.dev/runtime/api/build/build_v1alpha"
	"miren.dev/runtime/pkg/rpc"
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
