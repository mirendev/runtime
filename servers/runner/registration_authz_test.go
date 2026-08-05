package runner

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/pkg/caauth"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/testutils"
	"miren.dev/runtime/pkg/rpc"
)

const (
	authzRunnerID  = "abcd1234-5678-90ab-cdef-1234567890ab"
	authzSandboxID = "sandbox/myapp-web-aaa111"
)

// newOwnershipTestServer builds a RegistrationServer backed by an in-memory
// entity store containing a node owned by authzRunnerID and a sandbox scheduled
// to it.
func newOwnershipTestServer(t *testing.T) (*RegistrationServer, func()) {
	t.Helper()

	es, cleanup := testutils.NewInMemEntityServer(t)

	ca, err := caauth.New(caauth.Options{CommonName: "test-ca", Organization: "test"})
	require.NoError(t, err)

	srv := NewRegistrationServer(RegistrationServerConfig{
		Log:       testutils.TestLogger(t),
		Authority: ca,
		EAC:       es.EAC,
	})

	ctx := context.Background()
	nodeID := compute_v1alpha.NewNodeId(authzRunnerID).Id()

	_, err = es.EAC.Create(ctx, entity.New(
		entity.DBId, nodeID,
		(&compute_v1alpha.Node{RunnerId: authzRunnerID}).Encode,
	).Attrs())
	require.NoError(t, err)

	schedule := compute_v1alpha.Schedule{
		Key: compute_v1alpha.Key{Kind: compute_v1alpha.KindSandbox, Node: nodeID},
	}
	_, err = es.EAC.Create(ctx, entity.New(
		entity.DBId, entity.Id(authzSandboxID),
		(&compute_v1alpha.Sandbox{}).Encode,
		schedule.Encode,
	).Attrs())
	require.NoError(t, err)

	return srv, cleanup
}

func ctxWithCert(subject string) context.Context {
	return rpc.ContextWithIdentity(context.Background(), &rpc.Identity{
		Subject: subject,
		Method:  rpc.AuthMethodCert,
	})
}

func TestAuthorizeSandboxOwnership_Owner(t *testing.T) {
	srv, cleanup := newOwnershipTestServer(t)
	defer cleanup()

	ctx := ctxWithCert(runnerCertName(authzRunnerID))
	require.NoError(t, srv.authorizeSandboxOwnership(ctx, authzSandboxID))
}

func TestAuthorizeSandboxOwnership_OtherRunnerDenied(t *testing.T) {
	srv, cleanup := newOwnershipTestServer(t)
	defer cleanup()

	ctx := ctxWithCert(runnerCertName("99999999-0000-0000-0000-000000000000"))
	require.Error(t, srv.authorizeSandboxOwnership(ctx, authzSandboxID))
}

func TestAuthorizeSandboxOwnership_AnonymousSkipped(t *testing.T) {
	srv, cleanup := newOwnershipTestServer(t)
	defer cleanup()

	ctx := rpc.ContextWithIdentity(context.Background(), &rpc.Identity{
		Subject: "anonymous",
		Method:  rpc.AuthMethodAnonymous,
	})
	require.NoError(t, srv.authorizeSandboxOwnership(ctx, authzSandboxID))
}

func TestAuthorizeSandboxOwnership_NonCertDenied(t *testing.T) {
	srv, cleanup := newOwnershipTestServer(t)
	defer cleanup()

	ctx := rpc.ContextWithIdentity(context.Background(), &rpc.Identity{
		Subject: "user@example.com",
		Method:  rpc.AuthMethodJWT,
	})
	require.Error(t, srv.authorizeSandboxOwnership(ctx, authzSandboxID))
}

func TestAuthorizeSandboxOwnership_NoIdentityDenied(t *testing.T) {
	srv, cleanup := newOwnershipTestServer(t)
	defer cleanup()

	require.Error(t, srv.authorizeSandboxOwnership(context.Background(), authzSandboxID))
}

func TestAuthorizeSandboxOwnership_MissingSandboxDenied(t *testing.T) {
	srv, cleanup := newOwnershipTestServer(t)
	defer cleanup()

	ctx := ctxWithCert(runnerCertName(authzRunnerID))
	require.Error(t, srv.authorizeSandboxOwnership(ctx, "sandbox/does-not-exist"))
}

