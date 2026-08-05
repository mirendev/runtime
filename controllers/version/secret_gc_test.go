package version

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/testutils"
	"miren.dev/runtime/pkg/entity/types"
	"miren.dev/runtime/pkg/secret"
)

// aged is safely outside the retention window, so a version created at this
// time is judged on references alone.
func aged() time.Time {
	return time.Now().Add(-2 * secretVersionRetention)
}

func createSecretVersion(t *testing.T, eac *entityserver_v1alpha.EntityAccessClient,
	name string, sv *core_v1alpha.SecretVersion, createdAt time.Time) entity.Id {
	t.Helper()

	ent := entity.New(
		(&core_v1alpha.Metadata{Name: name}).Encode,
		sv.Encode,
		entity.Ident, types.Keyword(sv.ShortKind()+"/"+name),
	)
	ent.SetCreatedAt(createdAt)

	var rpcE entityserver_v1alpha.Entity
	rpcE.SetAttrs(ent.Attrs())
	pr, err := eac.Put(context.Background(), &rpcE)
	require.NoError(t, err)
	return entity.Id(pr.Id())
}

// shortIDOf reads the handle a reference names a version by.
func shortIDOf(t *testing.T, eac *entityserver_v1alpha.EntityAccessClient, id entity.Id) string {
	t.Helper()

	res, err := eac.Get(context.Background(), id.String())
	require.NoError(t, err)

	for _, attr := range res.Entity().Attrs() {
		if attr.ID == entity.DBShortId {
			return attr.Value.String()
		}
	}
	t.Fatalf("secret version %s has no short id", id)
	return ""
}

func newSecretGC(t *testing.T, eac *entityserver_v1alpha.EntityAccessClient) *GCController {
	return &GCController{Log: testutils.TestLogger(t), EAC: eac}
}

// The version a floating reference resolves to is never reapable, however old
// it is and whether or not anything pins it.
func TestSecretGCKeepsTheCurrentVersion(t *testing.T) {
	ctx := context.Background()
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	current := createSecretVersion(t, inmem.EAC, "sv-current",
		&core_v1alpha.SecretVersion{State: core_v1alpha.ENABLED}, aged())

	_, err := inmem.Client.Create(ctx, "payments.stripe-key", &core_v1alpha.Secret{
		Path:           "payments/stripe-key",
		CurrentVersion: current,
	})
	require.NoError(t, err)

	// Point the version back at its secret now that the secret exists.
	require.NoError(t, inmem.Client.UpdateAttrs(ctx, current,
		entity.Ref(core_v1alpha.SecretVersionSecretId, entity.Id("secret/payments.stripe-key"))))

	result, err := newSecretGC(t, inmem.EAC).RunSecretGC(ctx)
	require.NoError(t, err)

	assert.Equal(t, 0, result.DeletedVersions)
	assert.Equal(t, 1, result.RetainedCurrent)
}

// The load-bearing case: a superseded version that a live ConfigVersion still
// pins must survive, or a rollback would come up on the wrong secret.
func TestSecretGCKeepsAVersionPinnedByAConfigVersion(t *testing.T) {
	ctx := context.Background()
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	secretID := entity.Id("secret/payments.stripe-key")

	old := createSecretVersion(t, inmem.EAC, "sv-old",
		&core_v1alpha.SecretVersion{Secret: secretID, State: core_v1alpha.ENABLED}, aged())
	current := createSecretVersion(t, inmem.EAC, "sv-current",
		&core_v1alpha.SecretVersion{Secret: secretID, State: core_v1alpha.ENABLED}, aged())

	_, err := inmem.Client.Create(ctx, "payments.stripe-key", &core_v1alpha.Secret{
		Path:           "payments/stripe-key",
		CurrentVersion: current,
	})
	require.NoError(t, err)

	// A past app version's config still names the old secret version.
	appID, err := inmem.Client.Create(ctx, "gcapp", &core_v1alpha.App{})
	require.NoError(t, err)
	_, err = inmem.Client.Create(ctx, "rolled-back-cfg", &core_v1alpha.ConfigVersion{
		App: appID,
		Spec: core_v1alpha.ConfigSpec{
			Variables: []core_v1alpha.ConfigSpecVariables{{
				Key:     "STRIPE_API_KEY",
				Backend: secret.ClusterBackendName,
				Value:   secret.FormatRef("payments/stripe-key", shortIDOf(t, inmem.EAC, old)),
			}},
		},
	})
	require.NoError(t, err)

	result, err := newSecretGC(t, inmem.EAC).RunSecretGC(ctx)
	require.NoError(t, err)

	assert.Equal(t, 0, result.DeletedVersions)
	assert.Equal(t, 1, result.RetainedPinned)
	assert.Equal(t, 1, result.RetainedCurrent)

	// It is still readable, which is the point.
	_, err = inmem.EAC.Get(ctx, old.String())
	assert.NoError(t, err)
}

func TestSecretGCReapsAnUnreferencedVersion(t *testing.T) {
	ctx := context.Background()
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	secretID := entity.Id("secret/payments.stripe-key")

	orphan := createSecretVersion(t, inmem.EAC, "sv-orphan",
		&core_v1alpha.SecretVersion{Secret: secretID, State: core_v1alpha.ENABLED}, aged())
	current := createSecretVersion(t, inmem.EAC, "sv-current",
		&core_v1alpha.SecretVersion{Secret: secretID, State: core_v1alpha.ENABLED}, aged())

	_, err := inmem.Client.Create(ctx, "payments.stripe-key", &core_v1alpha.Secret{
		Path:           "payments/stripe-key",
		CurrentVersion: current,
	})
	require.NoError(t, err)

	result, err := newSecretGC(t, inmem.EAC).RunSecretGC(ctx)
	require.NoError(t, err)

	assert.Equal(t, 1, result.DeletedVersions)
	assert.Equal(t, 1, result.RetainedCurrent)
	assert.Equal(t, 2, result.TotalScanned)

	_, err = inmem.EAC.Get(ctx, orphan.String())
	assert.Error(t, err, "the unreferenced version should be gone")
}

