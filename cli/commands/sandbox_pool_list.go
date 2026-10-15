package commands

import (
	"fmt"
	"time"

	"miren.dev/runtime/api/compute/compute_v1alpha"
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
	if err != nil {
		return err
	}
	defer client.Close()
	res, err := compute_v1alpha.NewSandboxPoolsClient(client).List(ctx, "")
	if err != nil {
		return err
	}

	if opts.IsJSON() {
		pools := make([]sandboxPoolListEntry, 0, len(res.Pools()))
		for _, pool := range res.Pools() {
			pools = append(pools, sandboxPoolListEntry{
				ID: pool.Id(), ShortID: pool.ShortId(), App: pool.App(),
				Version: pool.Version(), VersionShortID: pool.VersionShortId(),
				Service: pool.Service(), ScalingMode: pool.ScalingMode(),
				DesiredInstances: pool.DesiredInstances(), CurrentInstances: pool.CurrentInstances(), ReadyInstances: pool.ReadyInstances(),
				CreatedAt: time.UnixMilli(pool.CreatedAt()).UTC().Format(time.RFC3339),
				UpdatedAt: time.UnixMilli(pool.UpdatedAt()).UTC().Format(time.RFC3339), Revision: pool.Revision(),
			})
		}
		return PrintJSONTo(ctx.Stdout, pools)
	}

	var rows []ui.Row
	headers := []string{"ID", "VERSION", "SERVICE", "MODE", "DESIRED", "CURRENT", "READY", "CREATED", "UPDATED"}

	for _, pool := range res.Pools() {
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
