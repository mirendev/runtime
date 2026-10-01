package ephemeral

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"miren.dev/runtime/api/addon/addon_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	testutils "miren.dev/runtime/pkg/entity/testutils"
)

func TestLookupByLabel(t *testing.T) {
	ctx := context.Background()
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	// Create an app
	app := &core_v1alpha.App{}
	appID, err := inmem.Client.Create(ctx, "myapp", app)
	require.NoError(t, err)

	// Create a non-ephemeral version
	normalVersion := &core_v1alpha.AppVersion{
		App:     appID,
		Version: "myapp-v1",
	}
	_, err = inmem.Client.Create(ctx, "myapp-v1", normalVersion)
	require.NoError(t, err)

	// Create an ephemeral version
	ephVersion := &core_v1alpha.AppVersion{
		App:                appID,
		Version:            "myapp-v2",
		EphemeralLabel:     "feat-login",
		EphemeralTtl:       "24h",
		EphemeralExpiresAt: time.Now().Add(24 * time.Hour),
	}
	_, err = inmem.Client.Create(ctx, "myapp-v2", ephVersion)
	require.NoError(t, err)

	t.Run("finds ephemeral version by label", func(t *testing.T) {
		found, err := LookupByLabel(ctx, inmem.EAC, appID, "feat-login")
		require.NoError(t, err)
		require.NotNil(t, found)
		require.Equal(t, "myapp-v2", found.Version)
		require.Equal(t, "feat-login", found.EphemeralLabel)
	})

	t.Run("returns nil for non-existent label", func(t *testing.T) {
		found, err := LookupByLabel(ctx, inmem.EAC, appID, "no-such-label")
		require.NoError(t, err)
		require.Nil(t, found)
	})

	t.Run("scoped to app", func(t *testing.T) {
		// Create another app with the same label
		otherApp := &core_v1alpha.App{}
		otherAppID, err := inmem.Client.Create(ctx, "otherapp", otherApp)
		require.NoError(t, err)

		otherVersion := &core_v1alpha.AppVersion{
			App:            otherAppID,
			Version:        "otherapp-v1",
			EphemeralLabel: "feat-login",
			EphemeralTtl:   "1h",
		}
		_, err = inmem.Client.Create(ctx, "otherapp-v1", otherVersion)
		require.NoError(t, err)

		// Lookup should return the correct app's version
		found, err := LookupByLabel(ctx, inmem.EAC, appID, "feat-login")
		require.NoError(t, err)
		require.NotNil(t, found)
		require.Equal(t, "myapp-v2", found.Version)

		// And the other app's version for the other app
		found2, err := LookupByLabel(ctx, inmem.EAC, otherAppID, "feat-login")
		require.NoError(t, err)
		require.NotNil(t, found2)
		require.Equal(t, "otherapp-v1", found2.Version)
	})

	t.Run("returns nil for expired version", func(t *testing.T) {
		expiredVersion := &core_v1alpha.AppVersion{
			App:                appID,
			Version:            "myapp-expired",
			EphemeralLabel:     "old-branch",
			EphemeralTtl:       "1h",
			EphemeralExpiresAt: time.Now().Add(-10 * time.Minute),
		}
		_, err := inmem.Client.Create(ctx, "myapp-expired", expiredVersion)
		require.NoError(t, err)

		found, err := LookupByLabel(ctx, inmem.EAC, appID, "old-branch")
		require.NoError(t, err)
		require.Nil(t, found, "expired version should not be returned")
	})
}

