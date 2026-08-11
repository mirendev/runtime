package addon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"miren.dev/runtime/api/addon/addon_v1alpha"
	coreutil "miren.dev/runtime/api/core"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/addon"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/idgen"
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
}

// NewController creates a new addon controller.
func NewController(
	log *slog.Logger,
	ec *entityserver.Client,
	eac *entityserver_v1alpha.EntityAccessClient,
	registry *addon.Registry,
) *Controller {
	return &Controller{
		log:      log.With("module", "addon"),
		ec:       ec,
		eac:      eac,
		registry: registry,
	}
}

func (c *Controller) Init(ctx context.Context) error {
	c.log.Info("initializing addon controller")
	return nil
}

func (c *Controller) Reconcile(ctx context.Context, assoc *addon_v1alpha.AddonAssociation, meta *entity.Meta) error {
	switch assoc.Status {
	case "pending":
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
	var current addon_v1alpha.AddonAssociation
	if err := c.ec.GetById(ctx, assoc.ID, &current); err != nil {
		return fmt.Errorf("re-reading association: %w", err)
	}
	if current.Status != "pending" {
		c.log.Info("association no longer pending, skipping provision",
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

	// Step 5: Create a new AppVersion with addon env vars merged in and activate it.
	if err := c.createVersionWithAddonVars(ctx, assoc.App, envVars); err != nil {
		return fmt.Errorf("creating version with addon vars: %w", err)
	}

	// Step 5b: Persist variables on the association immediately so that
	// compensation (deprovision → removeEnvVars) can find the keys to
	// remove even if step 6 fails.
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

	// Step 2: Remove addon env vars from app ConfigVersion
	if err := c.removeEnvVars(ctx, assoc.App, assoc.Variables); err != nil {
		return fmt.Errorf("removing addon env vars: %w", err)
	}

	// Step 3: Delete the association entity
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

	spec, err := coreutil.ResolveConfig(ctx, c.eac, &version)
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

// createVersionWithAddonVars creates a new AppVersion with addon env vars merged
// into the ConfigVersion and sets it as the active version. This ensures the
// launcher always reads a version that has all vars baked in.
func (c *Controller) createVersionWithAddonVars(ctx context.Context, appID entity.Id, envVars []addon.Variable) error {
	return createVersionWithVars(ctx, c.log, c.ec, c.eac, appID, envVars)
}

// errNoActiveVersion signals that an app has no active version to update. It is
// treated as a hard error on the provision path (triggering compensation) and as
// a no-op on the deprovision path (nothing to remove).
var errNoActiveVersion = errors.New("app has no active version")

// updateAppActiveVersion atomically applies transform to the app's active config
// spec: it resolves the current active config, lets transform mutate it in place,
// mints a new ConfigVersion/AppVersion, and points ActiveVersion at it under
// optimistic concurrency control — which triggers a redeploy via the launcher's
// App watch.
//
// Multiple addon associations for the same app reconcile concurrently — they are
// distinct entities handled by separate controller workers, and the in-flight
// guard only serializes events for the same entity ID. Without OCC, two writers
// both read the same active version and the second Patch clobbers the first,
// dropping one addon's variables (MIR-1458). The Patch is therefore guarded by a
// CAS on the app's revision and the whole read-modify-write is retried on
// conflict, so the retry observes the winner's version and the transforms
// compose. Shared by the provisioning, deprovisioning, and rotation paths.
func updateAppActiveVersion(
	ctx context.Context,
	log *slog.Logger,
	ec *entityserver.Client,
	eac *entityserver_v1alpha.EntityAccessClient,
	appID entity.Id,
	transform func(spec *core_v1alpha.ConfigSpec) error,
) error {
	// maxAttempts is only a live-lock backstop to guarantee termination — it is
	// not expected to be reached. Each conflict means some other writer swung the
	// active version first; there are only ever a handful of concurrent addon
	// operations per app, so a few retries suffice. It is set high so genuine
	// contention never spuriously fails a caller, and on exhaustion the error is
	// returned to the reconcile framework, which retries the whole reconcile.
	const maxAttempts = 100

	for attempt := 1; ; attempt++ {
		var app core_v1alpha.App
		appEnt, err := ec.GetByIdWithEntity(ctx, appID, &app)
		if err != nil {
			return fmt.Errorf("getting app: %w", err)
		}
		if app.ActiveVersion == "" {
			return errNoActiveVersion
		}
		appRev := appEnt.Revision()

		var version core_v1alpha.AppVersion
		if err := ec.GetById(ctx, app.ActiveVersion, &version); err != nil {
			return fmt.Errorf("getting app version: %w", err)
		}

		// Resolve the current config (reads ConfigVersion if present, else inline)
		spec, err := coreutil.ResolveConfig(ctx, eac, &version)
		if err != nil {
			return fmt.Errorf("resolving config: %w", err)
		}

		if err := transform(spec); err != nil {
			return err
		}

		appName, err := resolveAppName(ctx, ec, appID)
		if err != nil {
			return fmt.Errorf("resolving app name: %w", err)
		}

		newVersionName := appName + "-" + idgen.Gen("v")

		configVer := &core_v1alpha.ConfigVersion{
			App:  appID,
			Spec: *spec,
		}
		cvid, err := ec.Create(ctx, newVersionName+"-cfg", configVer)
		if err != nil {
			return fmt.Errorf("creating config version: %w", err)
		}

		version.Version = newVersionName
		version.ConfigVersion = cvid
		version.Config = core_v1alpha.Config{}

		newID, err := ec.Create(ctx, newVersionName, &version)
		if err != nil {
			return fmt.Errorf("creating new app version: %w", err)
		}

		// Swing the active version under OCC. A conflict means another writer
		// updated the active version between our read and this write, so retry
		// against the new active version.
		err = ec.Patch(ctx, appID, appRev,
			entity.Ref(core_v1alpha.AppActiveVersionId, newID),
		)
		if err == nil {
			log.Info("updated app active version",
				"app", appID, "old_version", app.ActiveVersion, "new_version", newID)
			return nil
		}

		if errors.Is(err, cond.ErrConflict{}) {
			// This attempt lost the race, so the version pair we just minted was
			// never activated. Best-effort delete it, AppVersion first: only drop
			// the ConfigVersion once its AppVersion is gone, so a failed delete
			// leaves a coherent pair for the version GC to reap rather than an
			// AppVersion dangling at a missing ConfigVersion.
			if delErr := ec.Delete(ctx, newID); delErr != nil {
				log.Warn("failed to delete superseded app version after conflict",
					"app", appID, "version", newID, "error", delErr)
			} else if delErr := ec.Delete(ctx, cvid); delErr != nil {
				log.Warn("failed to delete superseded config version after conflict",
					"app", appID, "config_version", cvid, "error", delErr)
			}

			if attempt < maxAttempts {
				log.Info("app active version changed concurrently, retrying",
					"app", appID, "attempt", attempt)
				continue
			}
		}

		return fmt.Errorf("setting active version: %w", err)
	}
}

// createVersionWithVars merges envVars into the app's active config, mints a new
// ConfigVersion + AppVersion, and points ActiveVersion at it — which triggers a
// redeploy via the launcher's App watch. It is shared by the provisioning path
// (adding addon vars) and the rotation path (replacing a rotated credential).
func createVersionWithVars(
	ctx context.Context,
	log *slog.Logger,
	ec *entityserver.Client,
	eac *entityserver_v1alpha.EntityAccessClient,
	appID entity.Id,
	envVars []addon.Variable,
) error {
	if len(envVars) == 0 {
		return nil
	}

	return updateAppActiveVersion(ctx, log, ec, eac, appID, func(spec *core_v1alpha.ConfigSpec) error {
		// Convert ConfigSpecVariables to Variable for merging
		existingVars := make([]core_v1alpha.Variable, len(spec.Variables))
		for i, v := range spec.Variables {
			existingVars[i] = core_v1alpha.Variable(v)
		}

		merged := mergeAddonVars(existingVars, envVars)

		// Write back as ConfigSpecVariables
		spec.Variables = make([]core_v1alpha.ConfigSpecVariables, len(merged))
		for i, v := range merged {
			spec.Variables[i] = core_v1alpha.ConfigSpecVariables(v)
		}
		return nil
	})
}

// removeEnvVars removes addon-sourced variables from the app's active version
// by creating a new ConfigVersion without those vars and a new AppVersion
// pointing to it.
func (c *Controller) removeEnvVars(ctx context.Context, appID entity.Id, variables []addon_v1alpha.Variables) error {
	if len(variables) == 0 {
		return nil
	}

	// Build set of keys to remove
	removeKeys := make(map[string]bool, len(variables))
	for _, v := range variables {
		removeKeys[v.Key] = true
	}

	err := updateAppActiveVersion(ctx, c.log, c.ec, c.eac, appID, func(spec *core_v1alpha.ConfigSpec) error {
		var filtered []core_v1alpha.ConfigSpecVariables
		for _, v := range spec.Variables {
			if removeKeys[v.Key] && v.Source == "addon" {
				continue
			}
			filtered = append(filtered, v)
		}
		spec.Variables = filtered
		return nil
	})

	// The app or its active version being gone (or having no active version)
	// means there is nothing left to clean up — treat as a successful no-op.
	if errors.Is(err, cond.ErrNotFound{}) || errors.Is(err, errNoActiveVersion) {
		c.log.Info("app or active version unavailable, skipping env var removal", "app", appID)
		return nil
	}
	return err
}

// mergeAddonVars merges addon-contributed variables into the existing variable set.
// Addon vars (source="addon") never override manual vars.
func mergeAddonVars(existing []core_v1alpha.Variable, addonVars []addon.Variable) []core_v1alpha.Variable {
	varMap := make(map[string]core_v1alpha.Variable, len(existing))

	for _, v := range existing {
		varMap[v.Key] = v
	}

	for _, v := range addonVars {
		source := ""
		if ev, ok := varMap[v.Key]; ok {
			source = ev.Source
			if source == "" {
				source = "manual"
			}
		}

		// Never override manual vars
		if source == "manual" {
			continue
		}

		varMap[v.Key] = core_v1alpha.Variable{
			Key:       v.Key,
			Value:     v.Value,
			Sensitive: v.Sensitive,
			Source:    "addon",
		}
	}

	result := make([]core_v1alpha.Variable, 0, len(varMap))
	for _, v := range varMap {
		result = append(result, v)
	}
	return result
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
