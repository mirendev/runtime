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
	"miren.dev/runtime/pkg/entity"
)

// ReportStaleAssociationVariables warns about associations whose recorded
// variables disagree with the copy in the app's active version.
//
// Rotation used to write the new credential into a new app version and leave the
// association holding the provision-time value. The runtime now resolves through
// the association, so one left stale by a rotation on v0.12.x names a password
// that no longer works.
//
// This only reports; it never writes. There is no reliable way to tell which
// record is fresher: before this design the version was, after it the
// association is. A sweep that guessed wrong would take a working app offline,
// and it would guess wrong on every coordinator restart that followed a
// rotation. Running `miren addon rotate` again reconciles both records.
//
// Delete this once v0.12.x and v0.13.x are out of upgrade range. It writes
// nothing, so there is no data to unwind.
func ReportStaleAssociationVariables(
	ctx context.Context,
	log *slog.Logger,
	ec *entityserver.Client,
	eac *entityserver_v1alpha.EntityAccessClient,
) (stale int, checked int, err error) {
	results, err := ec.List(ctx, entity.Ref(entity.EntityKind, addon_v1alpha.KindAddonAssociation))
	if err != nil {
		return 0, 0, fmt.Errorf("listing addon associations: %w", err)
	}

	for results.Next() {
		var assoc addon_v1alpha.AddonAssociation
		if err := results.Read(&assoc); err != nil {
			return stale, checked, fmt.Errorf("reading addon association: %w", err)
		}

		if assoc.Status != "active" || len(assoc.Variables) == 0 {
			continue
		}
		checked++

		live, err := activeVariables(ctx, ec, eac, assoc.App)
		if err != nil {
			// One unreadable app must not stop the sweep.
			log.Warn("could not read active config while checking association variables",
				"association", assoc.ID, "app", assoc.App, "error", err)
			continue
		}

		var disagreeing []string
		for _, v := range assoc.Variables {
			if cur, ok := live[v.Key]; ok && cur.Value != v.Value {
				disagreeing = append(disagreeing, v.Key)
			}
		}
		if len(disagreeing) == 0 {
			continue
		}

		stale++
		// Warn, not Error: nothing has failed, and one command fixes it. Name the
		// keys but never the values, which are credentials.
		log.Warn("addon association variables disagree with the app's active config; "+
			"the app will run with the association's values. If this addon's credentials were "+
			"rotated before upgrading, re-run `miren addon rotate` to reconcile both records",
			"association", assoc.ID, "app", assoc.App, "keys", disagreeing)
	}

	return stale, checked, nil
}

// activeVariables returns the addon-sourced variables an app's active version
// currently carries, keyed by name.
func activeVariables(
	ctx context.Context,
	ec *entityserver.Client,
	eac *entityserver_v1alpha.EntityAccessClient,
	appID entity.Id,
) (map[string]core_v1alpha.ConfigSpecVariables, error) {
	var app core_v1alpha.App
	if err := ec.GetById(ctx, appID, &app); err != nil {
		return nil, fmt.Errorf("getting app %s: %w", appID, err)
	}
	if app.ActiveVersion == "" {
		return nil, nil
	}

	var version core_v1alpha.AppVersion
	if err := ec.GetById(ctx, app.ActiveVersion, &version); err != nil {
		return nil, fmt.Errorf("getting app version %s: %w", app.ActiveVersion, err)
	}

	// The stored config, not the resolved one. The point is to compare against
	// what the version carries, not against what the association would supply.
	spec, err := coreutil.ResolveConfig(ctx, eac, &version)
	if err != nil {
		return nil, fmt.Errorf("resolving config for %s: %w", app.ActiveVersion, err)
	}

	out := make(map[string]core_v1alpha.ConfigSpecVariables, len(spec.Variables))
	for _, v := range spec.Variables {
		if v.Source == coreutil.SourceAddon {
			out[v.Key] = v
		}
	}
	return out, nil
}
