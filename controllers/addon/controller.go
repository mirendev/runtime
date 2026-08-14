package addon

import (
	"context"
	"fmt"
	"log/slog"

	"miren.dev/runtime/api/addon/addon_v1alpha"
	coreutil "miren.dev/runtime/api/core"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/addon"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/saga"
)

// Controller reconciles AddonAssociation entities, driving provisioning
// and deprovisioning of addons through their providers.
//
// Implements controller.ReconcileControllerI[*addon_v1alpha.AddonAssociation]
type Controller struct {
	log      *slog.Logger
	ec       *entityserver.Client
	eac      *entityserver_v1alpha.EntityAccessClient
	registry *addon.Registry

	// Providers build their own executors, so this is not for running sagas.
	// It is here so the controller can retire a teardown record it owns; see
	// deprovision.
	sagaStorage saga.Storage
}

// NewController creates a new addon controller.
func NewController(
	log *slog.Logger,
	ec *entityserver.Client,
	eac *entityserver_v1alpha.EntityAccessClient,
	registry *addon.Registry,
	sagaStorage saga.Storage,
) *Controller {
	return &Controller{
		log:         log.With("module", "addon"),
		ec:          ec,
		eac:         eac,
		registry:    registry,
		sagaStorage: sagaStorage,
	}
}

func (c *Controller) Init(ctx context.Context) error {
	c.log.Info("initializing addon controller")
	return nil
}

func (c *Controller) Reconcile(ctx context.Context, assoc *addon_v1alpha.AddonAssociation, meta *entity.Meta) error {
	switch assoc.Status {
	case "pending", "provisioning":
		// "provisioning" belongs here because provision() writes it before the
		// work starts, so a coordinator that dies mid-provision leaves the
		// association on it. The switch used to have no case for that: every
		// later pass fell through and returned in silence, nothing picked the
		// work back up, and because the deployment launcher holds an app back
		// while an association is still provisioning, the app stopped
		// deploying too. That is MIR-1524.
		//
		// Re-entering is safe because of the revs below this one: the
		// execution is named after the association, so a second pass continues
		// the interrupted attempt rather than starting a fresh one beside
		// whatever the first already built.
		return c.provision(ctx, assoc, meta)
	case "deprovisioning":
		return c.deprovision(ctx, assoc, meta)
	case "active", "error":
		return nil
	default:
		return nil
	}
}

func (c *Controller) provision(ctx context.Context, assoc *addon_v1alpha.AddonAssociation, meta *entity.Meta) error {
	c.log.Info("provisioning addon", "association", assoc.ID, "addon", assoc.Addon, "variant", assoc.Variant)

	// Re-read the association to guard against stale events (e.g. on startup
	// resync, the reconcile event may carry an older revision than what's
	// now in the store). If the user has already marked this association for
	// deprovisioning, skip; the subsequent deprovisioning event will handle it.
	//
	// "provisioning" passes as well as "pending", because an attempt a crash
	// interrupted is still an attempt that should finish.
	var current addon_v1alpha.AddonAssociation
	if err := c.ec.GetById(ctx, assoc.ID, &current); err != nil {
		return fmt.Errorf("re-reading association: %w", err)
	}
	if current.Status != "pending" && current.Status != "provisioning" {
		c.log.Info("association no longer wants provisioning, skipping",
			"association", assoc.ID, "status", current.Status)
		return nil
	}

	// Step 1: Set status to provisioning
	if err := meta.Update((&addon_v1alpha.AddonAssociation{Status: "provisioning"}).Encode()); err != nil {
		return fmt.Errorf("setting status to provisioning: %w", err)
	}

	// Resolve provider
	addonName := addon.NameFromRef(assoc.Addon)
	provider, _, ok := c.registry.Get(addonName)
	if !ok {
		return c.setError(meta, fmt.Errorf("unknown addon %q", addonName))
	}

	// Resolve variant config (includes resolved image based on version)
	variantConfig, err := c.registry.GetVariantConfig(addonName, assoc.Variant, assoc.Version)
	if err != nil {
		return c.setError(meta, fmt.Errorf("resolving variant config: %w", err))
	}

	// Look up the app to get its name
	appName, err := c.resolveAppName(ctx, assoc.App)
	if err != nil {
		return c.setError(meta, fmt.Errorf("resolving app name: %w", err))
	}

	// Step 2: Call provider.Provision
	app := addon.App{
		ID:   assoc.App,
		Name: appName,
	}
	result, err := provider.Provision(ctx, addon.AssociationFrom(assoc, meta.Entity), app, addon.Variant{
		Name:   assoc.Variant,
		Config: variantConfig,
	})
	if err != nil {
		return c.setError(meta, fmt.Errorf("provisioning: %w", err))
	}

	// Steps 3-6: Complete provisioning. If any post-provision step fails,
	// compensate by calling Deprovision to clean up the resources that were
	// just created. If compensation also fails, return the error without
	// setting terminal "error" status so the controller retries.
	if err := c.completeProvision(ctx, assoc, meta, provider, result); err != nil {
		// Same reasoning as deprovision(): this compensation runs the teardown
		// saga under the association's name, so a failed one would answer every
		// later attempt from its own record and the retry promised above would
		// never actually run.
		if dropErr := saga.DropIfFailed(ctx, c.sagaStorage,
			addon.DeprovisionExecutionID(assoc.ID)); dropErr != nil {
			return fmt.Errorf("clearing failed teardown record: %w", dropErr)
		}
		depErr := provider.Deprovision(ctx, addon.AssociationFrom(assoc, meta.Entity))
		if depErr != nil {
			c.log.Error("compensation deprovision failed, will retry",
				"provision_error", err, "deprovision_error", depErr)
			return fmt.Errorf("provision failed: %v; compensation failed: %w", err, depErr)
		}
		return c.setError(meta, err)
	}

	c.log.Info("addon provisioned", "association", assoc.ID)
	return nil
}

