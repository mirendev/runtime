package cluster

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/pkg/secret"
)

// newApp creates an app entity for a secret to belong to; the store refuses an
// owner that does not exist.
func newApp(t *testing.T, ec *entityserver.Client, name string) string {
	t.Helper()
	id, err := ec.Create(t.Context(), name, &core_v1alpha.App{})
	require.NoError(t, err)
	return string(id)
}

// The first write on an app's behalf makes the secret that app's, and every
// later read reports it.
func TestPutOwnedRecordsTheOwner(t *testing.T) {
	b, ec := newTestBackend(t)
	ctx := t.Context()
	shop := newApp(t, ec, "shop")

	_, _, err := b.PutOwned(ctx, "shop/db", []byte("v1"), secret.Ownership{App: shop})
	require.NoError(t, err)

	owner, err := b.Owner(ctx, "shop/db")
	require.NoError(t, err)
	assert.Equal(t, shop, owner)

	got, err := b.Resolve(ctx, "shop/db")
	require.NoError(t, err)
	assert.Equal(t, shop, got.Owner)

	listed, err := b.List(ctx)
	require.NoError(t, err)
	require.Len(t, listed, 1)
	assert.Equal(t, shop, listed[0].Owner)
}

// A write for one app never lands on a secret another app owns, while an
// ownerless write, which is what an operator makes, still can.
func TestPutOwnedRefusesAnotherAppsSecret(t *testing.T) {
	b, ec := newTestBackend(t)
	ctx := t.Context()
	shop := newApp(t, ec, "shop")
	other := newApp(t, ec, "other")

	_, _, err := b.PutOwned(ctx, "shop/db", []byte("v1"), secret.Ownership{App: shop})
	require.NoError(t, err)

	_, _, err = b.PutOwned(ctx, "shop/db", []byte("stolen"), secret.Ownership{App: other})
	require.ErrorIs(t, err, secret.ErrOwnedByOtherApp)

	got, err := b.Resolve(ctx, "shop/db")
	require.NoError(t, err)
	assert.Equal(t, []byte("v1"), got.Bytes)

	_, _, err = b.Put(ctx, "shop/db", []byte("v2"))
	require.NoError(t, err)
	owner, err := b.Owner(ctx, "shop/db")
	require.NoError(t, err)
	assert.Equal(t, shop, owner, "an operator write keeps the owner")
}

// Writing to an existing unowned secret on an app's behalf does not claim it.
func TestPutOwnedLeavesAnUnownedSecretUnowned(t *testing.T) {
	b, ec := newTestBackend(t)
	ctx := t.Context()
	shop := newApp(t, ec, "shop")

	_, _, err := b.Put(ctx, "shared/key", []byte("v1"))
	require.NoError(t, err)
	_, _, err = b.PutOwned(ctx, "shared/key", []byte("v2"), secret.Ownership{App: shop})
	require.NoError(t, err)

	owner, err := b.Owner(ctx, "shared/key")
	require.NoError(t, err)
	assert.Empty(t, owner)
}

// A write that requires an owner refuses an existing unowned secret rather than
// leaving it unowned, and writes nothing.
func TestPutOwnedRequireRefusesAnUnownedSecret(t *testing.T) {
	b, ec := newTestBackend(t)
	ctx := t.Context()
	shop := newApp(t, ec, "shop")

	_, _, err := b.Put(ctx, "shared/key", []byte("v1"))
	require.NoError(t, err)
	_, _, err = b.PutOwned(ctx, "shared/key", []byte("v2"), secret.Ownership{App: shop, Require: true})
	require.ErrorIs(t, err, secret.ErrNotOwned)

	got, err := b.Resolve(ctx, "shared/key")
	require.NoError(t, err)
	assert.Equal(t, []byte("v1"), got.Bytes)

	// Creating, or writing again to a secret it already owns, is fine.
	_, _, err = b.PutOwned(ctx, "shop/key", []byte("v1"), secret.Ownership{App: shop, Require: true})
	require.NoError(t, err)
	_, _, err = b.PutOwned(ctx, "shop/key", []byte("v2"), secret.Ownership{App: shop, Require: true})
	require.NoError(t, err)
}

// The Authorize hook sees the owner of the row actually being written, and a
// refusal stops the write before anything lands.
func TestPutOwnedAuthorizesTheRowItWrites(t *testing.T) {
	b, ec := newTestBackend(t)
	ctx := t.Context()
	shop := newApp(t, ec, "shop")

	_, _, err := b.PutOwned(ctx, "shop/db", []byte("v1"), secret.Ownership{App: shop})
	require.NoError(t, err)

	var saw string
	denied := errors.New("denied")
	_, _, err = b.PutOwned(ctx, "shop/db", []byte("v2"), secret.Ownership{
		Authorize: func(_ context.Context, owner string) error {
			saw = owner
			return denied
		},
	})
	require.ErrorIs(t, err, denied)
	assert.Equal(t, shop, saw)

	got, err := b.Resolve(ctx, "shop/db")
	require.NoError(t, err)
	assert.Equal(t, []byte("v1"), got.Bytes)
}

// Two apps racing to create the same path end with exactly one owner, and the
// loser's write is refused rather than landing on the winner's secret.
func TestPutOwnedRaceHasOneWinner(t *testing.T) {
	b, ec := newTestBackend(t)
	ctx := t.Context()

	apps := []string{newApp(t, ec, "a"), newApp(t, ec, "b"), newApp(t, ec, "c"), newApp(t, ec, "d")}
	errs := make([]error, len(apps))
	var wg sync.WaitGroup
	for i, app := range apps {
		wg.Go(func() {
			_, _, errs[i] = b.PutOwned(ctx, "contested/key", []byte(app), secret.Ownership{App: app})
		})
	}
	wg.Wait()

	owner, err := b.Owner(ctx, "contested/key")
	require.NoError(t, err)

	got, err := b.Resolve(ctx, "contested/key")
	require.NoError(t, err)
	assert.Equal(t, owner, string(got.Bytes), "the current value was written by the owner")

	for i, app := range apps {
		if app == owner {
			assert.NoError(t, errs[i])
		} else {
			assert.ErrorIs(t, errs[i], secret.ErrOwnedByOtherApp)
		}
	}
}
