package commands

import (
	"errors"
	"fmt"
	"net"
	"sort"
	"strings"
	"time"

	"miren.dev/runtime/api/app/app_v1alpha"
	"miren.dev/runtime/clientconfig"
	"miren.dev/runtime/pkg/ui"
)

func AppList(ctx *Context, opts struct {
	FormatOptions
	ConfigCentric
}) error {
	cluster, _, err := opts.LoadCluster()
	if err != nil {
		if errors.Is(err, clientconfig.ErrNoConfig) || errors.Is(err, ErrNoConfig) {
			ctx.Printf("No cluster configured\n")
			ctx.Printf("\nUse 'miren cluster add' to add a cluster\n")
			return nil
		}
		return err
	}
	if cluster == nil {
		ctx.Printf("No cluster configured\n")
		ctx.Printf("\nUse 'miren cluster add' to add a cluster\n")
		return nil
	}

	crudcl, err := ctx.RPCClient("dev.miren.runtime/app")
	if err != nil {
		return err
	}
	defer crudcl.Close()

	crud := app_v1alpha.NewCrudClient(crudcl)
	result, err := crud.List(ctx)
	if err != nil {
		return err
	}

	appList := result.Apps()

	// Get default hostname for routes display
	defaultHost := ""
	if cluster.Hostname != "" {
		defaultHost = cluster.Hostname
		if h, _, err := net.SplitHostPort(defaultHost); err == nil {
			defaultHost = h
		}
	}

	if opts.IsJSON() {
		var apps []struct {
			Name             string   `json:"name"`
			Version          string   `json:"version,omitempty"`
			ReadyInstances   int32    `json:"ready_instances"`
			DesiredInstances int32    `json:"desired_instances"`
			Health           string   `json:"health"`
			ScalingMode      string   `json:"scaling_mode,omitempty"`
			Routes           []string `json:"routes,omitempty"`
			CrashCount       int64    `json:"crash_count"`
			CooldownSeconds  int32    `json:"cooldown_seconds"`

			MaintenanceRoutes []string `json:"maintenance_routes,omitempty"`
		}

		for _, a := range appList {
			appData := struct {
				Name             string   `json:"name"`
				Version          string   `json:"version,omitempty"`
				ReadyInstances   int32    `json:"ready_instances"`
				DesiredInstances int32    `json:"desired_instances"`
				Health           string   `json:"health"`
				ScalingMode      string   `json:"scaling_mode,omitempty"`
				Routes           []string `json:"routes,omitempty"`
				CrashCount       int64    `json:"crash_count"`
				CooldownSeconds  int32    `json:"cooldown_seconds"`

				MaintenanceRoutes []string `json:"maintenance_routes,omitempty"`
			}{
				Name:             a.Name(),
				Health:           a.Health(),
				ReadyInstances:   a.ReadyInstances(),
				DesiredInstances: a.DesiredInstances(),
				ScalingMode:      a.ScalingMode(),
				CrashCount:       a.CrashCount(),
				CooldownSeconds:  a.CooldownSeconds(),

				MaintenanceRoutes: resolveRoutes(a.MaintenanceRoutes(), defaultHost),
			}

			if a.HasCurrentVersion() {
				appData.Version = a.CurrentVersion().Version()
			}

			routes := resolveRoutes(a.Routes(), defaultHost)
			if len(routes) > 0 {
				appData.Routes = routes
			}

			apps = append(apps, appData)
		}

		sort.Slice(apps, func(i, j int) bool {
			return apps[i].Name < apps[j].Name
		})

		return PrintJSON(apps)
	}

	var rows []ui.Row
	headers := []string{"NAME", "VERSION", "SCALE", "ROUTE"}

	for _, a := range appList {
		name := a.Name()
		version := "-"
		status := "-"
		routeDisplay := "-"

		if a.HasCurrentVersion() {
			cv := a.CurrentVersion()
			if cv.HasShortId() && cv.ShortId() != "" {
				version = cv.ShortId()
			} else {
				version = ui.DisplayAppVersion(cv.Version())
			}
		}

		// Runtime status from server-aggregated pool state
		if a.HasHealth() && a.Health() != "unknown" {
			modeSuffix := " (auto)"
			if a.ScalingMode() == "fixed" {
				modeSuffix = " (fixed)"
			}

			switch a.Health() {
			case "crashed":
				retryIn := formatDuration(time.Duration(a.CooldownSeconds()) * time.Second)
				status = infoRed.Render(fmt.Sprintf("crashed (%dx, retry %s)", a.CrashCount(), retryIn))
			case "idle":
				if a.ScalingMode() == "auto" {
					status = infoGray.Render("💤 idle")
				} else {
					status = infoYellow.Render("⚠️ 0 (fixed)")
				}
			case "healthy":
				status = infoGreen.Render(fmt.Sprintf("%d%s", a.ReadyInstances(), modeSuffix))
			case "degraded", "starting":
				status = infoLabel.Render(fmt.Sprintf("%d/%d%s", a.ReadyInstances(), a.DesiredInstances(), modeSuffix))
			}
		}

		// Build clickable route
		routes := resolveRoutes(a.Routes(), defaultHost)
		if len(routes) > 0 {
			host := routes[0]
			displayHost := strings.ReplaceAll(host, "127.0.0.1", "localhost")
			scheme := "https://"
			if strings.Contains(host, "localhost") || strings.HasPrefix(host, "127.") {
				scheme = "http://"
			}
			routeDisplay = ui.Hyperlink(scheme+host, displayHost)
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

// resolveRoutes fills in the default host for default routes (empty strings).
func resolveRoutes(routes []string, defaultHost string) []string {
	if len(routes) == 0 {
		return nil
	}
	var resolved []string
	for _, host := range routes {
		if host != "" {
			resolved = append(resolved, host)
		} else if defaultHost != "" {
			resolved = append(resolved, defaultHost)
		}
	}
	return resolved
}
