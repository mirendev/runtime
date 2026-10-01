// Package appversion holds shared helpers for managing AppVersion entities and
// the resources that hang off them. It is consumed by both the ephemeral GC
// (TTL expiry) and the version retention GC (count/age pruning) so the deletion
// cascade lives in exactly one place.
package appversion

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"miren.dev/runtime/api/addon/addon_v1alpha"
	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/entity"
)

// Delete hard-deletes an AppVersion and its 1:1 ConfigVersion.
//
// It deliberately does NOT touch sandbox pools. The sandbox pool Manager owns
// pool lifecycle: on deploy the launcher de-refs and scales superseded pools to
// zero, and the Manager's sweep reaps drained pools that no current version
// references (see controllers/sandboxpool). By the time a version is old enough
// to prune, its pools are already gone. Callers that need immediate pool
// teardown rather than waiting for that sweep should use DeleteWithPools.
//
// ConfigVersion cleanup is best-effort: an orphaned ConfigVersion holds no
// blobs, so a failure there is logged but does not block deleting the version.
func Delete(ctx context.Context, eac *entityserver_v1alpha.EntityAccessClient, version *core_v1alpha.AppVersion, log *slog.Logger) error {
	// Delete the version first. If we cleaned up the ConfigVersion first and
	// then the version delete failed, the surviving version would point at a
	// missing config; ordering it this way means the best-effort cleanup only
	// runs once the parent is actually gone.
	if _, err := eac.Delete(ctx, version.ID.String()); err != nil {
		return fmt.Errorf("failed to delete app version %s: %w", version.ID, err)
	}

	if version.ConfigVersion != "" {
		if _, err := eac.Delete(ctx, version.ConfigVersion.String()); err != nil {
			log.Warn("failed to delete config version for app version",
				"version_id", version.ID, "config_version", version.ConfigVersion, "error", err)
		}
	}

	log.Info("deleted app version", "version_id", version.ID)
	return nil
}

// DeleteWithPools tears down the version's sandbox pools before deleting the
// version, for callers (e.g. ephemeral TTL expiry) that want prompt teardown
// instead of waiting for the pool Manager's idle sweep. Pools referenced only
// by this version are deleted; pools shared with other versions keep their
// remaining references.
//
// Pool cleanup runs first. If it fails, the AppVersion is retained so a future
// pass can retry — otherwise we would leak pools that can no longer be traced
// back to any version.
func DeleteWithPools(ctx context.Context, eac *entityserver_v1alpha.EntityAccessClient, version *core_v1alpha.AppVersion, log *slog.Logger) error {
	if err := cleanupAddonClones(ctx, eac, version.ID); err != nil {
		return err
	}
	if err := cleanupSandboxPools(ctx, eac, version.ID, log); err != nil {
		return err
	}
	return Delete(ctx, eac, version, log)
}

func cleanupAddonClones(ctx context.Context, eac *entityserver_v1alpha.EntityAccessClient, versionID entity.Id) error {
	resp, err := eac.List(ctx, entity.Ref(addon_v1alpha.AddonAssociationAppVersionId, versionID))
	if err != nil {
		return fmt.Errorf("failed to list addon clones for app version %s: %w", versionID, err)
	}
	if len(resp.Values()) == 0 {
		return nil
	}
	for _, ent := range resp.Values() {
		var assoc addon_v1alpha.AddonAssociation
		assoc.Decode(ent.Entity())
		if assoc.Status == "error" {
			if _, err := eac.Delete(ctx, assoc.ID.String()); err != nil {
				return fmt.Errorf("deleting failed addon clone %s: %w", assoc.ID, err)
			}
			continue
		}
		if assoc.Status != "deprovisioning" {
			if _, err := eac.Patch(ctx, []entity.Attr{
				entity.Ref(entity.DBId, assoc.ID),
				entity.String(addon_v1alpha.AddonAssociationStatusId, "deprovisioning"),
			}, 0); err != nil {
				return fmt.Errorf("requesting teardown of addon clone %s: %w", assoc.ID, err)
			}
		}
	}
	return nil
}

// cleanupSandboxPools removes the sandbox pools that reference the given
// version. Pools owned exclusively by this version are deleted; pools shared
// across versions have just this version's reference removed. Any failure is
// returned so the caller can retain the version for a later retry.
func cleanupSandboxPools(ctx context.Context, eac *entityserver_v1alpha.EntityAccessClient, versionID entity.Id, log *slog.Logger) error {
	poolResp, err := eac.List(ctx, entity.Ref(
		compute_v1alpha.SandboxPoolReferencedByVersionsId,
		versionID,
	))
	if err != nil {
		return fmt.Errorf("failed to list pools for app version %s: %w", versionID, err)
	}

	var poolErrs []error
	for _, p := range poolResp.Values() {
		poolID := p.Id()

		var pool compute_v1alpha.SandboxPool
		pool.Decode(p.Entity())

		if len(pool.ReferencedByVersions) > 1 {
			// Shared pool — remove our reference instead of deleting.
			var remaining []entity.Id
			for _, ref := range pool.ReferencedByVersions {
				if ref != versionID {
					remaining = append(remaining, ref)
				}
			}
			pool.ReferencedByVersions = remaining

			updateEntity := &entityserver_v1alpha.Entity{}
			updateEntity.SetId(poolID)
			updateEntity.SetAttrs(pool.Encode())
			updateEntity.SetRevision(p.Revision())
			if _, err := eac.Put(ctx, updateEntity); err != nil {
				log.Warn("failed to remove version reference from shared pool", "pool_id", poolID, "error", err)
				poolErrs = append(poolErrs, fmt.Errorf("pool %s: %w", poolID, err))
			} else {
				log.Debug("removed version reference from shared pool", "pool_id", poolID)
			}
			continue
		}

		// Exclusively owned — delete the pool.
		if _, err := eac.Delete(ctx, poolID); err != nil {
			log.Warn("failed to delete sandbox pool", "pool_id", poolID, "error", err)
			poolErrs = append(poolErrs, fmt.Errorf("pool %s: %w", poolID, err))
		} else {
			log.Debug("deleted sandbox pool for app version", "pool_id", poolID)
		}
	}

	if len(poolErrs) > 0 {
		return fmt.Errorf("failed to delete %d sandbox pool(s) for app version %s; retaining version for retry: %w",
			len(poolErrs), versionID, errors.Join(poolErrs...))
	}

	return nil
}
