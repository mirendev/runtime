package sandbox

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"slices"

	appapi "miren.dev/runtime/api/app"
	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/ui"
)

func (s *Server) SelectForApp(ctx context.Context, state *compute_v1alpha.SandboxesSelectForApp) error {
	appName, service := state.Args().App(), state.Args().Service()
	if !rpc.AllowApp(ctx, appName) {
		return rpc.AppAccessError(ctx, appName)
	}
	var app core_v1alpha.App
	appEnt, err := s.ec.GetWithEntity(ctx, appName, &app)
	if err != nil && !errors.Is(err, cond.ErrNotFound{}) {
		return err
	}
	if err == nil && !app.Is(appEnt.Entity()) {
		err = fmt.Errorf("%q is not an app", appName)
	}
	if err != nil {
		// Only explicitly authorized callers may distinguish missing and non-app
		// targets from hidden apps. Resolve valid short IDs before checking owners.
		if authErr := rpc.AuthorizeResource(ctx, rpc.ResourceRequest{Resource: "app", Action: "exec", Instance: appName}); authErr != nil {
			return authErr
		}
		return err
	}
	var md core_v1alpha.Metadata
	md.Decode(appEnt.Entity())
	if !rpc.AllowApp(ctx, md.Name) {
		return rpc.AppAccessError(ctx, md.Name)
	}
	if err := appapi.Authorize(ctx, s.ec.EAC(), md.Name, "exec"); err != nil {
		return err
	}
	pools, err := s.ec.List(ctx, entity.Ref(compute_v1alpha.SandboxPoolAppId, app.ID))
	if err != nil {
		return err
	}
	poolServices := make(map[string]string)
	var known []string
	for pools.Next() {
		var pool compute_v1alpha.SandboxPool
		if err := pools.Read(&pool); err != nil {
			return err
		}
		if !slices.Contains(known, pool.Service) {
			known = append(known, pool.Service)
		}
		if service == "" || service == pool.Service {
			poolServices[pool.ID.String()] = pool.Service
		}
	}
	slices.Sort(known)
	if service != "" && len(poolServices) == 0 {
		state.Results().SetFailure("unknown_service")
		state.Results().SetServices(known)
		return nil
	}

	sandboxes, err := s.ec.List(ctx, entity.Ref(entity.EntityKind, compute_v1alpha.KindSandbox))
	if err != nil {
		return err
	}
	var candidates []*compute_v1alpha.SandboxSelection
	var running []string
	for sandboxes.Next() {
		var sb compute_v1alpha.Sandbox
		if err := sandboxes.Read(&sb); err != nil {
			return err
		}
		pool, _ := sandboxes.Metadata().Labels.Get("pool")
		svc, ours := poolServices[pool]
		var schedule compute_v1alpha.Schedule
		schedule.Decode(sandboxes.Entity())
		if !ours || sb.Status != compute_v1alpha.RUNNING || entity.Empty(schedule.Key.Node) {
			continue
		}
		if !slices.Contains(running, svc) {
			running = append(running, svc)
		}
		// Service is a requirement, version only a preference. In particular,
		// old web must beat new workers during a rolling or failed deploy.
		if service == "" && slices.Contains(known, "web") && svc != "web" {
			continue
		}
		candidate := new(compute_v1alpha.SandboxSelection)
		candidate.SetId(sb.ID.String())
		candidate.SetShortId(ui.BriefId(sandboxes.Entity()))
		candidate.SetService(svc)
		candidate.SetVersion(sb.Spec.Version.String())
		candidates = append(candidates, candidate)
	}
	if len(candidates) == 0 {
		if service == "" && slices.Contains(known, "web") {
			slices.Sort(running)
			state.Results().SetFailure("missing_default")
			state.Results().SetServices(running)
		} else {
			state.Results().SetFailure("no_running")
		}
		return nil
	}
	var active []*compute_v1alpha.SandboxSelection
	if !entity.Empty(app.ActiveVersion) {
		for _, candidate := range candidates {
			if candidate.Version() == app.ActiveVersion.String() {
				active = append(active, candidate)
			}
		}
	}
	if len(active) > 0 {
		candidates = active
	}
	selection := candidates[rand.IntN(len(candidates))]
	selection.SetCandidateCount(int32(len(candidates)))
	state.Results().SetSelection(selection)
	return nil
}
