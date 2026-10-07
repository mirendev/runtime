package ephemeral

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/appversion"
	"miren.dev/runtime/pkg/entity"
)

const DefaultMaxEphemeral = 10

// ReplaceExisting finds and deletes an existing ephemeral version with the same
// label for the given app. This ensures deploying with an existing label replaces
// the old version. Associated sandbox pools are cleaned up via their
// referenced_by_versions index.
func ReplaceExisting(ctx context.Context, eac *entityserver_v1alpha.EntityAccessClient, appID entity.Id, label string, log *slog.Logger) error {
	versions, err := listEphemeralVersions(ctx, eac, appID)
	if err != nil {
		return err
	}

	for _, v := range versions {
		if v.version.EphemeralLabel == label {
			log.Info("replacing existing ephemeral version",
				"label", label, "version_id", v.version.ID)
			if err := appversion.DeleteWithPoolsAndWait(ctx, eac, v.version, log); err != nil {
				return fmt.Errorf("failed to delete existing ephemeral version %s: %w", v.version.ID, err)
			}
		}
	}
	return nil
}

// EnforceLimit ensures the number of ephemeral versions for an app does not
// exceed the given maximum. If at the limit, the version nearest to expiry is
// evicted to make room.
func EnforceLimit(ctx context.Context, eac *entityserver_v1alpha.EntityAccessClient, appID entity.Id, maxEphemeral int, log *slog.Logger) error {
	versions, err := listEphemeralVersions(ctx, eac, appID)
	if err != nil {
		return err
	}

	if len(versions) < maxEphemeral {
		return nil
	}

	// Sort by expires_at ascending (nearest to expiry first)
	sort.Slice(versions, func(i, j int) bool {
		return versions[i].version.EphemeralExpiresAt.Before(versions[j].version.EphemeralExpiresAt)
	})

	// Evict enough versions to make room for one new one
	toEvict := len(versions) - maxEphemeral + 1
	for i := 0; i < toEvict && i < len(versions); i++ {
		v := versions[i]
		log.Info("evicting ephemeral version (limit reached)",
			"label", v.version.EphemeralLabel,
			"version_id", v.version.ID,
			"expires_at", v.version.EphemeralExpiresAt)
		if err := appversion.DeleteWithPoolsAndWait(ctx, eac, v.version, log); err != nil {
			return fmt.Errorf("failed to evict ephemeral version %s: %w", v.version.ID, err)
		}
	}
	return nil
}

// DeleteExpired finds and deletes all ephemeral versions that have passed their
// expiration time for the given app.
func DeleteExpired(ctx context.Context, eac *entityserver_v1alpha.EntityAccessClient, appID entity.Id, log *slog.Logger) (int, error) {
	versions, err := listEphemeralVersions(ctx, eac, appID)
	if err != nil {
		return 0, err
	}

	now := time.Now()
	deleted := 0
	for _, v := range versions {
		if !v.version.EphemeralExpiresAt.IsZero() && now.After(v.version.EphemeralExpiresAt) {
			log.Info("deleting expired ephemeral version",
				"label", v.version.EphemeralLabel,
				"version_id", v.version.ID,
				"expired_at", v.version.EphemeralExpiresAt)
			if err := appversion.DeleteWithPools(ctx, eac, v.version, log); err != nil {
				log.Error("failed to delete expired ephemeral version",
					"version_id", v.version.ID, "error", err)
				continue
			}
			deleted++
		}
	}
	return deleted, nil
}

type ephemeralVersion struct {
	version *core_v1alpha.AppVersion
}

// listEphemeralVersions returns all ephemeral AppVersions for a given app.
func listEphemeralVersions(ctx context.Context, eac *entityserver_v1alpha.EntityAccessClient, appID entity.Id) ([]ephemeralVersion, error) {
	// List all AppVersions for this app
	resp, err := eac.List(ctx, entity.Ref(core_v1alpha.AppVersionAppId, appID))
	if err != nil {
		return nil, fmt.Errorf("failed to list app versions: %w", err)
	}

	var result []ephemeralVersion
	for _, e := range resp.Values() {
		var av core_v1alpha.AppVersion
		av.Decode(e.Entity())

		if av.EphemeralLabel != "" {
			result = append(result, ephemeralVersion{version: &av})
		}
	}
	return result, nil
}

// LookupByLabel finds an ephemeral AppVersion for the given app and label.
// Returns nil if no matching version exists or the version has expired.
func LookupByLabel(ctx context.Context, eac *entityserver_v1alpha.EntityAccessClient, appID entity.Id, label string) (*core_v1alpha.AppVersion, error) {
	resp, err := eac.List(ctx, entity.String(core_v1alpha.AppVersionEphemeralLabelId, label))
	if err != nil {
		return nil, fmt.Errorf("failed to lookup ephemeral version: %w", err)
	}

	now := time.Now()
	for _, e := range resp.Values() {
		var av core_v1alpha.AppVersion
		av.Decode(e.Entity())

		if av.App != appID {
			continue
		}

		// Skip expired versions — there may be a valid replacement later in the result set
		if !av.EphemeralExpiresAt.IsZero() && now.After(av.EphemeralExpiresAt) {
			continue
		}

		return &av, nil
	}
	return nil, nil
}
