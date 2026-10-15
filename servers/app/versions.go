package app

import (
	"context"
	"fmt"
	"sort"

	"miren.dev/runtime/api/app/app_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/rpc/standard"
)

func (r *AppInfo) ListVersions(ctx context.Context, state *app_v1alpha.CrudListVersions) error {
	a := state.Args()
	if !rpc.AllowApp(ctx, a.App()) {
		return rpc.AppAccessError(ctx, a.App())
	}

	var app core_v1alpha.App
	record, err := r.EC.GetWithEntity(ctx, a.App(), &app)
	if err != nil {
		return err
	}
	if !app.Is(record.Entity()) {
		return fmt.Errorf("%q is not an app", a.App())
	}
	var owner core_v1alpha.Metadata
	owner.Decode(record.Entity())
	if !rpc.AllowApp(ctx, owner.Name) {
		return rpc.AppAccessError(ctx, owner.Name)
	}
	results, err := r.EC.List(ctx, entity.Ref(core_v1alpha.AppVersionAppId, app.ID))
	if err != nil {
		return fmt.Errorf("failed to list app versions: %w", err)
	}

	var versions []*app_v1alpha.AppVersionSummary
	for results.Next() {
		var av core_v1alpha.AppVersion
		if err := results.Read(&av); err != nil {
			continue
		}
		if a.Ephemeral() && av.EphemeralLabel == "" {
			continue
		}

		var v app_v1alpha.AppVersionSummary
		v.SetId(av.ID.String())
		v.SetVersion(av.Version)
		v.SetCreatedAt(standard.ToTimestamp(results.Entity().GetCreatedAt()))
		switch {
		case av.ID == app.ActiveVersion:
			v.SetStatus("active")
		case av.EphemeralLabel != "":
			v.SetStatus("ephemeral")
		default:
			v.SetStatus("replaced")
		}
		if av.EphemeralLabel != "" {
			v.SetEphemeralLabel(av.EphemeralLabel)
			v.SetEphemeralTtl(av.EphemeralTtl)
			if !av.EphemeralExpiresAt.IsZero() {
				v.SetExpiresAt(standard.ToTimestamp(av.EphemeralExpiresAt))
			}
		}
		versions = append(versions, &v)
	}

	sort.Slice(versions, func(i, j int) bool {
		return standard.FromTimestamp(versions[i].CreatedAt()).After(standard.FromTimestamp(versions[j].CreatedAt()))
	})
	if a.Limit() > 0 && int64(len(versions)) > a.Limit() {
		versions = versions[:a.Limit()]
	}
	state.Results().SetVersions(versions)
	return nil
}
