package app

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/api/ingress/ingress_v1alpha"
	"miren.dev/runtime/api/storage/storage_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/testutils"
)

func TestDeleteAppTransitive(t *testing.T) {
	ctx := context.Background()

	// Setup in-memory entity server
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	client := entityserver.NewClient(slog.Default(), inmem.EAC)
	log := slog.Default()

	// Helper to check if entity exists
	entityExists := func(id entity.Id) bool {
		_, err := inmem.Store.GetEntity(ctx, id)
		return err == nil
	}

	t.Run("deletes app with no dependencies", func(t *testing.T) {
		// Create a simple app with no dependencies
		app := &core_v1alpha.App{}
		appID, err := client.Create(ctx, "simple-app", app)
		require.NoError(t, err)

		// Verify app exists
		require.True(t, entityExists(appID), "app should exist before deletion")

		// Delete the app
		err = DeleteAppTransitive(ctx, client, log, appID)
		require.NoError(t, err)

		// Verify app is deleted
		require.False(t, entityExists(appID), "app should be deleted")
	})

	t.Run("deletes app with app_version and artifact", func(t *testing.T) {
		// Create app
		app := &core_v1alpha.App{}
		appID, err := client.Create(ctx, "app-with-version", app)
		require.NoError(t, err)

		// Create app_version referencing the app
		appVersion := &core_v1alpha.AppVersion{
			App:     appID,
			Version: "v1.0.0",
		}
		versionID, err := client.Create(ctx, "app-with-version/v1.0.0", appVersion)
		require.NoError(t, err)

		// Create artifact referencing the app
		artifact := &core_v1alpha.Artifact{
			App: appID,
		}
		artifactID, err := client.Create(ctx, "app-with-version-artifact", artifact)
		require.NoError(t, err)

		// Verify all exist
		require.True(t, entityExists(appID), "app should exist")
		require.True(t, entityExists(versionID), "app_version should exist")
		require.True(t, entityExists(artifactID), "artifact should exist")

		// Delete the app
		err = DeleteAppTransitive(ctx, client, log, appID)
		require.NoError(t, err)

		// Verify app, app_version, and artifact are deleted
		require.False(t, entityExists(appID), "app should be deleted")
		require.False(t, entityExists(versionID), "app_version should be deleted")
		require.False(t, entityExists(artifactID), "artifact should be deleted (has dev.miren.app_ref tag)")
	})

	t.Run("deletes app with http_route", func(t *testing.T) {
		// Create app
		app := &core_v1alpha.App{}
		appID, err := client.Create(ctx, "app-with-route", app)
		require.NoError(t, err)

		// Create http_route referencing the app
		route := &ingress_v1alpha.HttpRoute{
			App:  appID,
			Host: "example.com",
		}
		routeID, err := client.Create(ctx, "app-with-route-route", route)
		require.NoError(t, err)

		// Verify both exist
		require.True(t, entityExists(appID), "app should exist")
		require.True(t, entityExists(routeID), "http_route should exist")

		// Delete the app
		err = DeleteAppTransitive(ctx, client, log, appID)
		require.NoError(t, err)

		// Verify app and route are deleted
		require.False(t, entityExists(appID), "app should be deleted")
		require.False(t, entityExists(routeID), "http_route should be deleted (has dev.miren.app_ref tag)")
	})

	t.Run("deletes app with disk_lease", func(t *testing.T) {
		// Create app
		app := &core_v1alpha.App{}
		appID, err := client.Create(ctx, "app-with-disk", app)
		require.NoError(t, err)

		// Create disk_lease referencing the app
		diskLease := &storage_v1alpha.DiskLease{
			AppId: appID,
		}
		leaseID, err := client.Create(ctx, "app-with-disk-lease", diskLease)
		require.NoError(t, err)

		// Verify both exist
		require.True(t, entityExists(appID), "app should exist")
		require.True(t, entityExists(leaseID), "disk_lease should exist")

		// Delete the app
		err = DeleteAppTransitive(ctx, client, log, appID)
		require.NoError(t, err)

		// Verify app and disk lease are deleted
		require.False(t, entityExists(appID), "app should be deleted")
		require.False(t, entityExists(leaseID), "disk_lease should be deleted (has dev.miren.app_ref tag)")
	})

	t.Run("deletes app with transitive dependencies (app -> app_version -> sandbox)", func(t *testing.T) {
		// Create app
		app := &core_v1alpha.App{}
		appID, err := client.Create(ctx, "app-with-sandbox", app)
		require.NoError(t, err)

		// Create app_version referencing the app
		appVersion := &core_v1alpha.AppVersion{
			App:     appID,
			Version: "v1.0.0",
		}
		versionID, err := client.Create(ctx, "app-with-sandbox/v1.0.0", appVersion)
		require.NoError(t, err)

		// Create sandbox referencing the app_version (via spec.version)
		sandbox := &compute_v1alpha.Sandbox{
			Spec: compute_v1alpha.SandboxSpec{
				Version: versionID,
			},
		}
		sandboxID, err := client.Create(ctx, "app-with-sandbox-sandbox", sandbox)
		require.NoError(t, err)

		// Verify all exist
		require.True(t, entityExists(appID), "app should exist")
		require.True(t, entityExists(versionID), "app_version should exist")
		require.True(t, entityExists(sandboxID), "sandbox should exist")

		// Delete the app
		err = DeleteAppTransitive(ctx, client, log, appID)
		require.NoError(t, err)

		// Verify app and app_version are deleted
		require.False(t, entityExists(versionID), "app_version should be deleted")
		require.False(t, entityExists(appID), "app should be deleted")
		// Sandbox is NOT deleted - it would be cleaned up by its controller
		require.True(t, entityExists(sandboxID), "sandbox should still exist (cleaned up by controller)")
	})

	t.Run("deletes app with sandbox_pool referencing app", func(t *testing.T) {
		// Create app
		app := &core_v1alpha.App{}
		appID, err := client.Create(ctx, "app-with-pool", app)
		require.NoError(t, err)

		// Create app_version referencing the app
		appVersion := &core_v1alpha.AppVersion{
			App:     appID,
			Version: "v1.0.0",
		}
		versionID, err := client.Create(ctx, "app-with-pool/v1.0.0", appVersion)
		require.NoError(t, err)

		// Create sandbox_pool referencing the app (via app field)
		pool := &compute_v1alpha.SandboxPool{
			App: appID,
			SandboxSpec: compute_v1alpha.SandboxSpec{
				Version: versionID,
			},
			ReferencedByVersions: []entity.Id{versionID},
		}
		poolID, err := client.Create(ctx, "app-with-pool-pool", pool)
		require.NoError(t, err)

		// Verify all exist
		require.True(t, entityExists(appID), "app should exist")
		require.True(t, entityExists(versionID), "app_version should exist")
		require.True(t, entityExists(poolID), "sandbox_pool should exist")

		// Delete the app
		err = DeleteAppTransitive(ctx, client, log, appID)
		require.NoError(t, err)

		// Verify app, app_version, and pool are all deleted
		require.False(t, entityExists(versionID), "app_version should be deleted")
		require.False(t, entityExists(poolID), "sandbox_pool should be deleted")
		require.False(t, entityExists(appID), "app should be deleted")
	})

	t.Run("deletes app with multiple app_versions and their dependencies", func(t *testing.T) {
		// Create app
		app := &core_v1alpha.App{}
		appID, err := client.Create(ctx, "app-multi-version", app)
		require.NoError(t, err)

		// Create multiple app_versions
		version1 := &core_v1alpha.AppVersion{
			App:     appID,
			Version: "v1.0.0",
		}
		version1ID, err := client.Create(ctx, "app-multi-version/v1.0.0", version1)
		require.NoError(t, err)

		version2 := &core_v1alpha.AppVersion{
			App:     appID,
			Version: "v2.0.0",
		}
		version2ID, err := client.Create(ctx, "app-multi-version/v2.0.0", version2)
		require.NoError(t, err)

		// Create sandboxes for each version
		sandbox1 := &compute_v1alpha.Sandbox{
			Spec: compute_v1alpha.SandboxSpec{
				Version: version1ID,
			},
		}
		sandbox1ID, err := client.Create(ctx, "app-multi-version-sandbox1", sandbox1)
		require.NoError(t, err)

		sandbox2 := &compute_v1alpha.Sandbox{
			Spec: compute_v1alpha.SandboxSpec{
				Version: version2ID,
			},
		}
		sandbox2ID, err := client.Create(ctx, "app-multi-version-sandbox2", sandbox2)
		require.NoError(t, err)

		// Create artifact and route for the app
		artifact := &core_v1alpha.Artifact{
			App: appID,
		}
		artifactID, err := client.Create(ctx, "app-multi-version-artifact", artifact)
		require.NoError(t, err)

		route := &ingress_v1alpha.HttpRoute{
			App:  appID,
			Host: "multi.example.com",
		}
		routeID, err := client.Create(ctx, "app-multi-version-route", route)
		require.NoError(t, err)

		// Verify all exist
		require.True(t, entityExists(appID), "app should exist")
		require.True(t, entityExists(version1ID), "app_version v1 should exist")
		require.True(t, entityExists(version2ID), "app_version v2 should exist")
		require.True(t, entityExists(sandbox1ID), "sandbox1 should exist")
		require.True(t, entityExists(sandbox2ID), "sandbox2 should exist")
		require.True(t, entityExists(artifactID), "artifact should exist")
		require.True(t, entityExists(routeID), "route should exist")

		// Delete the app
		err = DeleteAppTransitive(ctx, client, log, appID)
		require.NoError(t, err)

		// Verify app, app_versions, and entities with dev.miren.app_ref are deleted
		require.False(t, entityExists(version1ID), "app_version v1 should be deleted")
		require.False(t, entityExists(version2ID), "app_version v2 should be deleted")
		require.False(t, entityExists(artifactID), "artifact should be deleted (has dev.miren.app_ref tag)")
		require.False(t, entityExists(routeID), "route should be deleted (has dev.miren.app_ref tag)")
		require.False(t, entityExists(appID), "app should be deleted")
		// Sandboxes are NOT deleted - they reference app_versions, not apps directly
		require.True(t, entityExists(sandbox1ID), "sandbox1 should still exist (cleaned up by controller)")
		require.True(t, entityExists(sandbox2ID), "sandbox2 should still exist (cleaned up by controller)")
	})

	t.Run("handles app with active_version self-reference", func(t *testing.T) {
		// Create app
		app := &core_v1alpha.App{}
		appID, err := client.Create(ctx, "app-self-ref", app)
		require.NoError(t, err)

		// Create app_version
		appVersion := &core_v1alpha.AppVersion{
			App:     appID,
			Version: "v1.0.0",
		}
		versionID, err := client.Create(ctx, "app-self-ref/v1.0.0", appVersion)
		require.NoError(t, err)

		// Update app to reference the version as active_version
		app.ID = appID
		app.ActiveVersion = versionID
		err = client.Update(ctx, app)
		require.NoError(t, err)

		// Verify both exist and are linked
		require.True(t, entityExists(appID), "app should exist")
		require.True(t, entityExists(versionID), "app_version should exist")

		// Delete the app - should handle the circular reference
		err = DeleteAppTransitive(ctx, client, log, appID)
		require.NoError(t, err)

		// Verify both are deleted
		require.False(t, entityExists(versionID), "app_version should be deleted")
		require.False(t, entityExists(appID), "app should be deleted")
	})
}
