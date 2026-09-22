package commands

import (
	"fmt"
	"strings"

	"miren.dev/runtime/appconfig"
	"miren.dev/runtime/clientconfig"
	"miren.dev/runtime/pkg/ui"
)

type deployTargetAddOpts struct {
	AppCentric
	Name        string `position:"0" usage:"Name for the deployment target (prompts when omitted)"`
	ClusterName string `position:"1" usage:"Configured cluster name (prompts when omitted)"`
	Default     bool   `long:"default" description:"Make this the default deployment target"`
}

func DeployTargetAdd(ctx *Context, opts deployTargetAddOpts) error {
	name := opts.Name
	if name == "" {
		if !ui.IsInteractive() {
			return fmt.Errorf("name is required in non-interactive mode")
		}
		var err error
		name, err = ui.PromptForInput(ui.WithLabel("Deployment target name"))
		if err != nil {
			return fmt.Errorf("failed to read deployment target name: %w", err)
		}
		if name == "" {
			return fmt.Errorf("deployment target name is required")
		}
	}

	clusterName, cluster, err := selectDeployTargetCluster(opts.ConfigCentric, opts.ClusterName)
	if err != nil {
		return err
	}
	if cluster == nil {
		return nil
	}

	dc, err := appconfig.LoadDeployConfigUnder(opts.ResolvedDir())
	if err != nil {
		return fmt.Errorf("error loading %s: %w", appconfig.DeployConfigPath, err)
	}
	if dc == nil {
		dc = &appconfig.DeployConfig{}
	}

	target := appconfig.DeployTarget{Name: name, Cluster: clusterName, ClusterID: cluster.XID}
	if err := dc.AddTarget(target, opts.Default); err != nil {
		return err
	}
	if err := appconfig.SaveDeployConfigUnder(opts.ResolvedDir(), dc); err != nil {
		return fmt.Errorf("error saving %s: %w", appconfig.DeployConfigPath, err)
	}

	ctx.Printf("Added deploy target %q for cluster %q", name, clusterName)
	if cluster.XID != "" {
		ctx.Printf(" (%s)", cluster.XID)
	}
	ctx.Printf("\n")
	if len(dc.Targets) == 1 || opts.Default {
		ctx.Printf("Default deploy target: %s\n", name)
	}
	return nil
}

func selectDeployTargetCluster(opts ConfigCentric, requested string) (string, *clientconfig.ClusterConfig, error) {
	cfg, err := opts.LoadConfig()
	if err != nil {
		return "", nil, err
	}
	if requested == "" {
		requested = opts.Cluster
	}

	if requested != "" {
		cluster, err := cfg.GetCluster(requested)
		if err != nil || cluster == nil {
			return "", nil, fmt.Errorf("cluster %q not found; configured clusters: %s", requested, strings.Join(cfg.GetClusterNames(), ", "))
		}
		return requested, cluster, nil
	}

	names := cfg.GetClusterNames()
	if len(names) == 0 {
		return "", nil, fmt.Errorf("no clusters configured; add one with 'miren cluster add'")
	}
	items := make([]ui.PickerItem, len(names))
	for i, name := range names {
		cluster, err := cfg.GetCluster(name)
		if err != nil {
			return "", nil, err
		}
		id := cluster.XID
		if id == "" {
			id = "-"
		}
		items[i] = ui.TablePickerItem{Columns: []string{name, id}, ItemID: name}
	}

	selected, err := ui.RunPicker(items,
		ui.WithTitle("Select a cluster for this deployment target:"),
		ui.WithHeaders([]string{"CLUSTER", "CLOUD ID"}),
	)
	if err != nil {
		return "", nil, fmt.Errorf("cluster is required in non-interactive mode; configured clusters: %s", strings.Join(names, ", "))
	}
	if selected == nil {
		return "", nil, nil
	}
	name := selected.ID()
	cluster, err := cfg.GetCluster(name)
	return name, cluster, err
}

type deployTargetProjectOpts struct {
	AppCentric
	FormatOptions
}

func DeployTargetList(ctx *Context, opts deployTargetProjectOpts) error {
	dc, err := appconfig.LoadDeployConfigUnder(opts.ResolvedDir())
	if err != nil {
		return fmt.Errorf("error loading %s: %w", appconfig.DeployConfigPath, err)
	}
	if dc == nil {
		if opts.IsJSON() {
			return PrintJSON([]appconfig.DeployTarget{})
		}
		ctx.Printf("No deployment targets configured. Add one with: miren deploy target add <name> [cluster]\n")
		return nil
	}

	if opts.IsJSON() {
		type targetJSON struct {
			Name      string `json:"name"`
			Cluster   string `json:"cluster"`
			ClusterID string `json:"cluster_id,omitempty"`
			Default   bool   `json:"default"`
		}
		items := make([]targetJSON, len(dc.Targets))
		for i, target := range dc.Targets {
			items[i] = targetJSON{Name: target.Name, Cluster: target.Cluster, ClusterID: target.ClusterID, Default: i == 0}
		}
		return PrintJSON(items)
	}

	rows := make([]ui.Row, len(dc.Targets))
	for i, target := range dc.Targets {
		isDefault := ""
		if i == 0 {
			isDefault = "✓"
		}
		clusterID := target.ClusterID
		if clusterID == "" {
			clusterID = "-"
		}
		rows[i] = ui.Row{target.Name, target.Cluster, clusterID, isDefault}
	}
	table := ui.NewTable(
		ui.WithColumns(ui.AutoSizeColumns([]string{"TARGET", "CLUSTER", "CLOUD ID", "DEFAULT"}, rows, nil)),
		ui.WithRows(rows),
	)
	ctx.Printf("%s\n", table.Render())
	return nil
}

type deployTargetNameOpts struct {
	AppCentric
	Name string `position:"0" usage:"Deployment target name" required:"true"`
}

func DeployTargetRemove(ctx *Context, opts deployTargetNameOpts) error {
	dc, err := appconfig.LoadDeployConfigUnder(opts.ResolvedDir())
	if err != nil {
		return fmt.Errorf("error loading %s: %w", appconfig.DeployConfigPath, err)
	}
	if dc == nil {
		return fmt.Errorf("no deployment targets configured")
	}
	if err := dc.RemoveTarget(opts.Name); err != nil {
		return err
	}
	if len(dc.Targets) == 0 {
		err = appconfig.RemoveDeployConfigUnder(opts.ResolvedDir())
	} else {
		err = appconfig.SaveDeployConfigUnder(opts.ResolvedDir(), dc)
	}
	if err != nil {
		return fmt.Errorf("error saving %s: %w", appconfig.DeployConfigPath, err)
	}
	ctx.Printf("Removed deploy target %q\n", opts.Name)
	return nil
}

func DeployTargetSetDefault(ctx *Context, opts deployTargetNameOpts) error {
	dc, err := appconfig.LoadDeployConfigUnder(opts.ResolvedDir())
	if err != nil {
		return fmt.Errorf("error loading %s: %w", appconfig.DeployConfigPath, err)
	}
	if dc == nil {
		return fmt.Errorf("no deployment targets configured")
	}
	if err := dc.SetDefaultTarget(opts.Name); err != nil {
		return err
	}
	if err := appconfig.SaveDeployConfigUnder(opts.ResolvedDir(), dc); err != nil {
		return fmt.Errorf("error saving %s: %w", appconfig.DeployConfigPath, err)
	}
	ctx.Printf("Default deploy target: %s\n", opts.Name)
	return nil
}
