package app

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	appclient "miren.dev/runtime/api/app"
	"miren.dev/runtime/api/app/app_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/rpc/standard"
)

func (r *AppInfo) ListVersions(ctx context.Context, state *app_v1alpha.CrudListVersions) error {
	a := state.Args()
	if !rpc.AllowApp(ctx, a.App()) {
		return rpc.ErrUnauthorized
	}
	if err := appclient.Authorize(ctx, r.EC.EAC(), a.App(), "read"); err != nil {
		return err
	}

	var app core_v1alpha.App
	target := a.App()
	if !strings.Contains(target, "/") {
		target = "app/" + target
	}
	record, err := r.EC.GetByIdWithEntity(ctx, entity.Id(target), &app)
	if err != nil {
		if rpc.BoundApp(ctx) != "" && errors.Is(err, cond.ErrNotFound{}) {
			return rpc.ErrUnauthorized
		}
		return err
	}
	if !app.Is(record.Entity()) {
		if rpc.BoundApp(ctx) != "" {
			return rpc.ErrUnauthorized
		}
		return fmt.Errorf("%q is not an app", a.App())
	}
	var owner core_v1alpha.Metadata
	owner.Decode(record.Entity())
	if !rpc.AllowApp(ctx, owner.Name) {
		return rpc.ErrUnauthorized
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
		v.SetShortId(results.Entity().ShortId())
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