// A rotation is exactly when someone might need the old value back, so a
// recently superseded version is kept even with nothing referencing it.
func TestSecretGCKeepsARecentlySupersededVersion(t *testing.T) {
	ctx := context.Background()
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	secretID := entity.Id("secret/payments.stripe-key")

	createSecretVersion(t, inmem.EAC, "sv-recent",
		&core_v1alpha.SecretVersion{Secret: secretID, State: core_v1alpha.ENABLED}, time.Now())
	current := createSecretVersion(t, inmem.EAC, "sv-current",
		&core_v1alpha.SecretVersion{Secret: secretID, State: core_v1alpha.ENABLED}, time.Now())

	_, err := inmem.Client.Create(ctx, "payments.stripe-key", &core_v1alpha.Secret{
		Path:           "payments/stripe-key",
		CurrentVersion: current,
	})
	require.NoError(t, err)

	result, err := newSecretGC(t, inmem.EAC).RunSecretGC(ctx)
	require.NoError(t, err)

	assert.Equal(t, 0, result.DeletedVersions)
	assert.Equal(t, 1, result.RetainedRecent)
}

// A disabled version is still reapable once nothing references it — revoking is
// about resolution, not retention.
func TestSecretGCReapsADisabledUnreferencedVersion(t *testing.T) {
	ctx := context.Background()
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	secretID := entity.Id("secret/payments.stripe-key")

	createSecretVersion(t, inmem.EAC, "sv-revoked",
		&core_v1alpha.SecretVersion{Secret: secretID, State: core_v1alpha.DISABLED}, aged())
	current := createSecretVersion(t, inmem.EAC, "sv-current",
		&core_v1alpha.SecretVersion{Secret: secretID, State: core_v1alpha.ENABLED}, aged())

	_, err := inmem.Client.Create(ctx, "payments.stripe-key", &core_v1alpha.Secret{
		Path:           "payments/stripe-key",
		CurrentVersion: current,
	})
	require.NoError(t, err)

	result, err := newSecretGC(t, inmem.EAC).RunSecretGC(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, result.DeletedVersions)
}

// A cluster holding no secrets must sweep cleanly rather than erroring.
func TestSecretGCWithNoSecrets(t *testing.T) {
	ctx := context.Background()
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	result, err := newSecretGC(t, inmem.EAC).RunSecretGC(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, result.TotalScanned)
}

// The pin snapshot is taken once at the top of the sweep, so a ConfigVersion
// minted afterwards would otherwise lose its secret to a decision made before
// it existed. Unlike an app version, a reaped secret version cannot be rebuilt.
func TestSecretGCRecheckesPinsImmediatelyBeforeDeleting(t *testing.T) {
	ctx := context.Background()
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	secretID := entity.Id("secret/payments.stripe-key")

	orphan := createSecretVersion(t, inmem.EAC, "sv-orphan",
		&core_v1alpha.SecretVersion{Secret: secretID, State: core_v1alpha.ENABLED}, aged())
	current := createSecretVersion(t, inmem.EAC, "sv-current",
		&core_v1alpha.SecretVersion{Secret: secretID, State: core_v1alpha.ENABLED}, aged())

	_, err := inmem.Client.Create(ctx, "payments.stripe-key", &core_v1alpha.Secret{
		Path:           "payments/stripe-key",
		CurrentVersion: current,
	})
	require.NoError(t, err)

	gc := newSecretGC(t, inmem.EAC)

	// Nothing references it yet, so this version is squarely a delete candidate.
	ref := secret.FormatRef("payments/stripe-key", shortIDOf(t, inmem.EAC, orphan))
	stale, err := gc.pinnedSecretVersions(ctx)
	require.NoError(t, err)
	require.False(t, stale[ref], "precondition: the snapshot sees it as unreferenced")

	// A config lands pinning it, exactly as one could between snapshot and
	// delete.
	appID, err := inmem.Client.Create(ctx, "gcapp", &core_v1alpha.App{})
	require.NoError(t, err)
	_, err = inmem.Client.Create(ctx, "late-cfg", &core_v1alpha.ConfigVersion{
		App: appID,
		Spec: core_v1alpha.ConfigSpec{
			Variables: []core_v1alpha.ConfigSpecVariables{{
				Key:     "STRIPE_API_KEY",
				Backend: secret.ClusterBackendName,
				Value:   ref,
			}},
		},
	})
	require.NoError(t, err)

	// The re-check sees what the snapshot could not.
	nowPinned, err := gc.secretVersionPinnedNow(ctx, ref)
	require.NoError(t, err)
	assert.True(t, nowPinned, "the pre-delete re-check must see the newly minted pin")

	// And a full sweep now retains it rather than reaping it.
	result, err := gc.RunSecretGC(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, result.DeletedVersions)
	assert.Equal(t, 1, result.RetainedPinned)

	_, err = inmem.EAC.Get(ctx, orphan.String())
	assert.NoError(t, err, "the newly pinned version must still exist")
}
