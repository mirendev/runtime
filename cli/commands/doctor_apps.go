package commands

import (
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"time"

	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/clientconfig"
	"miren.dev/runtime/pkg/ui"
)

// DoctorApps shows diagnostic information about apps
func DoctorApps(ctx *Context, opts struct {
	ConfigCentric
}) error {
	cfg, err := opts.LoadConfig()
	if err != nil {
		if errors.Is(err, clientconfig.ErrNoConfig) {
			ctx.Printf("No cluster configured\n")
			ctx.Printf("\nUse 'miren cluster add' to add a cluster\n")
			return nil
		}
		return err
	}

	clusterName := cfg.ActiveCluster()
	if opts.Cluster != "" {
		clusterName = opts.Cluster
	}

	_, err = cfg.GetCluster(clusterName)
	if err != nil {
		return err
	}

	client, err := ctx.RPCClient("entities")
	if err != nil {
		return err
	}
	defer client.Close()

	eac := entityserver_v1alpha.NewEntityAccessClient(client)

	// Get apps
	kindRes, err := eac.LookupKind(ctx, "app")
	if err != nil {
		return err
	}

	appsRes, err := eac.List(ctx, kindRes.Attr())
	if err != nil {
		return err
	}

	// Get versions
	versionKindRes, err := eac.LookupKind(ctx, "app_version")
	if err != nil {
		return err
	}

	versionsRes, err := eac.List(ctx, versionKindRes.Attr())
	if err != nil {
		return err
	}

	// Get deployments
	deploymentKindRes, err := eac.LookupKind(ctx, "deployment")
	if err != nil {
		return err
	}

	deploymentsRes, err := eac.List(ctx, deploymentKindRes.Attr())
	if err != nil {
		return err
	}

	// Get sandboxes for instance count
	sandboxKindRes, err := eac.LookupKind(ctx, "sandbox")
	if err != nil {
		return err
	}

	sandboxesRes, err := eac.List(ctx, sandboxKindRes.Attr())
	if err != nil {
		return err
	}

	// Build version map (version ID -> app version, and version ID -> app name)
	versionMap := make(map[string]*core_v1alpha.AppVersion)
	versionToApp := make(map[string]string) // version ID -> app name
	for _, e := range versionsRes.Values() {
		v := new(core_v1alpha.AppVersion)
		v.Decode(e.Entity())
		versionMap[v.ID.String()] = v
		// Extract app name from the app ref
		appName := ui.CleanEntityID(v.App.String())
		versionToApp[v.ID.String()] = appName
	}

	// Build deployment map (most recent deployment per app)
	deploymentMap := make(map[string]*core_v1alpha.Deployment)
	for _, e := range deploymentsRes.Values() {
		d := new(core_v1alpha.Deployment)
		d.Decode(e.Entity())

		if existing, ok := deploymentMap[d.AppName]; ok {
			existingTime, existingErr := time.Parse(time.RFC3339, existing.CompletedAt)
			newTime, newErr := time.Parse(time.RFC3339, d.CompletedAt)

			// Replace if: new has valid time and (existing invalid OR new is later)
			if newErr == nil && (existingErr != nil || newTime.After(existingTime)) {
				deploymentMap[d.AppName] = d
			}
		} else {
			deploymentMap[d.AppName] = d
		}
	}

	// Build instance count map (app name -> running instance count)
	instanceCount := make(map[string]int)
	for _, e := range sandboxesRes.Values() {
		var sandbox compute_v1alpha.Sandbox
		sandbox.Decode(e.Entity())

		// Only count running sandboxes
		if sandbox.Status != compute_v1alpha.RUNNING {
			continue
		}

		// Get app name from version
		versionID := sandbox.Spec.Version.String()
		if appName, ok := versionToApp[versionID]; ok {
			instanceCount[appName]++
		}
	}

	// Build table
	var rows []ui.Row
	var appNames []string // Store actual app names for interactive picker
	headers := []string{"NAME", "VERSION", "STATUS", "INSTANCES", "BRANCH"}

	for _, e := range appsRes.Values() {
		var app core_v1alpha.App
		app.Decode(e.Entity())

		var md core_v1alpha.Metadata
		md.Decode(e.Entity())

		name := md.Name
		appNames = append(appNames, name) // Store actual name before any formatting
		version := "-"
		status := "-"
		instances := "0"
		branch := "-"

		// Get instance count
		if count, ok := instanceCount[md.Name]; ok {
			instances = fmt.Sprintf("%d", count)
			if count > 0 {
				instances = infoGreen.Render(instances)
			}
		}

		if app.ActiveVersion.String() != "" {
			if appVersion, ok := versionMap[app.ActiveVersion.String()]; ok {
				version = ui.DisplayAppVersion(appVersion.Version)
			}
		}

		if deployment, ok := deploymentMap[md.Name]; ok {
			// Get deployment status with color
			if deployment.Status != "" {
				switch deployment.Status {
				case "active":
					status = infoGreen.Render(deployment.Status)
				case "failed":
					status = infoRed.Render(deployment.Status)
				case "in_progress":
					status = infoLabel.Render(deployment.Status)
				default:
					status = deployment.Status
				}
			}

			// Get git branch
			if deployment.GitInfo.Branch != "" {
				branch = deployment.GitInfo.Branch
			}

		}

		rows = append(rows, ui.Row{name, version, status, instances, branch})
	}

	sort.Slice(rows, func(i, j int) bool {
		return rows[i][0] < rows[j][0]
	})
	sort.Strings(appNames)

	ctx.Printf("%s\n", infoBold.Render("Apps"))
	ctx.Printf("%s\n", infoGray.Render("===="))

	if len(rows) == 0 {
		ctx.Printf("%s\n", infoGray.Render("No apps found"))
		return nil
	}

	ctx.Printf("%s %d total\n\n", infoLabel.Render("Count:"), len(rows))

	columns := ui.AutoSizeColumns(headers, rows, nil)
	table := ui.NewTable(
		ui.WithColumns(columns),
		ui.WithRows(rows),
	)

	ctx.Printf("%s\n", table.Render())

	// Interactive mode
	if !ui.IsInteractive() {
		return nil
	}

	ctx.Printf("\n")

	for {
		// Build picker items from app names
		var items []ui.PickerItem
		for _, name := range appNames {
			items = append(items, ui.SimplePickerItem{Text: name})
		}
		items = append(items, ui.SimplePickerItem{Text: "[done]"})

		selected, err := ui.RunPicker(items, ui.WithTitle("Select an app:"))
		if err != nil || selected == nil || selected.ID() == "[done]" {
			return nil
		}

		appName := selected.ID()

		// Action picker for selected app
		actionItems := []ui.PickerItem{
			ui.SimplePickerItem{Text: "View logs"},
			ui.SimplePickerItem{Text: "Redeploy"},
			ui.SimplePickerItem{Text: "[back]"},
		}

		action, err := ui.RunPicker(actionItems, ui.WithTitle(fmt.Sprintf("Action for %s:", appName)))
		if err != nil || action == nil || action.ID() == "[back]" {
			continue
		}

		switch action.ID() {
		case "View logs":
			ctx.Printf("\n%s\n\n", infoGray.Render("Logs for "+appName+":"))
			cmd := exec.Command("miren", "logs", "-a", appName)
			cmd.Stdout = ctx.Stdout
			cmd.Stderr = ctx.Stderr
			cmd.Run()
			ctx.Printf("\n%s", infoGray.Render("Press Enter to continue..."))
			fmt.Scanln()
			ctx.Printf("\n")

		case "Redeploy":
			ctx.Printf("\n%s\n", infoGray.Render("Redeploying "+appName+"..."))
			cmd := exec.Command("miren", "deploy", "-a", appName)
			cmd.Stdout = ctx.Stdout
			cmd.Stderr = ctx.Stderr
			if err := cmd.Run(); err != nil {
				ctx.Printf("%s\n", infoRed.Render("Redeploy failed: "+err.Error()))
			} else {
				ctx.Printf("%s\n", infoGreen.Render("✓ Redeploy initiated"))
			}
			ctx.Printf("\n")
		}
	}
}
