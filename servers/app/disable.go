package app

import (
	"context"
	"fmt"
	"slices"
	"time"

	"miren.dev/runtime/api/app/app_v1alpha"
	"miren.dev/runtime/api/compute/compute_v1alpha"
	coreutil "miren.dev/runtime/api/core"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/rpc"
)

// Disable holds every service of an app at zero instances until Enable. The
// app keeps its versions, routes, env, disks and addons; nothing boots for it,
// including on a deploy, and requests get a 503.
//
// The flag on the App is what keeps it disabled: the launcher, activator and
// pool manager all read it, so the pools scaled down here stay down.
func (r *AppInfo) Disable(ctx context.Context, state *app_v1alpha.CrudDisable) error {
	args := state.Args()
	name := args.App()

	if !rpc.AllowApp(ctx, name) {
		return rpc.AppAccessError(ctx, name)
	}

	var appRec core_v1alpha.App
	if err := r.EC.Get(ctx, name, &appRec); err != nil {
		return fmt.Errorf("app %q not found: %w", name, err)
	}

	// Set the flag before touching pools, so nothing that reads it scales a
	// pool back up in between. Disabling again only updates the reason.
	disabledAt := appRec.DisabledAt
	if disabledAt.IsZero() {
		disabledAt = time.Now()
	}
	if err := r.EC.Patch(ctx, appRec.ID, 0,
		entity.Time(core_v1alpha.AppDisabledAtId, disabledAt),
		entity.String(core_v1alpha.AppDisabledReasonId, args.Reason()),
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

	if !rpc.AllowApp(ctx, name) {
		return rpc.AppAccessError(ctx, name)
	}

	var appRec core_v1alpha.App
	if err := r.EC.Get(ctx, name, &appRec); err != nil {
		return fmt.Errorf("app %q not found: %w", name, err)
	}
	if appRec.DisabledAt.IsZero() {
		return fmt.Errorf("app %q is not disabled", name)
	}

	if err := r.EC.Patch(ctx, appRec.ID, 0,
		entity.Time(core_v1alpha.AppDisabledAtId, time.Time{}),
		entity.String(core_v1alpha.AppDisabledReasonId, ""),
	); err != nil {
		return fmt.Errorf("enabling app %q: %w", name, err)
	}

	// The launcher's resync would restore fixed counts within a minute; doing
	// it here makes enable take effect now.
	var restoredPools int32
	if appRec.ActiveVersion != "" {
		var ver core_v1alpha.AppVersion
		if err := r.EC.GetById(ctx, appRec.ActiveVersion, &ver); err != nil {
			return fmt.Errorf("getting active version: %w", err)
		}
		spec, err := coreutil.ResolveRuntimeConfig(ctx, r.EC.EAC(), &ver)
		if err != nil {
			return fmt.Errorf("resolving config: %w", err)
		}

		poolList, err := r.EC.List(ctx, entity.Ref(compute_v1alpha.SandboxPoolAppId, appRec.ID))
		if err != nil {
			return fmt.Errorf("listing pools: %w", err)
		}
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
				return fmt.Errorf("restoring pool %s: %w", pool.ID, err)
			}
			restoredPools++
		}
	}

	r.Log.Info("app enabled", "app", name, "pools_restored", restoredPools)

	state.Results().SetRestoredPools(restoredPools)
	return nil
}