func TestReplaceExisting(t *testing.T) {
	ctx := context.Background()
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()
	log := testutils.TestLogger(t)

	app := &core_v1alpha.App{}
	appID, err := inmem.Client.Create(ctx, "myapp", app)
	require.NoError(t, err)

	t.Run("deletes existing version with same label", func(t *testing.T) {
		// Create an ephemeral version
		v := &core_v1alpha.AppVersion{
			App:            appID,
			Version:        "myapp-eph1",
			EphemeralLabel: "feat-x",
			EphemeralTtl:   "24h",
		}
		versionID, err := inmem.Client.Create(ctx, "myapp-eph1", v)
		require.NoError(t, err)
		cloneID, err := inmem.Client.Create(ctx, "myapp-eph1-postgresql", &addon_v1alpha.AddonAssociation{
			App: appID, AppVersion: versionID, Addon: "addon/miren-postgresql", Status: "active",
		})
		require.NoError(t, err)

		// Verify it exists
		found, err := LookupByLabel(ctx, inmem.EAC, appID, "feat-x")
		require.NoError(t, err)
		require.NotNil(t, found)

		// Replace it
		err = ReplaceExisting(ctx, inmem.EAC, appID, "feat-x", log)
		require.NoError(t, err)

		// Verify it's gone
		found, err = LookupByLabel(ctx, inmem.EAC, appID, "feat-x")
		require.NoError(t, err)
		require.Nil(t, found)

		var clone addon_v1alpha.AddonAssociation
		require.NoError(t, inmem.Client.GetById(ctx, cloneID, &clone))
		require.Equal(t, "deprovisioning", clone.Status)
	})

	t.Run("no-op when label does not exist", func(t *testing.T) {
		err := ReplaceExisting(ctx, inmem.EAC, appID, "nonexistent", log)
		require.NoError(t, err)
	})

	t.Run("does not affect other labels", func(t *testing.T) {
		v1 := &core_v1alpha.AppVersion{
			App:            appID,
			Version:        "myapp-keep",
			EphemeralLabel: "keep-me",
			EphemeralTtl:   "24h",
		}
		_, err := inmem.Client.Create(ctx, "myapp-keep", v1)
		require.NoError(t, err)

		v2 := &core_v1alpha.AppVersion{
			App:            appID,
			Version:        "myapp-delete",
			EphemeralLabel: "delete-me",
			EphemeralTtl:   "24h",
		}
		_, err = inmem.Client.Create(ctx, "myapp-delete", v2)
		require.NoError(t, err)

		// Replace only "delete-me"
		err = ReplaceExisting(ctx, inmem.EAC, appID, "delete-me", log)
		require.NoError(t, err)

		// "keep-me" still exists
		found, err := LookupByLabel(ctx, inmem.EAC, appID, "keep-me")
		require.NoError(t, err)
		require.NotNil(t, found)

		// "delete-me" is gone
		found, err = LookupByLabel(ctx, inmem.EAC, appID, "delete-me")
		require.NoError(t, err)
		require.Nil(t, found)
	})
}

func TestEnforceLimit(t *testing.T) {
	ctx := context.Background()
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()
	log := testutils.TestLogger(t)

	app := &core_v1alpha.App{}
	appID, err := inmem.Client.Create(ctx, "myapp", app)
	require.NoError(t, err)

	t.Run("no-op when under limit", func(t *testing.T) {
		v := &core_v1alpha.AppVersion{
			App:                appID,
			Version:            "myapp-under",
			EphemeralLabel:     "under-limit",
			EphemeralTtl:       "24h",
			EphemeralExpiresAt: time.Now().Add(24 * time.Hour),
		}
		_, err := inmem.Client.Create(ctx, "myapp-under", v)
		require.NoError(t, err)

		err = EnforceLimit(ctx, inmem.EAC, appID, 10, log)
		require.NoError(t, err)

		// Version still exists
		found, err := LookupByLabel(ctx, inmem.EAC, appID, "under-limit")
		require.NoError(t, err)
		require.NotNil(t, found)
	})

	t.Run("evicts nearest-to-expiry when at limit", func(t *testing.T) {
		// Clean up from previous subtest
		ReplaceExisting(ctx, inmem.EAC, appID, "under-limit", log)

		now := time.Now()

		// Create 3 ephemeral versions with different expiry times
		versions := []struct {
			name    string
			label   string
			expires time.Duration
		}{
			{"myapp-soon", "expires-soon", 1 * time.Hour},
			{"myapp-mid", "expires-mid", 12 * time.Hour},
			{"myapp-late", "expires-late", 24 * time.Hour},
		}

		for _, v := range versions {
			av := &core_v1alpha.AppVersion{
				App:                appID,
				Version:            v.name,
				EphemeralLabel:     v.label,
				EphemeralTtl:       v.expires.String(),
				EphemeralExpiresAt: now.Add(v.expires),
			}
			_, err := inmem.Client.Create(ctx, v.name, av)
			require.NoError(t, err)
		}

		// Enforce limit of 3 (should evict 1 to make room for a new one)
		err := EnforceLimit(ctx, inmem.EAC, appID, 3, log)
		require.NoError(t, err)

		// The nearest-to-expiry ("expires-soon") should be evicted
		found, err := LookupByLabel(ctx, inmem.EAC, appID, "expires-soon")
		require.NoError(t, err)
		require.Nil(t, found, "nearest-to-expiry version should have been evicted")

		// Others remain
		found, err = LookupByLabel(ctx, inmem.EAC, appID, "expires-mid")
		require.NoError(t, err)
		require.NotNil(t, found)

		found, err = LookupByLabel(ctx, inmem.EAC, appID, "expires-late")
		require.NoError(t, err)
		require.NotNil(t, found)
	})
}

