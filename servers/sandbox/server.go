// Package sandbox serves sandbox inventory, lifecycle and exec selection.
//
// Sandbox entities are execution records and contain resolved container
// environment values. This package is the boundary that turns those raw
// records into a deliberately small, safe inventory response.
package sandbox

import (
	"context"
	"log/slog"
	"strings"

	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/rpc"
)

type Server struct {
	log *slog.Logger
	ec  *entityserver.Client
}

func NewServer(log *slog.Logger, ec *entityserver.Client) *Server {
	return &Server{log: log.With("module", "sandbox-inventory"), ec: ec}
}

var _ compute_v1alpha.Sandboxes = (*Server)(nil)

func (s *Server) List(ctx context.Context, state *compute_v1alpha.SandboxesList) error {
	if _, err := rpc.FilterResource(ctx, rpc.ResourceRequest{Resource: "app", Action: "list"}); err != nil {
		return err
	}
	apps, err := inventoryApps(ctx, s.ec)
	if err != nil {
		return err
	}
	pools, err := s.ec.List(ctx, entity.Ref(entity.EntityKind, compute_v1alpha.KindSandboxPool))
	if err != nil {
		return err
	}
	poolServices := make(map[string]string)
	poolShortIDs := make(map[string]string)
	poolApps := make(map[string]entity.Id)
	for pools.Next() {
		var pool compute_v1alpha.SandboxPool
		if err := pools.Read(&pool); err != nil {
			return err
		}
		poolServices[pool.ID.String()] = pool.Service
		poolApps[pool.ID.String()] = pool.App
		if shortID := pools.Entity().ShortId(); shortID != "" {
			poolShortIDs[pool.ID.String()] = shortID
		}
	}

	// Missing associations are orphans, but failed bulk reads are not evidence
	// that an association is missing.
	versionApps := make(map[string]entity.Id)
	versionShortIDs := make(map[string]string)
	if versions, listErr := s.ec.List(ctx, entity.Ref(entity.EntityKind, core_v1alpha.KindAppVersion)); listErr != nil {
		return listErr
	} else {
		for versions.Next() {
			var version core_v1alpha.AppVersion
			if err := versions.Read(&version); err != nil {
				return err
			}
			versionApps[version.ID.String()] = version.App
			if shortID := versions.Entity().ShortId(); shortID != "" {
				versionShortIDs[version.ID.String()] = shortID
			}
		}
	}

	nodes, err := s.ec.List(ctx, entity.Ref(entity.EntityKind, compute_v1alpha.KindNode))
	if err != nil {
		return err
	}
	nodeNames := make(map[entity.Id]string)
	for nodes.Next() {
		var node compute_v1alpha.Node
		if err := nodes.Read(&node); err != nil {
			return err
		}
		name := node.Name
		if name == "" {
			name = node.RunnerId
			if len(name) > 12 {
				name = name[:12]
			}
		}
		if name != "" {
			nodeNames[node.ID] = name
		}
	}

	sandboxes, err := s.ec.List(ctx, entity.Ref(entity.EntityKind, compute_v1alpha.KindSandbox))
	if err != nil {
		return err
	}

	items := make([]*compute_v1alpha.SandboxInfo, 0, sandboxes.Length())
	for sandboxes.Next() {
		var sb compute_v1alpha.Sandbox
		if err := sandboxes.Read(&sb); err != nil {
			return err
		}
		ent := sandboxes.Entity()

		var metadata core_v1alpha.Metadata
		metadata.Decode(ent)
		pool, _ := metadata.Labels.Get("pool")
		service := poolServices[pool]
		appID := versionApps[sb.Spec.Version.String()]
		if pool != "" {
			appID = poolApps[pool]
		}
		owner := apps[appID]
		allowed, err := filterInventoryApp(ctx, owner)
		if err != nil {
			return err
		}
		if !allowed {
			continue
		}

		address := ""
		if len(sb.Network) > 0 {
			address = sb.Network[0].Address
		}

		var schedule compute_v1alpha.Schedule
		schedule.Decode(ent)
		runner := ""
		if !entity.Empty(schedule.Key.Node) {
			runner = nodeNames[schedule.Key.Node]
		}

		status := strings.TrimPrefix(string(sb.Status), "status.")
		if status == "" {
			status = "unknown"
		}

		item := new(compute_v1alpha.SandboxInfo)
		item.SetId(sb.ID.String())
		item.SetShortId(ent.ShortId())
		item.SetApp(owner.Instance)
		item.SetVersion(sb.Spec.Version.String())
		item.SetVersionShortId(versionShortIDs[sb.Spec.Version.String()])
		item.SetService(service)
		item.SetPool(pool)
		item.SetPoolShortId(poolShortIDs[pool])
		item.SetAddress(address)
		item.SetRunner(runner)
		item.SetStatus(status)
		item.SetCreatedAt(ent.GetCreatedAt().UnixMilli())
		item.SetUpdatedAt(ent.GetUpdatedAt().UnixMilli())
		items = append(items, item)
	}

	state.Results().SetSandboxes(items)
	return nil
}

func inventoryApps(ctx context.Context, ec *entityserver.Client) (map[entity.Id]rpc.ResourceRequest, error) {
	apps, err := ec.List(ctx, entity.Ref(entity.EntityKind, core_v1alpha.KindApp))
	if err != nil {
		return nil, err
	}
	owners := make(map[entity.Id]rpc.ResourceRequest)
	for apps.Next() {
		var app core_v1alpha.App
		if err := apps.Read(&app); err != nil {
			return nil, err
		}
		var md core_v1alpha.Metadata
		md.Decode(apps.Entity())
		if md.Name != "" {
			owners[app.ID] = rpc.ResourceRequest{Resource: "app", Action: "list", Instance: md.Name, Owner: app.CreatedBy}
		}
	}
	return owners, nil
}

func filterInventoryApp(ctx context.Context, owner rpc.ResourceRequest) (bool, error) {
	if !rpc.AllowApp(ctx, owner.Instance) {
		return false, nil
	}
	if owner.Instance == "" {
		owner = rpc.ResourceRequest{Resource: "cluster", Action: "admin"}
	}
	return rpc.FilterResource(ctx, owner)
}
