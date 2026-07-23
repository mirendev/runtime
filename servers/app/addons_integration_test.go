package app

import (
	"context"
	"log/slog"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"miren.dev/runtime/api/addon/addon_v1alpha"
	"miren.dev/runtime/api/app/app_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/testutils"
)

// setupAddonsTestEtcd wires the AddonsServer over a real etcd-backed entity
// store, which (unlike the mock) enforces ident-unique Create and
// revision-guarded Patch — the two primitives the admission gate relies on.
// Requires the dev environment's etcd; skip in -short.
func setupAddonsTestEtcd(t *testing.T) (context.Context, *app_v1alpha.AddonsClient, *entityserver.Client) {
	t.Helper()

	ctx := context.Background()
	etcd, cleanup := testutils.NewEtcdEntityServer(t)
	t.Cleanup(cleanup)

	ec := entityserver.NewClient(slog.Default(), etcd.EAC)
	return ctx, newAddonsClient(t, ctx, ec), ec
}

// TestRotateCredentialConcurrentAdmissionEtcd is the real-store proof that the
// admission gate is atomic. Many RotateCredential calls firing at once on one
// association must yield exactly one rotation_request; the rest are rejected.
// This is the race the old list-then-create check let slip through, and the
// mock store can't verify it (it enforces neither ident-unique Create nor the
// revision guard), so it runs against real etcd.
func TestRotateCredentialConcurrentAdmissionEtcd(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx, client, ec := setupAddonsTestEtcd(t)

	_, err := ec.Create(ctx, "myapp", &core_v1alpha.App{})
	require.NoError(t, err)
	assocID := createActiveAddon(t, ctx, client, ec, "myapp")

	const n = 8
	var wg sync.WaitGroup
	ids := make([]string, n)
	errs := make([]error, n)
	start := make(chan struct{})

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start // release all goroutines together to maximize contention
			res, err := client.RotateCredential(ctx, "myapp", "miren-postgresql", "app")
			if err != nil {
				errs[i] = err
				return
			}
			ids[i] = res.Id()
		}(i)
	}
	close(start)
	wg.Wait()

	var admitted, rejected int
	var admittedID string
	for i := 0; i < n; i++ {
		if errs[i] == nil {
			admitted++
			admittedID = ids[i]
		} else {
			rejected++
			assert.Contains(t, errs[i].Error(), "rotation", "rejection should mention the rotation")
		}
	}

	assert.Equal(t, 1, admitted, "exactly one rotation should be admitted")
	assert.Equal(t, n-1, rejected, "every other caller should be rejected")
	assert.NotEmpty(t, admittedID)
	assert.Equal(t, 1, countRotationRequests(t, ctx, ec, assocID), "only one rotation_request should exist")
}

// TestRotateCredentialReclaimAfterDoneEtcd verifies the terminal->pending
// reclaim against real optimistic concurrency: the handler reads the request's
// revision and Patches at it, so a wrong or stale revision would be rejected by
// etcd. The mock can't catch that (it ignores the revision guard), so this
// confirms the CAS path passes the correct revision on a real store.
func TestRotateCredentialReclaimAfterDoneEtcd(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx, client, ec := setupAddonsTestEtcd(t)

	_, err := ec.Create(ctx, "myapp", &core_v1alpha.App{})
	require.NoError(t, err)
	assocID := createActiveAddon(t, ctx, client, ec, "myapp")

	res1, err := client.RotateCredential(ctx, "myapp", "miren-postgresql", "app")
	require.NoError(t, err)
	reqID := entity.Id(res1.Id())

	// Simulate the controller finishing, with a leftover secret to prove reclaim
	// clears it.
	require.NoError(t, ec.Patch(ctx, reqID, 0,
		entity.String(addon_v1alpha.RotationRequestStatusId, "done"),
		entity.String(addon_v1alpha.RotationRequestNewSecretId, "leftover")))

	res2, err := client.RotateCredential(ctx, "myapp", "miren-postgresql", "root")
	require.NoError(t, err)
	assert.Equal(t, reqID, entity.Id(res2.Id()), "terminal request should be reclaimed in place")

	var req addon_v1alpha.RotationRequest
	require.NoError(t, ec.GetById(ctx, reqID, &req))
	assert.Equal(t, "pending", req.Status)
	assert.Equal(t, "root", req.Credential)
	assert.Empty(t, req.NewSecret)
	assert.Equal(t, 1, countRotationRequests(t, ctx, ec, assocID))
}
