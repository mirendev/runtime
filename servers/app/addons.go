package app

import (
	"context"
	"fmt"
	"log/slog"

	"miren.dev/runtime/api/addon/addon_v1alpha"
	"miren.dev/runtime/api/app/app_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/pkg/addon"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/idgen"
)

// AddonsServer implements the app_v1alpha.Addons RPC interface.
type AddonsServer struct {
	log          *slog.Logger
	ec           *entityserver.Client
	registry     *addon.Registry
	imageChecker addon.ImageChecker
}

var _ app_v1alpha.Addons = &AddonsServer{}

func NewAddonsServer(log *slog.Logger, ec *entityserver.Client, registry *addon.Registry, imageChecker addon.ImageChecker) *AddonsServer {
	return &AddonsServer{
		log:          log.With("module", "addons-rpc"),
		ec:           ec,
		registry:     registry,
		imageChecker: imageChecker,
	}
}

func (s *AddonsServer) CreateInstance(ctx context.Context, state *app_v1alpha.AddonsCreateInstance) error {
	args := state.Args()
	appName := args.App()
	addonSpec := args.Addon()
	variantOverride := args.Variant()
	version := args.Version()

	if appName == "" {
		return fmt.Errorf("app name is required")
	}
	if addonSpec == "" {
		return fmt.Errorf("addon name is required")
	}

	// Resolve addon and variant
	addonName, variantName, err := s.registry.ResolveAddonAndVariant(addonSpec)
	if err != nil {
		return err
	}
	if variantOverride != "" {
		if _, err := s.registry.GetVariantConfig(addonName, variantOverride, ""); err != nil {
			return fmt.Errorf("invalid variant override: %w", err)
		}
		variantName = variantOverride
	}

	// Resolve and validate the container image
	if s.imageChecker != nil {
		variantConfig, err := s.registry.GetVariantConfig(addonName, variantName, version)
		if err != nil {
			return fmt.Errorf("resolving image: %w", err)
		}
		image := variantConfig[addon.ConfigImage]
		if err := s.imageChecker.CheckImage(ctx, image); err != nil {
			return err
		}
	}

	// Look up the app entity
	var app core_v1alpha.App
	if err := s.ec.Get(ctx, appName, &app); err != nil {
		return fmt.Errorf("app %q not found: %w", appName, err)
	}

	// Look up the addon entity
	var addonEntity addon_v1alpha.Addon
	if err := s.ec.Get(ctx, addonName, &addonEntity); err != nil {
		return fmt.Errorf("addon %q not found: %w", addonName, err)
	}

	// Check for existing association (prevent duplicates)
	existing, err := s.ec.List(ctx, entity.Ref(addon_v1alpha.AddonAssociationAppId, app.ID))
	if err != nil {
		return fmt.Errorf("listing existing associations: %w", err)
	}
	for existing.Next() {
		var assoc addon_v1alpha.AddonAssociation
		if err := existing.Read(&assoc); err != nil {
			return fmt.Errorf("reading addon association: %w", err)
		}
		if assoc.Addon == addonEntity.ID {
			return fmt.Errorf("addon %q is already attached to app %q", addonName, appName)
		}
	}

	// Create AddonAssociation entity with status="pending"
	assoc := &addon_v1alpha.AddonAssociation{
		App:     app.ID,
		Addon:   addonEntity.ID,
		Variant: variantName,
		Version: version,
		Status:  "pending",
	}

	name := idgen.GenNS("addon-assoc")
	id, err := s.ec.Create(ctx, name, assoc)
	if err != nil {
		return fmt.Errorf("creating addon association: %w", err)
	}

	state.Results().SetId(string(id))
	s.log.Info("addon association created",
		"id", id,
		"app", appName,
		"addon", addonName,
		"variant", variantName,
		"version", version,
	)

	return nil
}

