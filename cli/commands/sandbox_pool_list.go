package commands

import (
	"fmt"
	"time"

	"miren.dev/runtime/api/compute/compute_v1alpha"
	coreutil "miren.dev/runtime/api/core"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/ui"
)

type sandboxPoolListEntry struct {
	ID               string `json:"id"`
	ShortID          string `json:"short_id,omitempty"`
	App              string `json:"app,omitempty"`
	Version          string `json:"version,omitempty"`
	VersionShortID   string `json:"version_short_id,omitempty"`
	Service          string `json:"service"`
	ScalingMode      string `json:"scaling_mode"`
	DesiredInstances int64  `json:"desired_instances"`
	CurrentInstances int64  `json:"current_instances"`
	ReadyInstances   int64  `json:"ready_instances"`
	CreatedAt        string `json:"created_at"`
	UpdatedAt        string `json:"updated_at"`
	Revision         int64  `json:"revision"`
}

func SandboxPoolList(ctx *Context, opts struct {
	FormatOptions
	ConfigCentric
}) error {
	client, err := ctx.RPCClient("dev.miren.runtime/sandbox-pools")
	if err == nil {
		defer client.Close()
		err = requireRPCMethod(ctx, client, "list")
	}
	if err != nil && !isMissingRPC(err) {
		return err
	}
	var pools []*compute_v1alpha.SandboxPoolInfo
	if err == nil {
		res, callErr := compute_v1alpha.NewSandboxPoolsClient(client).List(ctx, "")
		err = callErr
		if err == nil {
			pools = res.Pools()
		}
	}
	if isMissingRPC(err) {
		pools, err = sandboxPoolsLegacy(ctx)
	}
	if err != nil {
		return err
	}

	if opts.IsJSON() {
		entries := make([]sandboxPoolListEntry, 0, len(pools))
		for _, pool := range pools {
			entries = append(entries, sandboxPoolListEntry{
				ID: pool.Id(), ShortID: pool.ShortId(), App: pool.App(),
				Version: pool.Version(), VersionShortID: pool.VersionShortId(),
				Service: pool.Service(), ScalingMode: pool.ScalingMode(),
				DesiredInstances: pool.DesiredInstances(), CurrentInstances: pool.CurrentInstances(), ReadyInstances: pool.ReadyInstances(),
				CreatedAt: time.UnixMilli(pool.CreatedAt()).UTC().Format(time.RFC3339),
				UpdatedAt: time.UnixMilli(pool.UpdatedAt()).UTC().Format(time.RFC3339), Revision: pool.Revision(),
			})
		}
		return PrintJSONTo(ctx.Stdout, entries)
	}

	var rows []ui.Row
	headers := []string{"ID", "VERSION", "SERVICE", "MODE", "DESIRED", "CURRENT", "READY", "CREATED", "UPDATED"}

	for _, pool := range pools {
		versionDisplay := ui.DisplayAppVersion(pool.Version())
		if shortId := pool.VersionShortId(); shortId != "" {
			versionDisplay = shortId
		}

		rows = append(rows, ui.Row{
			ui.DisplayShortID(pool.ShortId(), pool.Id()),
			versionDisplay,
			pool.Service(),
			pool.ScalingMode(),
			fmt.Sprintf("%d", pool.DesiredInstances()),
			fmt.Sprintf("%d", pool.CurrentInstances()),
			fmt.Sprintf("%d", pool.ReadyInstances()),
			humanFriendlyTimestamp(time.UnixMilli(pool.CreatedAt())),
			humanFriendlyTimestamp(time.UnixMilli(pool.UpdatedAt())),
		})
	}

	if len(rows) == 0 {
		ctx.Printf("No sandbox pools found\n")
		return nil
	}

	columns := ui.AutoSizeColumns(headers, rows, ui.Columns().NoTruncate(0))
	table := ui.NewTable(
		ui.WithColumns(columns),
		ui.WithRows(rows),
	)

	ctx.Printf("%s\n", table.Render())
	return nil
}

func sandboxPoolsLegacy(ctx *Context) ([]*compute_v1alpha.SandboxPoolInfo, error) {
	client, err := ctx.RPCClient("entities")
	if err != nil {
		return nil, err
	}
	defer client.Close()
	eac := entityserver_v1alpha.NewEntityAccessClient(client)
	res, err := eac.List(ctx, entity.Ref(entity.EntityKind, compute_v1alpha.KindSandboxPool))
	if err != nil {
		return nil, err
	}
	versions, err := eac.List(ctx, entity.Ref(entity.EntityKind, core_v1alpha.KindAppVersion))
	if err != nil {
		return nil, err
	}
	specs := make(map[string]*core_v1alpha.ConfigSpec)
	shortIDs := make(map[string]string)
	for _, entry := range versions.Values() {
		var version core_v1alpha.AppVersion
		version.Decode(entry.Entity())
		shortIDs[version.ID.String()] = entry.Entity().ShortId()
		if spec, err := coreutil.ResolveRuntimeConfig(ctx, eac, &version); err == nil {
			specs[version.ID.String()] = spec
		}
	}
	items := make([]*compute_v1alpha.SandboxPoolInfo, 0, len(res.Values()))
	for _, entry := range res.Values() {
		var pool compute_v1alpha.SandboxPool
		pool.Decode(entry.Entity())
		item := new(compute_v1alpha.SandboxPoolInfo)
		item.SetId(pool.ID.String())
		item.SetShortId(entry.Entity().ShortId())
		item.SetApp(ui.CleanEntityID(pool.App.String()))
		item.SetVersion(pool.SandboxSpec.Version.String())
		item.SetVersionShortId(shortIDs[pool.SandboxSpec.Version.String()])
		item.SetService(pool.Service)
		item.SetScalingMode("auto")
		if spec := specs[pool.SandboxSpec.Version.String()]; spec != nil {
			for _, svc := range spec.Services {
				if svc.Name == pool.Service && svc.Concurrency.Mode == "fixed" {
					item.SetScalingMode("fixed")
				}
			}
		}
		item.SetDesiredInstances(pool.DesiredInstances)
		item.SetCurrentInstances(pool.CurrentInstances)
		item.SetReadyInstances(pool.ReadyInstances)
		item.SetCreatedAt(entry.CreatedAt())
		item.SetUpdatedAt(entry.UpdatedAt())
		item.SetRevision(entry.Revision())
		items = append(items, item)
	}
	return items, nil
}
