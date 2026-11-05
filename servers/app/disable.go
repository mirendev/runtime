package app

import (
	"context"
	"fmt"
	"slices"
	"time"

	appclient "miren.dev/runtime/api/app"
	"miren.dev/runtime/api/app/app_v1alpha"
	"miren.dev/runtime/api/compute/compute_v1alpha"
	coreutil "miren.dev/runtime/api/core"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/pkg/entity"
)

// Disable holds every service of an app at zero instances until Enable. The
// app keeps its versions, routes, env, disks and addons; nothing boots for it,
// builds are refused, and requests get a 503.
//
// The flag on the App is what keeps it disabled: the launcher, activator and
// pool manager all read it, so the pools scaled down here stay down.
func (r *AppInfo) Disable(ctx context.Context, state *app_v1alpha.CrudDisable) error {
	args := state.Args()
	name := args.App()

	if err := appclient.Authorize(ctx, r.EC.EAC(), name, "deploy"); err != nil {
		return err
	}

	var appRec core_v1alpha.App
	if err := r.EC.Get(ctx, name, &appRec); err != nil {
		return fmt.Errorf("app %q not found: %w", name, err)
	}

	// Set the flag before touching pools, so nothing that reads it scales a
	// pool back up in between. Disabling again only updates the reason.
	disabled := core_v1alpha.Disabled{At: appRec.Disabled.At, Reason: args.Reason()}
	if disabled.At.IsZero() {
		disabled.At = time.Now()
	}
	if err := r.EC.Patch(ctx, appRec.ID, 0,
		entity.Component(core_v1alpha.AppDisabledId, disabled.Encode()),
	); err != nil {
		return fmt.Errorf("marking app %q disabled: %w", name, err)
	}

	poolList, err := r.EC.List(ctx, entity.Ref(compute_v1alpha.SandboxPoolAppId, appRec.ID))
	if err != nil {
		return fmt.Errorf("listing pools: %w", err)
	}

	var scaledPools int32
	for poolList.Next() {
		var pool compute_v1alpha.SandboxPool
		if err := poolList.Read(&pool); err != nil {
			continue
		}
		if pool.DesiredInstances == 0 {
			continue
		}
		if err := r.EC.Patch(ctx, pool.ID, 0,
			entity.Int64(compute_v1alpha.SandboxPoolDesiredInstancesId, 0),
		); err != nil {
			return fmt.Errorf("scaling pool %s to zero: %w", pool.ID, err)
		}
		scaledPools++
	}

	r.Log.Info("app disabled", "app", name, "reason", args.Reason(), "pools_scaled", scaledPools)

	state.Results().SetScaledPools(scaledPools)
	return nil
}

// Enable turns a disabled app back on. Fixed-mode services of the active
// version return to their configured count right away; autoscaled services
// stay at zero and start on the next request.
func (r *AppInfo) Enable(ctx context.Context, state *app_v1alpha.CrudEnable) error {
	name := state.Args().App()

	if err := appclient.Authorize(ctx, r.EC.EAC(), name, "deploy"); err != nil {
		return err
	}

	var appRec core_v1alpha.App
	if err := r.EC.Get(ctx, name, &appRec); err != nil {
		return fmt.Errorf("app %q not found: %w", name, err)
	}
	if appRec.Disabled.Empty() {
		return fmt.Errorf("app %q is not disabled", name)
	}

	// The scheduler resumes from this timestamp rather than replaying pending
	// ticks, even if it never observed the app disabled between sweeps.
	if err := r.EC.Patch(ctx, appRec.ID, 0,
		entity.Component(core_v1alpha.AppDisabledId, nil),
		entity.Time(core_v1alpha.AppSchedulesResumedAtId, time.Now()),
	); err != nil {
		return fmt.Errorf("enabling app %q: %w", name, err)
	}

	// The app is enabled from here on, so a failure below is not a failed
	// enable: retrying would only report that the app isn't disabled, and the
	// launcher's resync restores fixed counts within a minute anyway.
	restoredPools, err := r.restoreFixedPools(ctx, &appRec)
	if err != nil {
		r.Log.Warn("app enabled, but restoring its fixed pools failed; the launcher will restore them",
			"app", name, "error", err)
	}

	r.Log.Info("app enabled", "app", name, "pools_restored", restoredPools)

	state.Results().SetRestoredPools(restoredPools)
	return nil
}

// restoreFixedPools returns the active version's fixed services to their
// configured counts, so enable takes effect now rather than at the next resync.
// It reports how many pools it restored, including when it stops on an error.
func (r *AppInfo) restoreFixedPools(ctx context.Context, appRec *core_v1alpha.App) (int32, error) {
	if appRec.ActiveVersion == "" {
		return 0, nil
	}

	var ver core_v1alpha.AppVersion
	if err := r.EC.GetById(ctx, appRec.ActiveVersion, &ver); err != nil {
		return 0, fmt.Errorf("getting active version: %w", err)
	}
	spec, err := coreutil.ResolveRuntimeConfig(ctx, r.EC.EAC(), &ver)
	if err != nil {
		return 0, fmt.Errorf("resolving config: %w", err)
	}

	poolList, err := r.EC.List(ctx, entity.Ref(compute_v1alpha.SandboxPoolAppId, appRec.ID))
	if err != nil {
		return 0, fmt.Errorf("listing pools: %w", err)
	}

	var restored int32
	for poolList.Next() {
		var pool compute_v1alpha.SandboxPool
		if err := poolList.Read(&pool); err != nil {
			continue
		}
		if !slices.Contains(pool.ReferencedByVersions, appRec.ActiveVersion) {
			continue
		}
		svcConc, err := coreutil.GetServiceConcurrency(spec, pool.Service)
		if err != nil || svcConc.Mode != "fixed" || svcConc.NumInstances == 0 {
			continue
		}
		if pool.DesiredInstances == svcConc.NumInstances {
			continue
		}
		if err := r.EC.Patch(ctx, pool.ID, 0,
			entity.Int64(compute_v1alpha.SandboxPoolDesiredInstancesId, svcConc.NumInstances),
		); err != nil {
			return restored, fmt.Errorf("restoring pool %s: %w", pool.ID, err)
		}
		restored++
	}
	return restored, nil
}