// completeProvision performs the post-provision steps (attrs, env vars, version
// creation, status update). It is separated so that the caller can compensate
// by deprovisioning if any step fails.
func (c *Controller) completeProvision(
	ctx context.Context,
	assoc *addon_v1alpha.AddonAssociation,
	meta *entity.Meta,
	provider addon.AddonProvider,
	result *addon.ProvisionResult,
) error {
	// Step 3: Append provider attrs to association entity
	if len(result.Attrs) > 0 {
		if err := meta.Update(result.Attrs); err != nil {
			return fmt.Errorf("appending provider attrs: %w", err)
		}
	}

	// Step 4: Check for env var collisions and adjust if needed
	existingVars, err := c.getAppVariables(ctx, assoc.App)
	if err != nil {
		return fmt.Errorf("getting existing app variables: %w", err)
	}

	envVars := result.EnvVars
	collisions := findCollisions(existingVars, envVars)
	if len(collisions) > 0 {
		adjusted, err := provider.AdjustEnvVars(ctx, result, addon.AssociationFrom(assoc, meta.Entity), collisions)
		if err != nil {
			return fmt.Errorf("adjusting env vars: %w", err)
		}
		envVars = adjusted
	}

	// Step 5: Record what this addon supplies. This is the only write. The app's
	// versions are untouched; the binding reaches the app because
	// ResolveRuntimeConfig reads it from here. Step 6 sets status to active, which
	// makes it visible, and the launcher's AddonAssociation watch turns that into
	// a reconcile.
	variables := make([]addon_v1alpha.Variables, len(envVars))
	for i, v := range envVars {
		variables[i] = addon_v1alpha.Variables{
			Key:       v.Key,
			Value:     v.Value,
			Sensitive: v.Sensitive,
		}
	}
	if err := meta.Update((&addon_v1alpha.AddonAssociation{
		Variables: variables,
	}).Encode()); err != nil {
		return fmt.Errorf("persisting addon variables: %w", err)
	}

	// Step 6: Set status to active
	if err := meta.Update((&addon_v1alpha.AddonAssociation{
		Status: "active",
	}).Encode()); err != nil {
		return fmt.Errorf("setting status to active: %w", err)
	}

	return nil
}