func (s *AddonsServer) ListInstances(ctx context.Context, state *app_v1alpha.AddonsListInstances) error {
	appName := state.Args().App()
	if appName == "" {
		return fmt.Errorf("app name is required")
	}

	var app core_v1alpha.App
	if err := s.ec.Get(ctx, appName, &app); err != nil {
		return fmt.Errorf("app %q not found: %w", appName, err)
	}

	results, err := s.ec.List(ctx, entity.Ref(addon_v1alpha.AddonAssociationAppId, app.ID))
	if err != nil {
		return fmt.Errorf("listing addon associations: %w", err)
	}

	var addons []*app_v1alpha.AddonInstance
	for results.Next() {
		var assoc addon_v1alpha.AddonAssociation
		if err := results.Read(&assoc); err != nil {
			return fmt.Errorf("reading addon association: %w", err)
		}

		instance := &app_v1alpha.AddonInstance{}
		instance.SetId(string(assoc.ID))
		instance.SetName(addon.NameFromRef(assoc.Addon))
		instance.SetAddon(string(assoc.Addon))
		instance.SetVariant(assoc.Variant)
		instance.SetVersion(assoc.Version)
		addons = append(addons, instance)
	}

	state.Results().SetAddons(addons)
	return nil
}

func (s *AddonsServer) DeleteInstance(ctx context.Context, state *app_v1alpha.AddonsDeleteInstance) error {
	appName := state.Args().App()
	addonName := state.Args().Name()

	if appName == "" {
		return fmt.Errorf("app name is required")
	}
	if addonName == "" {
		return fmt.Errorf("addon name is required")
	}

	var app core_v1alpha.App
	if err := s.ec.Get(ctx, appName, &app); err != nil {
		return fmt.Errorf("app %q not found: %w", appName, err)
	}

	// Find the association for this addon
	results, err := s.ec.List(ctx, entity.Ref(addon_v1alpha.AddonAssociationAppId, app.ID))
	if err != nil {
		return fmt.Errorf("listing addon associations: %w", err)
	}

	for results.Next() {
		var assoc addon_v1alpha.AddonAssociation
		if err := results.Read(&assoc); err != nil {
			return fmt.Errorf("reading addon association: %w", err)
		}

		if addon.NameFromRef(assoc.Addon) == addonName {
			// Set status to deprovisioning so the controller handles cleanup
			if err := s.ec.Patch(ctx, assoc.ID, 0,
				entity.String(addon_v1alpha.AddonAssociationStatusId, "deprovisioning"),
			); err != nil {
				return fmt.Errorf("updating association status: %w", err)
			}

			s.log.Info("addon marked for deprovisioning",
				"association", assoc.ID,
				"app", appName,
				"addon", addonName,
			)
			return nil
		}
	}

	return fmt.Errorf("addon %q is not attached to app %q", addonName, appName)
}

// RotateCredential creates a pending RotationRequest for the addon attached to
// the named app. The rotation controller reconciles it: apply a new secret to
// the live engine, update the stored value, and redeploy affected consumers.
func (s *AddonsServer) RotateCredential(ctx context.Context, state *app_v1alpha.AddonsRotateCredential) error {
	appName := state.Args().App()
	addonName := state.Args().Name()
	credential := state.Args().Credential()

	if appName == "" {
		return fmt.Errorf("app name is required")
	}
	if addonName == "" {
		return fmt.Errorf("addon name is required")
	}

	var app core_v1alpha.App
	if err := s.ec.Get(ctx, appName, &app); err != nil {
		return fmt.Errorf("app %q not found: %w", appName, err)
	}

	results, err := s.ec.List(ctx, entity.Ref(addon_v1alpha.AddonAssociationAppId, app.ID))
	if err != nil {
		return fmt.Errorf("listing addon associations: %w", err)
	}

	for results.Next() {
		var assoc addon_v1alpha.AddonAssociation
		if err := results.Read(&assoc); err != nil {
			return fmt.Errorf("reading addon association: %w", err)
		}
		if addon.NameFromRef(assoc.Addon) != addonName {
			continue
		}
		if assoc.Status != "active" {
			return fmt.Errorf("addon %q is not active (status %q); cannot rotate", addonName, assoc.Status)
		}

		req := &addon_v1alpha.RotationRequest{
			Association: assoc.ID,
			Addon:       assoc.Addon,
			Credential:  credential,
			Status:      "pending",
		}
		reqName := fmt.Sprintf("rotate-%s-%s", addonName, idgen.Gen("rr"))
		id, err := s.ec.Create(ctx, reqName, req)
		if err != nil {
			return fmt.Errorf("creating rotation request: %w", err)
		}

		s.log.Info("addon credential rotation requested",
			"request", id, "association", assoc.ID, "app", appName, "addon", addonName)
		state.Results().SetId(string(id))
		return nil
	}

	return fmt.Errorf("addon %q is not attached to app %q", addonName, appName)
}