func TestDeleteExpired(t *testing.T) {
	ctx := context.Background()
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()
	log := testutils.TestLogger(t)

	app := &core_v1alpha.App{}
	appID, err := inmem.Client.Create(ctx, "myapp", app)
	require.NoError(t, err)

	t.Run("deletes expired versions only", func(t *testing.T) {
		// Create an expired version
		expired := &core_v1alpha.AppVersion{
			App:                appID,
			Version:            "myapp-expired",
			EphemeralLabel:     "old-branch",
			EphemeralTtl:       "1h",
			EphemeralExpiresAt: time.Now().Add(-1 * time.Hour),
		}
		_, err := inmem.Client.Create(ctx, "myapp-expired", expired)
		require.NoError(t, err)

		// Create a non-expired version
		active := &core_v1alpha.AppVersion{
			App:                appID,
			Version:            "myapp-active",
			EphemeralLabel:     "active-branch",
			EphemeralTtl:       "48h",
			EphemeralExpiresAt: time.Now().Add(47 * time.Hour),
		}
		_, err = inmem.Client.Create(ctx, "myapp-active", active)
		require.NoError(t, err)

		deleted, err := DeleteExpired(ctx, inmem.EAC, appID, log)
		require.NoError(t, err)
		require.Equal(t, 1, deleted)

		// Expired version gone
		found, err := LookupByLabel(ctx, inmem.EAC, appID, "old-branch")
		require.NoError(t, err)
		require.Nil(t, found)

		// Active version remains
		found, err = LookupByLabel(ctx, inmem.EAC, appID, "active-branch")
		require.NoError(t, err)
		require.NotNil(t, found)
	})

	t.Run("returns zero when nothing expired", func(t *testing.T) {
		// Only the "active-branch" version remains from previous subtest
		deleted, err := DeleteExpired(ctx, inmem.EAC, appID, log)
		require.NoError(t, err)
		require.Equal(t, 0, deleted)
	})
}

func TestListEphemeralVersions(t *testing.T) {
	ctx := context.Background()
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	app := &core_v1alpha.App{}
	appID, err := inmem.Client.Create(ctx, "myapp", app)
	require.NoError(t, err)

	// Create a mix of ephemeral and non-ephemeral versions
	normal := &core_v1alpha.AppVersion{
		App:     appID,
		Version: "myapp-v1",
	}
	_, err = inmem.Client.Create(ctx, "myapp-v1", normal)
	require.NoError(t, err)

	eph1 := &core_v1alpha.AppVersion{
		App:            appID,
		Version:        "myapp-eph1",
		EphemeralLabel: "feat-a",
		EphemeralTtl:   "24h",
	}
	_, err = inmem.Client.Create(ctx, "myapp-eph1", eph1)
	require.NoError(t, err)

	eph2 := &core_v1alpha.AppVersion{
		App:            appID,
		Version:        "myapp-eph2",
		EphemeralLabel: "feat-b",
		EphemeralTtl:   "48h",
	}
	_, err = inmem.Client.Create(ctx, "myapp-eph2", eph2)
	require.NoError(t, err)

	versions, err := listEphemeralVersions(ctx, inmem.EAC, appID)
	require.NoError(t, err)
	require.Len(t, versions, 2)

	labels := make(map[string]bool)
	for _, v := range versions {
		labels[v.version.EphemeralLabel] = true
	}
	require.True(t, labels["feat-a"])
	require.True(t, labels["feat-b"])
}
