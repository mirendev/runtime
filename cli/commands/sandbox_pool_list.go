package commands

import (
	"fmt"
	"time"

	"miren.dev/runtime/api/compute/compute_v1alpha"
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
	headers := []string{"ID", "VERSION", "SERVICE", "DESIRED", "CURRENT", "READY", "CREATED", "UPDATED"}

	for _, e := range res.Values() {
		var pool compute_v1alpha.SandboxPool
		pool.Decode(e.Entity())

		rows = append(rows, ui.Row{
			ui.CleanEntityID(pool.ID.String()),
			ui.DisplayAppVersion(pool.SandboxSpec.Version.String()),
			pool.Service,
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
