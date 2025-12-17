package commands

import (
	"errors"
	"fmt"
	"net"
	"sort"
	"strings"
	"time"

	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/api/ingress"
	"miren.dev/runtime/clientconfig"
	"miren.dev/runtime/pkg/ui"
)

// hyperlink creates a clickable terminal hyperlink using OSC 8 escape sequence
func hyperlink(url, text string) string {
	return fmt.Sprintf("\x1b]8;;%s\x1b\\%s\x1b]8;;\x1b\\", url, text)
}

func AppList(ctx *Context, opts struct {
	FormatOptions
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

	cluster, err := cfg.GetCluster(clusterName)
	if err != nil {
		return err
	}

	client, err := ctx.RPCClient("entities")
	if err != nil {
		return err
	}
	defer client.Close()

	// Get default hostname for routes display
	defaultHost := ""
	if cluster.Hostname != "" {
		defaultHost = cluster.Hostname
		// Strip port if present (handles IPv6)
		if h, _, err := net.SplitHostPort(defaultHost); err == nil {
			defaultHost = h
		}
	}

	eac := entityserver_v1alpha.NewEntityAccessClient(client)

	kindRes, err := eac.LookupKind(ctx, "app")
	if err != nil {
		return err
	}

	res, err := eac.List(ctx, kindRes.Attr())
	if err != nil {
		return err
	}

	versionKindRes, err := eac.LookupKind(ctx, "app_version")
	if err != nil {
		return err
	}

	versionsRes, err := eac.List(ctx, versionKindRes.Attr())
	if err != nil {
		return err
	}

	deploymentKindRes, err := eac.LookupKind(ctx, "deployment")
	if err != nil {
		return err
	}

	deploymentsRes, err := eac.List(ctx, deploymentKindRes.Attr())
	if err != nil {
		return err
	}

	// Get routes
	ic := ingress.NewClient(ctx.Log, client)
	routes, err := ic.List(ctx)
	if err != nil {
		return err
	}

	// Get sandbox pools for runtime state
	poolKindRes, err := eac.LookupKind(ctx, "sandbox_pool")
	if err != nil {
		return err
	}

	poolsRes, err := eac.List(ctx, poolKindRes.Attr())
	if err != nil {
		return err
	}

	// Build version map
	versionMap := make(map[string]*core_v1alpha.AppVersion)
	for _, e := range versionsRes.Values() {
		v := new(core_v1alpha.AppVersion)
		v.Decode(e.Entity())
		versionMap[v.ID.String()] = v
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

	// Build routes map (app name -> routes)
	routeMap := make(map[string][]string)
	for _, r := range routes {
		appName := ui.CleanEntityID(string(r.Route.App))
		host := r.Route.Host
		if host == "" && r.Route.Default {
			if defaultHost != "" {
				host = defaultHost
			}
		}
		if host != "" {
			routeMap[appName] = append(routeMap[appName], host)
		}
	}

	// Aggregate pool state per app (sum across all services)
	type appPoolState struct {
		ready        int
		desired      int
		inCooldown   bool
		crashCount   int64
		cooldownLeft time.Duration
	}
	poolStateMap := make(map[string]*appPoolState)
	now := time.Now()
	for _, e := range poolsRes.Values() {
		var pool compute_v1alpha.SandboxPool
		pool.Decode(e.Entity())

		appName := ui.CleanEntityID(pool.App.String())
		if poolStateMap[appName] == nil {
			poolStateMap[appName] = &appPoolState{}
		}
		state := poolStateMap[appName]
		state.ready += int(pool.ReadyInstances)
		state.desired += int(pool.DesiredInstances)
		if !pool.CooldownUntil.IsZero() && pool.CooldownUntil.After(now) {
			state.inCooldown = true
			state.crashCount = pool.ConsecutiveCrashCount
			state.cooldownLeft = pool.CooldownUntil.Sub(now)
		}
	}

	if opts.IsJSON() {
		var apps []struct {
			Name             string   `json:"name"`
			Version          string   `json:"version,omitempty"`
			ReadyInstances   int      `json:"ready_instances"`
			DesiredInstances int      `json:"desired_instances"`
			Health           string   `json:"health"`
			Routes           []string `json:"routes,omitempty"`
		}

		for _, e := range res.Values() {
			var app core_v1alpha.App
			app.Decode(e.Entity())

			var md core_v1alpha.Metadata
			md.Decode(e.Entity())

			appData := struct {
				Name             string   `json:"name"`
				Version          string   `json:"version,omitempty"`
				ReadyInstances   int      `json:"ready_instances"`
				DesiredInstances int      `json:"desired_instances"`
				Health           string   `json:"health"`
				Routes           []string `json:"routes,omitempty"`
			}{
				Name:   md.Name,
				Health: "unknown",
			}

			if app.ActiveVersion.String() != "" {
				if version, ok := versionMap[app.ActiveVersion.String()]; ok {
					appData.Version = version.Version
				}
			}

			if state, ok := poolStateMap[md.Name]; ok {
				appData.ReadyInstances = state.ready
				appData.DesiredInstances = state.desired

				if state.inCooldown {
					appData.Health = "crashed"
				} else if state.desired == 0 {
					appData.Health = "idle"
				} else if state.ready == state.desired {
					appData.Health = "healthy"
				} else if state.ready > 0 {
					appData.Health = "degraded"
				} else {
					appData.Health = "starting"
				}
			}

			if appRoutes, ok := routeMap[md.Name]; ok {
				appData.Routes = appRoutes
			}

			apps = append(apps, appData)
		}

		sort.Slice(apps, func(i, j int) bool {
			return apps[i].Name < apps[j].Name
		})

		return PrintJSON(apps)
	}

	var rows []ui.Row
	headers := []string{"NAME", "VERSION", "STATUS", "ROUTE"}

	for _, e := range res.Values() {
		var app core_v1alpha.App
		app.Decode(e.Entity())

		var md core_v1alpha.Metadata
		md.Decode(e.Entity())

		name := md.Name
		version := "-"
		status := "-"
		routeDisplay := "-"

		if app.ActiveVersion.String() != "" {
			if appVersion, ok := versionMap[app.ActiveVersion.String()]; ok {
				version = ui.DisplayAppVersion(appVersion.Version)
			}
		}

		// Runtime status from pool state
		if state, ok := poolStateMap[md.Name]; ok {
			if state.inCooldown {
				retryIn := formatDuration(state.cooldownLeft)
				status = infoRed.Render(fmt.Sprintf("crashed (%dx, retry %s)", state.crashCount, retryIn))
			} else if state.desired == 0 {
				status = infoGray.Render("💤 idle")
			} else if state.ready == state.desired {
				status = infoGreen.Render(fmt.Sprintf("%d", state.ready))
			} else {
				status = infoLabel.Render(fmt.Sprintf("%d/%d", state.ready, state.desired))
			}
		}

		// Build clickable route
		if appRoutes, ok := routeMap[md.Name]; ok && len(appRoutes) > 0 {
			host := appRoutes[0]
			displayHost := strings.ReplaceAll(host, "127.0.0.1", "localhost")
			// Use http for localhost/local domains, https for others
			scheme := "https://"
			if strings.Contains(host, "localhost") || strings.HasPrefix(host, "127.") {
				scheme = "http://"
			}
			routeDisplay = hyperlink(scheme+host, displayHost)
		}

		rows = append(rows, ui.Row{
			name,
			version,
			status,
			routeDisplay,
		})
	}

	sort.Slice(rows, func(i, j int) bool {
		return rows[i][0] < rows[j][0]
	})

	if len(rows) == 0 {
		ctx.Printf("No apps found\n")
		return nil
	}

	columns := ui.AutoSizeColumns(headers, rows, nil)
	table := ui.NewTable(
		ui.WithColumns(columns),
		ui.WithRows(rows),
	)

	ctx.Printf("%s\n", table.Render())
	return nil
}
