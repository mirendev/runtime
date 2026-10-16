package sandbox

import (
	"context"
	"fmt"

	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/rpc"
)

// authorizedSandbox resolves ownership from the pool, or from the version for
// poolless app-run sandboxes. Unowned/operator sandboxes are only reachable by
// unbound callers whose method permission is checked by the RPC authorizer.
func (s *Server) authorizedSandbox(ctx context.Context, name string) (*compute_v1alpha.Sandbox, int64, error) {
	bound := rpc.BoundApp(ctx) != ""
	var sb compute_v1alpha.Sandbox
	ent, err := s.ec.GetWithEntity(ctx, name, &sb)
	if err != nil {
		ent, err = s.ec.GetByIdWithEntity(ctx, entity.Id(name), &sb)
		if err != nil {
			if bound {
				return nil, 0, rpc.ErrUnauthorized
			}
			return nil, 0, fmt.Errorf("sandbox %q not found: %w", name, err)
		}
	}
	if !sb.Is(ent.Entity()) {
		if bound {
			return nil, 0, rpc.ErrUnauthorized
		}
		return nil, 0, fmt.Errorf("%q is not a sandbox", name)
	}
	// Method-authorized cluster callers must be able to recover sandboxes
	// whose pool, version, or app has already been deleted.
	if !bound {
		return &sb, ent.Revision(), nil
	}

	var md core_v1alpha.Metadata
	md.Decode(ent.Entity())
	var appID entity.Id
	if poolID, ok := md.Labels.Get("pool"); ok && poolID != "" {
		var pool compute_v1alpha.SandboxPool
		poolEnt, err := s.ec.GetByIdWithEntity(ctx, entity.Id(poolID), &pool)
		if err != nil {
			return nil, 0, rpc.ErrUnauthorized
		}
		if pool.ID.String() != poolID || !pool.Is(poolEnt.Entity()) {
			return nil, 0, rpc.ErrUnauthorized
		}
		appID = pool.App
	} else if !entity.Empty(sb.Spec.Version) {
		var version core_v1alpha.AppVersion
		versionEnt, err := s.ec.GetByIdWithEntity(ctx, sb.Spec.Version, &version)
		if err != nil {
			return nil, 0, rpc.ErrUnauthorized
		}
		if version.ID != sb.Spec.Version || !version.Is(versionEnt.Entity()) {
			return nil, 0, rpc.ErrUnauthorized
		}
		appID = version.App
	}

	appName := ""
	if !entity.Empty(appID) {
		var app core_v1alpha.App
		appEnt, err := s.ec.GetByIdWithEntity(ctx, appID, &app)
		if err != nil {
			return nil, 0, rpc.ErrUnauthorized
		}
		if app.ID != appID || !app.Is(appEnt.Entity()) {
			return nil, 0, rpc.ErrUnauthorized
		}
		var owner core_v1alpha.Metadata
		owner.Decode(appEnt.Entity())
		appName = owner.Name
	}
	if !rpc.AllowApp(ctx, appName) {
		return nil, 0, rpc.ErrUnauthorized
	}
	return &sb, ent.Revision(), nil
}

func (s *Server) Stop(ctx context.Context, state *compute_v1alpha.SandboxesStop) error {
	sb, revision, err := s.authorizedSandbox(ctx, state.Args().Sandbox())
	if err != nil {
		return err
	}
	if sb.Status == compute_v1alpha.STOPPED || sb.Status == compute_v1alpha.DEAD {
		return nil
	}
	// Only write status, not a stale copy of the execution record.
	return s.ec.Patch(ctx, sb.ID, revision, entity.Ref(compute_v1alpha.SandboxStatusId, compute_v1alpha.SandboxStatusStoppedId))
}

func (s *Server) Delete(ctx context.Context, state *compute_v1alpha.SandboxesDelete) error {
	sb, revision, err := s.authorizedSandbox(ctx, state.Args().Sandbox())
	if err != nil {
		return err
	}
	if sb.Status != compute_v1alpha.DEAD {
		return fmt.Errorf("cannot delete sandbox %s: status is %s (must be dead)", sb.ID, sb.Status)
	}
	_, err = s.ec.EAC().DeleteAtRevision(ctx, sb.ID.String(), revision)
	return err
}
