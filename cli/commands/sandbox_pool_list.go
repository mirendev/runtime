package commands

import (
	"fmt"
	"time"

	"miren.dev/runtime/api/compute/compute_v1alpha"
	coreutil "miren.dev/runtime/api/core"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/ui"
)

func SandboxPoolList(ctx *Context, opts struct {
	FormatOptions
	ConfigCentric
}) error {
	client, err := ctx.RPCClient("entities")
	if err != nil {
		return err
	}

	eac := entityserver_v1alpha.NewEntityAccessClient(client)

	kindRes, err := eac.LookupKind(ctx, "sandbox_pool")
	if err != nil {
		return err
	}

	res, err := eac.List(ctx, kindRes.Attr())
	if err != nil {
		return err
	}

	// Fetch app versions to determine concurrency mode
	versionKindRes, err := eac.LookupKind(ctx, "app_version")
	if err != nil {
		return err
	}

	versionsRes, err := eac.List(ctx, versionKindRes.Attr())
	if err != nil {
		return err
	}

	// Build resolved config spec map and version short ID map for lookup
	specMap := make(map[string]*core_v1alpha.ConfigSpec)
	versionShortIdMap := make(map[string]string)
	for _, e := range versionsRes.Values() {
		v := new(core_v1alpha.AppVersion)
		v.Decode(e.Entity())
		if sid := e.Entity().ShortId(); sid != "" {
			versionShortIdMap[v.ID.String()] = sid
		}
		if resolvedCfg, err := coreutil.ResolveRuntimeConfig(ctx, eac, v); err == nil {
			specMap[v.ID.String()] = resolvedCfg
		}
	}

	if opts.IsJSON() {
		var pools []compute_v1alpha.SandboxPool

		for _, e := range res.Values() {
			var pool compute_v1alpha.SandboxPool
			pool.Decode(e.Entity())
			pools = append(pools, pool)
		}

		return PrintJSON(pools)
	}

	var rows []ui.Row
	headers := []string{"ID", "VERSION", "SERVICE", "MODE", "DESIRED", "CURRENT", "READY", "CREATED", "UPDATED"}

	for _, e := range res.Values() {
		var pool compute_v1alpha.SandboxPool
		pool.Decode(e.Entity())

		// Determine scaling mode from version config
		mode := "auto"
		if spec, ok := specMap[pool.SandboxSpec.Version.String()]; ok {
			for _, svc := range spec.Services {
				if svc.Name == pool.Service && svc.Concurrency.Mode == "fixed" {
					mode = "fixed"
					break
				}
			}
		}

		versionDisplay := ui.DisplayAppVersion(pool.SandboxSpec.Version.String())
		if shortId, ok := versionShortIdMap[pool.SandboxSpec.Version.String()]; ok {
			versionDisplay = shortId
		}

		rows = append(rows, ui.Row{
			ui.BriefId(e.Entity()),
			versionDisplay,
			pool.Service,
			mode,
			fmt.Sprintf("%d", pool.DesiredInstances),
			fmt.Sprintf("%d", pool.CurrentInstances),
			fmt.Sprintf("%d", pool.ReadyInstances),
			humanFriendlyTimestamp(time.UnixMilli(e.CreatedAt())),
			humanFriendlyTimestamp(time.UnixMilli(e.UpdatedAt())),
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
