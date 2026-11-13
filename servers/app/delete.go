package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"miren.dev/runtime/api/addon/addon_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/api/storage/storage_v1alpha"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/entity"
)

const AppRefTag = "dev.miren.app_ref"

// DeleteAppTransitive deletes an app and all entities that directly reference it.
// This includes app_versions and sandbox_pools (both tagged with dev.miren.app_ref).
// Other transitive resources (sandboxes referencing app_versions) are cleaned up by their controllers.
//
// Artifacts are the exception among app_ref entities: they are left alone.
// Artifacts are deduplicated by manifest digest, so one tagged with this app
// may still back another app's version, and the registry resolves manifests
// through the artifact entity. The artifact GC archives whatever is left
// unreferenced once the versions are gone, and archiving is what lets the
// image and blob GCs reclaim the storage.
func DeleteAppTransitive(ctx context.Context, client *entityserver.Client, log *slog.Logger, appId entity.Id) error {
	log.Info("starting app deletion", "appId", appId)

	// Clear ActiveVersion first to prevent the DeploymentLauncher from recreating
	// pools during the deletion window. Without this, there's a race condition:
	// 1. We delete pools
	// 2. Launcher sees app has ActiveVersion but no pools
	// 3. Launcher creates new pools
	// 4. We delete the app
	// 5. New pools are orphaned and keep trying to launch sandboxes
	if err := client.Patch(ctx, appId, 0,
		entity.Ref(core_v1alpha.AppActiveVersionId, entity.Id("")),
		entity.Ref(core_v1alpha.AppActiveDeploymentId, entity.Id("")),
	); err != nil {
		log.Warn("failed to clear active version (app may already be deleted)", "appId", appId, "error", err)
		// Continue anyway - the app might already be partially deleted
	}

	// Trigger addon deprovisioning before deleting entities.
	// We set status to "deprovisioning" rather than deleting directly
	// because the addon controller needs the entity data to call
	// provider.Deprovision() (delete watch events don't include entity data).
	addonAssocIDs := make(map[entity.Id]bool)
	assocList, err := client.List(ctx, entity.Ref(addon_v1alpha.AddonAssociationAppId, appId))
	if err != nil {
		return fmt.Errorf("listing addon associations for app %s: %w", appId, err)
	}
	for assocList.Next() {
		var assoc addon_v1alpha.AddonAssociation
		if err := assocList.Read(&assoc); err != nil {
			continue
		}
		if assoc.ID != "" {
			addonAssocIDs[assoc.ID] = true
			if err := client.Patch(ctx, assoc.ID, 0,
				entity.String(addon_v1alpha.AddonAssociationStatusId, "deprovisioning")); err != nil {
				log.Warn("failed to set addon association to deprovisioning", "id", assoc.ID, "error", err)
			} else {
				log.Info("triggered addon deprovisioning", "associationId", assoc.ID)
			}
		}
	}

	// Find all the attributes that reference apps by id
	appRefResult, err := client.GetAttributesByTag(ctx, AppRefTag)
	if err != nil {
		return fmt.Errorf("failed to get app references: %w", err)
	}

	var referencingEntities []entity.Id
	disks := map[entity.Id]bool{}
	secrets := map[entity.Id]bool{}

	for _, schema := range appRefResult.Schemas() {
		if !schema.Indexed() {
			continue
		}

		attrId := entity.Id(schema.Id())
		if attrId == core_v1alpha.ArtifactAppId {
			continue
		}

		list, err := client.List(ctx, entity.Ref(attrId, appId))
		if err != nil {
			// Owned secrets must not survive the app: its id is reused by the
			// next app created under the same name, which would inherit them.
			// Everything else stays best-effort, as it always has been.
			if attrId == core_v1alpha.SecretAppId {
				return fmt.Errorf("listing secrets owned by app %s: %w", appId, err)
			}
			log.Warn("failed to list entities", "attr", attrId, "error", err)
			continue
		}

		for list.Next() {
			if ent := list.Entity(); ent != nil {
				if id := ent.Id(); id != "" {
					referencingEntities = append(referencingEntities, id)
					disks[id] = entity.Is(ent, storage_v1alpha.KindDisk)
					secrets[id] = entity.Is(ent, core_v1alpha.KindSecret)
				}
			}
		}
	}

	log.Info("found entities referencing app",
		"total", len(referencingEntities))

	// Delete all referencing entities (app_versions, pools, etc.)
	// Skip addon associations — they'll be deleted by the addon controller after deprovisioning.
	// Tolerate not-found errors since addon deprovisioning may have already
	// cleaned up some entities (e.g. pools created for addon infrastructure).
	for _, id := range referencingEntities {
		if addonAssocIDs[id] {
			continue
		}
		if disks[id] {
			// A disk the app created is deleted the way every disk is: the
			// disk controller soft-deletes its volume, so it stays listed and
			// undeletable for the retention period. Removing the entity here
			// would orphan the volume where nothing can find it.
			log.Info("marking app disk for deletion", "id", id)
			if err := client.Patch(ctx, id, 0, entity.Ref(storage_v1alpha.DiskStatusId, storage_v1alpha.DiskStatusDeletingId)); err != nil && !errors.Is(err, cond.ErrNotFound{}) {
				return fmt.Errorf("failed to mark disk %s for deletion: %w", id, err)
			}
			continue
		}
		if secrets[id] {
			// A secret the app owns goes with it. App ids are derived from the
			// app's name, so a secret left behind would belong to whatever app
			// is next created under that name. Its versions point at the
			// secret, not the app, so they are deleted here first; nothing else
			// would ever find them.
			if err := deleteSecretVersions(ctx, client, id); err != nil {
				return err
			}
		}
		log.Info("deleting entity", "id", id)
		if err := client.Delete(ctx, id); err != nil {
			if errors.Is(err, cond.ErrNotFound{}) {
				log.Info("entity already deleted", "id", id)
				continue
			}
			return fmt.Errorf("failed to delete entity %s: %w", id, err)
		}
	}

	// Delete the app
	log.Info("deleting app", "id", appId)
	if err := client.Delete(ctx, appId); err != nil {
		return fmt.Errorf("failed to delete app: %w", err)
	}

	log.Info("app deletion complete",
		"appId", appId,
		"deletedEntities", len(referencingEntities))

	return nil
}

// deleteSecretVersions deletes every stored version of a secret.
func deleteSecretVersions(ctx context.Context, client *entityserver.Client, secretID entity.Id) error {
	list, err := client.List(ctx, entity.Ref(core_v1alpha.SecretVersionSecretId, secretID))
	if err != nil {
		return fmt.Errorf("listing versions of secret %s: %w", secretID, err)
	}

	var versions []entity.Id
	for list.Next() {
		if ent := list.Entity(); ent != nil && ent.Id() != "" {
			versions = append(versions, entity.Id(ent.Id()))
		}
	}

	for _, id := range versions {
		if err := client.Delete(ctx, id); err != nil && !errors.Is(err, cond.ErrNotFound{}) {
			return fmt.Errorf("failed to delete secret version %s: %w", id, err)
		}
	}
	return nil
}
