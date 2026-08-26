package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"miren.dev/runtime/api/addon/addon_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/entity"
)

const AppRefTag = "dev.miren.app_ref"

// DeleteAppTransitive deletes an app and all entities that directly reference it.
// This includes app_versions and sandbox_pools (both tagged with dev.miren.app_ref).
// Other transitive resources (sandboxes referencing app_versions) are cleaned up by their controllers.
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

	for _, schema := range appRefResult.Schemas() {
		if !schema.Indexed() {
			continue
		}

		attrId := entity.Id(schema.Id())

		list, err := client.List(ctx, entity.Ref(attrId, appId))
		if err != nil {
			log.Warn("failed to list entities", "attr", attrId, "error", err)
			continue
		}

		for list.Next() {
			if ent := list.Entity(); ent != nil {
				if id := ent.Id(); id != "" {
					referencingEntities = append(referencingEntities, id)
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
