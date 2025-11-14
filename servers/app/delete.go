package app

import (
	"context"
	"fmt"
	"log/slog"

	"miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/pkg/entity"
)

// DeleteAppTransitive deletes an app and all entities that directly reference it.
// This includes app_versions and sandbox_pools (both tagged with dev.miren.app_ref).
// Other transitive resources (sandboxes referencing app_versions) are cleaned up by their controllers.
func DeleteAppTransitive(ctx context.Context, client *entityserver.Client, log *slog.Logger, appId entity.Id) error {
	log.Info("starting app deletion", "appId", appId)

	// Find all entities that reference this app (tagged with dev.miren.app_ref)
	appRefResult, err := client.GetAttributesByTag(ctx, "dev.miren.app_ref")
	if err != nil {
		return fmt.Errorf("failed to get app references: %w", err)
	}

	var referencingEntities []entity.Id
	var appVersionIds []entity.Id

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

					// Track app_versions separately for logging
					if attrId == "dev.miren.core/app_version.app" {
						appVersionIds = append(appVersionIds, id)
					}
				}
			}
		}
	}

	log.Info("found entities referencing app",
		"total", len(referencingEntities),
		"appVersions", len(appVersionIds))

	// Delete all referencing entities (app_versions, pools, etc.)
	for _, id := range referencingEntities {
		log.Info("deleting entity", "id", id)
		if err := client.Delete(ctx, id); err != nil {
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