func (c *Controller) deprovision(ctx context.Context, assoc *addon_v1alpha.AddonAssociation, meta *entity.Meta) error {
	c.log.Info("deprovisioning addon", "association", assoc.ID, "addon", assoc.Addon)

	// Resolve provider
	addonName := addon.NameFromRef(assoc.Addon)
	provider, _, ok := c.registry.Get(addonName)
	if !ok {
		return c.setError(meta, fmt.Errorf("unknown addon %q", addonName))
	}

	// Naming the execution after the association makes a teardown resumable,
	// but it also makes a failed one permanent: Execute would hand back the
	// recorded error on every later pass, so `addon destroy` would be one-shot
	// until saga retention freed the name. Retrying a compensated saga is the
	// owner's decision, and for teardown the controller is that owner. Every
	// undo in these sagas is a no-op, so a failed run tore nothing down and put
	// nothing back; a fresh attempt has strictly more to do, never something to
	// redo.
	if err := saga.DropIfFailed(ctx, c.sagaStorage,
		addon.DeprovisionExecutionID(assoc.ID)); err != nil {
		return fmt.Errorf("clearing failed teardown record: %w", err)
	}

	// Step 1: Call provider.Deprovision
	err := provider.Deprovision(ctx, addon.AssociationFrom(assoc, meta.Entity))
	if err != nil {
		// Try to set error status, but don't fail if the update is rejected
		// (e.g., the app was deleted and the entity server rejects the patch
		// due to a dangling app reference). The entity stays at "deprovisioning"
		// so the controller will retry.
		if setErr := c.setError(meta, fmt.Errorf("deprovisioning: %w", err)); setErr != nil {
			c.log.Warn("failed to set error status during deprovision", "error", setErr)
		}
		return fmt.Errorf("deprovisioning: %w", err)
	}

	// Step 2: Delete the association entity.
	//
	// Nothing strips the variables from the app's config, because they were never
	// written there. The app stopped receiving them when its status became
	// "deprovisioning": ResolveRuntimeConfig reads only active associations, and
	// the launcher's AddonAssociation watch reconciled the app on that change.
	// The watch ignores deletes, so this is not itself a trigger. That is fine,
	// because the app was already reconciled without the binding.
	if err := c.ec.Delete(ctx, assoc.ID); err != nil {
		return fmt.Errorf("deleting association: %w", err)
	}

	c.log.Info("addon deprovisioned", "association", assoc.ID)
	return nil
}

func (c *Controller) setError(meta *entity.Meta, err error) error {
	c.log.Error("addon error", "error", err)

	updateErr := meta.Update((&addon_v1alpha.AddonAssociation{
		Status:       "error",
		ErrorMessage: err.Error(),
	}).Encode())
	if updateErr != nil {
		return fmt.Errorf("setting error status: %w (original: %w)", updateErr, err)
	}

	return nil
}

// resolveAppName looks up an App entity and returns its metadata name.
func (c *Controller) resolveAppName(ctx context.Context, appID entity.Id) (string, error) {
	return resolveAppName(ctx, c.ec, appID)
}

func resolveAppName(ctx context.Context, ec *entityserver.Client, appID entity.Id) (string, error) {
	var meta core_v1alpha.Metadata
	if err := ec.GetById(ctx, appID, &meta); err != nil {
		return "", fmt.Errorf("getting app entity: %w", err)
	}
	if meta.Name == "" {
		return string(appID), nil
	}
	return meta.Name, nil
}

// getAppVariables fetches the current variables from the app's active version.
func (c *Controller) getAppVariables(ctx context.Context, appID entity.Id) ([]core_v1alpha.Variable, error) {
	var app core_v1alpha.App
	if err := c.ec.GetById(ctx, appID, &app); err != nil {
		return nil, fmt.Errorf("getting app: %w", err)
	}
	if app.ActiveVersion == "" {
		return nil, nil
	}

	var version core_v1alpha.AppVersion
	if err := c.ec.GetById(ctx, app.ActiveVersion, &version); err != nil {
		return nil, fmt.Errorf("getting app version: %w", err)
	}

	// The runtime view, so a collision with another addon's binding is visible
	// even though no version stores those bindings.
	spec, err := coreutil.ResolveRuntimeConfig(ctx, c.eac, &version)
	if err != nil {
		return nil, fmt.Errorf("resolving config: %w", err)
	}

	// Convert ConfigSpecVariables to Variable
	vars := make([]core_v1alpha.Variable, len(spec.Variables))
	for i, v := range spec.Variables {
		vars[i] = core_v1alpha.Variable(v)
	}
	return vars, nil
}

// findCollisions returns keys that exist in both existing vars and addon vars.
func findCollisions(existing []core_v1alpha.Variable, addonVars []addon.Variable) []string {
	existingKeys := make(map[string]bool, len(existing))
	for _, v := range existing {
		existingKeys[v.Key] = true
	}

	var collisions []string
	for _, v := range addonVars {
		if existingKeys[v.Key] {
			collisions = append(collisions, v.Key)
		}
	}
	return collisions
}
