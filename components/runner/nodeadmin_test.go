package runner

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/pkg/rpc"
)

func TestRequireCoordinatorRejectsAnAnonymousCaller(t *testing.T) {
	// The runner's listener is built with WithSkipVerify and no
	// authenticator, so an unauthenticated caller reaches the handler. This
	// endpoint pulls an image, runs it, and loads the result into the kernel
	// as root, so anonymous must not get through.
	err := requireCoordinator(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "presented none")

	err = requireCoordinator(rpc.ContextWithIdentity(context.Background(),
		&rpc.Identity{Method: rpc.AuthMethodAnonymous}))
	require.Error(t, err)
}

func TestRequireCoordinatorRejectsAnotherCertHolder(t *testing.T) {
	// A registered runner holds a valid cluster certificate. Holding one is
	// not the same as being the coordinator.
	err := requireCoordinator(rpc.ContextWithIdentity(context.Background(),
		&rpc.Identity{Method: rpc.AuthMethodCert, Subject: "runner-abc123"}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "only the coordinator")
}

func TestRequireCoordinatorRejectsANonCertMethod(t *testing.T) {
	// A bearer token or JWT carrying the right subject is still not the
	// coordinator's certificate.
	err := requireCoordinator(rpc.ContextWithIdentity(context.Background(),
		&rpc.Identity{Method: rpc.AuthMethodJWT, Subject: rpc.CoordinatorCertSubject}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "requires a certificate")
}

func TestRequireCoordinatorAcceptsTheCoordinator(t *testing.T) {
	require.NoError(t, requireCoordinator(rpc.ContextWithIdentity(context.Background(),
		&rpc.Identity{Method: rpc.AuthMethodCert, Subject: rpc.CoordinatorCertSubject})))
}