func TestAuthorizeSandboxOwnership_UnscheduledDenied(t *testing.T) {
	ctx := context.Background()
	es, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	ca, err := caauth.New(caauth.Options{CommonName: "test-ca", Organization: "test"})
	require.NoError(t, err)

	srv := NewRegistrationServer(RegistrationServerConfig{
		Log:       testutils.TestLogger(t),
		Authority: ca,
		EAC:       es.EAC,
	})

	// A sandbox that exists but has no scheduling node, exercising the
	// sch.Key.Node == "" branch rather than the missing-entity path.
	_, err = es.EAC.Create(ctx, entity.New(
		entity.DBId, entity.Id("sandbox/unscheduled"),
		(&compute_v1alpha.Sandbox{}).Encode,
	).Attrs())
	require.NoError(t, err)

	authCtx := ctxWithCert(runnerCertName(authzRunnerID))
	err = srv.authorizeSandboxOwnership(authCtx, "sandbox/unscheduled")
	require.ErrorContains(t, err, "not scheduled to a node")
}

func TestRunnerCertNameUsesFullID(t *testing.T) {
	// Two runner IDs sharing an 8-char prefix must map to distinct cert names,
	// otherwise a runner could choose a colliding ID and impersonate another.
	a := "abcd1234-1111-1111-1111-111111111111"
	b := "abcd1234-2222-2222-2222-222222222222"
	require.NotEqual(t, runnerCertName(a), runnerCertName(b))
	require.Equal(t, "runner-"+a, runnerCertName(a))
}

func TestResolveSandboxApp(t *testing.T) {
	ctx := context.Background()
	es, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	ca, err := caauth.New(caauth.Options{CommonName: "test-ca", Organization: "test"})
	require.NoError(t, err)

	srv := NewRegistrationServer(RegistrationServerConfig{
		Log:       testutils.TestLogger(t),
		Authority: ca,
		EAC:       es.EAC,
	})

	_, err = es.EAC.Create(ctx, entity.New(
		entity.DBId, entity.Id("app/myapp"),
		(&core_v1alpha.Metadata{Name: "myapp"}).Encode,
	).Attrs())
	require.NoError(t, err)

	_, err = es.EAC.Create(ctx, entity.New(
		entity.DBId, entity.Id("version/v1"),
		(&core_v1alpha.AppVersion{App: "app/myapp"}).Encode,
	).Attrs())
	require.NoError(t, err)

	_, err = es.EAC.Create(ctx, entity.New(
		entity.DBId, entity.Id("sandbox/s1"),
		(&compute_v1alpha.Sandbox{Spec: compute_v1alpha.SandboxSpec{Version: "version/v1"}}).Encode,
	).Attrs())
	require.NoError(t, err)

	require.Equal(t, "myapp", srv.resolveSandboxApp(ctx, "sandbox/s1"))
}

// The distributed-runner mint path must derive the role server-side, exactly as
// it does the app — a runner must not be able to forge the authority its
// sandboxes' tokens carry.
func TestResolveSandboxAppAndRole(t *testing.T) {
	ctx := context.Background()
	es, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	ca, err := caauth.New(caauth.Options{CommonName: "test-ca", Organization: "test"})
	require.NoError(t, err)

	srv := NewRegistrationServer(RegistrationServerConfig{
		Log:       testutils.TestLogger(t),
		Authority: ca,
		EAC:       es.EAC,
	})

	_, err = es.EAC.Create(ctx, entity.New(
		entity.DBId, entity.Id("app/myapp"),
		(&core_v1alpha.Metadata{Name: "myapp"}).Encode,
		(&core_v1alpha.App{WorkloadRole: "app-admin"}).Encode,
	).Attrs())
	require.NoError(t, err)

	_, err = es.EAC.Create(ctx, entity.New(
		entity.DBId, entity.Id("version/v1"),
		(&core_v1alpha.AppVersion{App: "app/myapp"}).Encode,
	).Attrs())
	require.NoError(t, err)

	_, err = es.EAC.Create(ctx, entity.New(
		entity.DBId, entity.Id("sandbox/s1"),
		(&compute_v1alpha.Sandbox{Spec: compute_v1alpha.SandboxSpec{Version: "version/v1"}}).Encode,
	).Attrs())
	require.NoError(t, err)

	name, role := srv.resolveSandboxAppAndRole(ctx, "sandbox/s1")
	require.Equal(t, "myapp", name)
	require.Equal(t, "app-admin", role)
}

func TestResolveSandboxApp_NoVersionReturnsEmpty(t *testing.T) {
	ctx := context.Background()
	es, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	ca, err := caauth.New(caauth.Options{CommonName: "test-ca", Organization: "test"})
	require.NoError(t, err)

	srv := NewRegistrationServer(RegistrationServerConfig{
		Log:       testutils.TestLogger(t),
		Authority: ca,
		EAC:       es.EAC,
	})

	_, err = es.EAC.Create(ctx, entity.New(
		entity.DBId, entity.Id("sandbox/no-version"),
		(&compute_v1alpha.Sandbox{}).Encode,
	).Attrs())
	require.NoError(t, err)

	require.Equal(t, "", srv.resolveSandboxApp(ctx, "sandbox/no-version"))
}
