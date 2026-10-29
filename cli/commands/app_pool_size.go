package commands

import (
	"fmt"
	"strconv"

	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/pkg/ui"
)

func AppPoolSize(ctx *Context, opts struct {
	AppCentric
	FormatOptions
	Size    string `position:"0" usage:"Desired instance count (omit to show current size)"`
	Service string `short:"s" long:"service" description:"Service to scale" default:"web"`
}) error {
	var count int64
	var err error
	if opts.Size != "" {
		count, err = strconv.ParseInt(opts.Size, 10, 64)
		if err != nil || count < 0 {
			return fmt.Errorf("pool size must be a non-negative integer")
		}
	}
	versions, err := fetchAppVersions(ctx, opts.App, false, 0)
	if err != nil {
		return err
	}
	var activeVersion string
	for _, version := range versions {
		if version.Status() == "active" {
			activeVersion = version.Id()
			break
		}
	}
	cl, err := ctx.RPCClient("dev.miren.runtime/sandbox-pools")
	if err != nil {
		return err
	}
	defer cl.Close()
	pools := compute_v1alpha.NewSandboxPoolsClient(cl)
	list, err := pools.List(ctx, opts.App)
	if err != nil {
		return err
	}
	var selected *compute_v1alpha.SandboxPoolInfo
	for _, pool := range list.Pools() {
		if activeVersion != "" && pool.Service() == opts.Service && pool.Version() == activeVersion {
			if selected != nil {
				return fmt.Errorf("multiple active pools for service %q; use 'miren debug sandbox-pool' to inspect them", opts.Service)
			}
			selected = pool
		}
	}
	if selected == nil {
		return fmt.Errorf("no active pool for app %q service %q", opts.App, opts.Service)
	}
	if opts.Size != "" {
		result, err := pools.SetDesired(ctx, selected.Id(), count, false, selected.Revision(), true)
		if err != nil {
			return err
		}
		selected.SetDesiredInstances(result.Desired())
	}
	result := struct {
		ID      string `json:"id"`
		ShortID string `json:"short_id,omitempty"`
		App     string `json:"app"`
		Service string `json:"service"`
		Desired int64  `json:"desired_instances"`
		Current int64  `json:"current_instances"`
		Ready   int64  `json:"ready_instances"`
	}{selected.Id(), selected.ShortId(), opts.App, opts.Service, selected.DesiredInstances(), selected.CurrentInstances(), selected.ReadyInstances()}
	if opts.IsJSON() {
		return PrintJSONTo(ctx.Stdout, result)
	}
	rows := []ui.Row{{ui.DisplayShortID(result.ShortID, result.ID), result.App, result.Service, strconv.FormatInt(result.Desired, 10), strconv.FormatInt(result.Current, 10), strconv.FormatInt(result.Ready, 10)}}
	headers := []string{"ID", "APP", "SERVICE", "DESIRED", "CURRENT", "READY"}
	ctx.Printf("%s\n", ui.NewTable(ui.WithColumns(ui.AutoSizeColumns(headers, rows, ui.Columns().NoTruncate(0))), ui.WithRows(rows)).Render())
	return nil
}
