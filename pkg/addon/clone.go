package addon

import (
	"context"
	"crypto/sha256"
	"fmt"
	"slices"

	"miren.dev/runtime/api/addon/addon_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/entity"
)

// RequestClones creates one version-scoped association for each selected primary
// association on an app. It is idempotent so a resumed deploy does not create
// a second clone of the same addon.
func RequestClones(ctx context.Context, eac *entityserver_v1alpha.EntityAccessClient, appID, versionID entity.Id, spec core_v1alpha.ConfigSpec) error {
	names := spec.CloneAddons
	if len(names) == 0 {
		return nil
	}
	existing, err := eac.List(ctx, entity.Ref(addon_v1alpha.AddonAssociationAppVersionId, versionID))
	if err != nil {
		return fmt.Errorf("listing existing addon clones: %w", err)
	}
	cloned := make(map[entity.Id]bool, len(existing.Values()))
	for _, ent := range existing.Values() {
		var assoc addon_v1alpha.AddonAssociation
		assoc.Decode(ent.Entity())
		cloned[assoc.SourceAssociation] = true
	}

	sources, err := eac.List(ctx, entity.Ref(addon_v1alpha.AddonAssociationAppId, appID))
	if err != nil {
		return fmt.Errorf("listing addon clone sources: %w", err)
	}
	for _, ent := range sources.Values() {
		var source addon_v1alpha.AddonAssociation
		source.Decode(ent.Entity())
		if source.AppVersion != "" || !slices.Contains(names, NameFromRef(source.Addon)) {
			continue
		}
		if source.Status != "active" {
			return fmt.Errorf("addon association %s is not ready to clone (status %s)", source.ID, source.Status)
		}
		if cloned[source.ID] {
			continue
		}
		assoc := &addon_v1alpha.AddonAssociation{
			App:               appID,
			AppVersion:        versionID,
			SourceAssociation: source.ID,
			Addon:             source.Addon,
			Variant:           source.Variant,
			Version:           source.Version,
			Services:          source.Services,
			Status:            "pending",
		}
		for _, override := range spec.CloneAddonVariants {
			if override.Name == NameFromRef(source.Addon) && override.Variant != "" {
				assoc.Variant = override.Variant
				break
			}
		}
		sum := sha256.Sum256([]byte(source.ID + "\x00" + versionID))
		id := entity.Id(fmt.Sprintf("addon_association/clone-%x", sum[:12]))
		if _, err := eac.Create(ctx, entity.New(entity.DBId, id, assoc.Encode).Attrs()); err != nil {
			return fmt.Errorf("requesting clone of addon association %s: %w", source.ID, err)
		}
	}
	return nil
}
