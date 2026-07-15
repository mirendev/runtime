package commands

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/NimbleMarkets/ntcharts/linechart/timeserieslinechart"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"

	"miren.dev/runtime/api/app/app_v1alpha"
	"miren.dev/runtime/appconfig"
	"miren.dev/runtime/clientconfig"
	"miren.dev/runtime/pkg/rpc/standard"
	"miren.dev/runtime/pkg/theme"
	"miren.dev/runtime/pkg/ui"
	"miren.dev/runtime/pkg/units"
)

type AppCentric struct {
	ConfigCentric `group:"Config Options"`

	_ struct{} `group:"App Options"`

	// App carries only the -a/--app flag. MIREN_APP is deliberately not an
	// env tag here: mflags applies env tags in FromStruct, before Validate
	// runs, which would leave Validate unable to tell an explicit flag from
	// the environment. Validate reads MIREN_APP itself instead, below the
	// config file. See Validate for the resolution order.
	App string `short:"a" long:"app" description:"Application name (defaults to .miren/app.toml, then $MIREN_APP)"`
	Dir string `short:"d" long:"dir" description:"Directory to run from" default:"."`

	config        *appconfig.AppConfig
	resolvedDir   string // absolute path to the directory containing .miren/app.toml
	foundInParent bool   // true when config was found by walking up from CWD
}

// ResolvedDir returns the directory that should be used as the app source.
// When a config is found in a parent directory, this returns that parent
// directory instead of the original Dir value.
func (a *AppCentric) ResolvedDir() string {
	if a.resolvedDir != "" {
		return a.resolvedDir
	}
	return a.Dir
}

// Validate loads .miren/app.toml and resolves the target app name. Resolution
// priority:
//  1. -a/--app flag (explicit override)
//  2. name in .miren/app.toml
//  3. MIREN_APP env var
//  4. inferred from the directory name, via a 'miren init' prompt
//
// The config file outranks MIREN_APP because every app sandbox exports
// MIREN_APP=<its own app>; without this ordering, deploying from inside a
// sandbox would silently target the sandbox's app rather than the app.toml in
// the working directory. Note this differs from LoadCluster below, where
// MIREN_CLUSTER still outranks stored per-app state.
func (a *AppCentric) Validate(glbl *GlobalFlags) error {
	a.config = nil
	a.resolvedDir = ""
	a.foundInParent = false

	var ac *appconfig.AppConfig
	var err error

	if a.Dir != "." {
		ac, err = appconfig.LoadAppConfigUnder(a.Dir)
	} else {
		var configPath string
		ac, configPath, err = appconfig.LoadAppConfigWithPath()
		if err == nil && ac != nil && configPath != "" {
			// Config was found — always record the resolved directory.
			configDir := filepath.Dir(filepath.Dir(configPath)) // strip .miren/app.toml
			a.resolvedDir = configDir
			if cwd, wdErr := os.Getwd(); wdErr == nil && configDir != cwd {
				a.foundInParent = true
			}
		}
	}

	if err != nil {
		return fmt.Errorf("error loading %s: %w", appconfig.AppConfigPath, err)
	}

	a.config = ac

	if a.App == "" {
		if a.config != nil && a.config.Name != "" {
			a.App = a.config.Name
		} else if envApp := os.Getenv("MIREN_APP"); envApp != "" {
			a.App = envApp
		} else {
			// No app name from flag, config, or env — try to help the user.
			workDir := a.Dir
			if workDir == "." || workDir == "" {
				wd, err := os.Getwd()
				if err != nil {
					return fmt.Errorf("no app configuration found — run 'miren init' to get started, or pass -a <name>")
				}
				workDir = wd
			} else {
				absDir, err := filepath.Abs(workDir)
				if err == nil {
					workDir = absDir
				}
			}

			appName := inferAppName(workDir)

			noAppMsg := "no app configuration found — run 'miren init' to get started, or pass -a <name>"

			if !term.IsTerminal(int(os.Stdin.Fd())) {
				return fmt.Errorf("%s", noAppMsg)
			}

			confirmed, err := ui.Confirm(
				ui.WithMessage(fmt.Sprintf("Looks like this directory isn't set up yet. Run 'miren init' to create app %q here?", appName)),
				ui.WithDefault(true),
				ui.WithIndent("  "),
			)
			if err != nil || !confirmed {
				return fmt.Errorf("%s", noAppMsg)
			}

			if _, err := initApp(workDir, appName); err != nil {
				return fmt.Errorf("failed to initialize app: %w", err)
			}

			// Reload the config we just created. workDir is already absolute.
			a.config, err = appconfig.LoadAppConfigUnder(workDir)
			if err != nil {
				return fmt.Errorf("error loading %s: %w", appconfig.AppConfigPath, err)
			}

			a.App = appName
		}
	}

	return nil
}

// LoadCluster implements per-app cluster pinning. Resolution priority:
//  1. -C flag (explicit override) — also saves to state
//  2. MIREN_CLUSTER env var
//  3. Per-app cluster from ~/.config/miren/app-state.toml (handled by ConfigCentric)
//  4. Global active_cluster from clientconfig (fallback)
func (a *AppCentric) LoadCluster() (*clientconfig.ClusterConfig, string, error) {
	// If -C flag was passed, use ConfigCentric's logic and persist the choice.
	if a.Cluster != "" {
		cc, name, err := a.ConfigCentric.LoadCluster()
		if err == nil && cc != nil {
			a.saveClusterState(name)
		}
		return cc, name, err
	}

	// Delegate to ConfigCentric which now handles per-app state.
	return a.ConfigCentric.LoadCluster()
}

func (a *AppCentric) saveClusterState(name string) {
	if a.App == "" {
		return
	}
	_ = appconfig.SaveAppState(a.App, &appconfig.AppState{Cluster: name})
}

func MinuteLabeler(i int, v float64) string {
	t := time.Unix(int64(v), 0).Local()
	return t.Format("15:04")
}

func App(ctx *Context, opts struct {
	AppCentric
	FormatOptions
	Watch bool `short:"w" long:"watch" description:"Watch the app stats"`
	Graph bool `short:"g" long:"graph" description:"Graph the app stats"`
}) error {
	crudcl, err := ctx.RPCClient("dev.miren.runtime/app")
	if err != nil {
		return err
	}

	crud := app_v1alpha.NewCrudClient(crudcl)

	cfgres, err := crud.GetConfiguration(ctx, opts.App)
	if err != nil {
		ctx.Printf("unknown application: %s\n", opts.App)
		return nil
	}

	cl, err := ctx.RPCClient(rpcAppStatus)
	if err != nil {
		return err
	}

	ac := app_v1alpha.NewAppStatusClient(cl)

	res, err := ac.AppInfo(ctx, opts.App)
	if err != nil {
		return err
	}

	status := res.Status()

	if opts.IsJSON() {
		return printAppJSON(status)
	}

	//spew.Dump(status)
	//windows := status.Pools()

	p := tea.NewProgram(Model{
		cfg:   cfgres.Configuration(),
		cl:    ac,
		app:   opts.App,
		watch: opts.Watch,
		cpu: timeserieslinechart.New(60, 12,
			timeserieslinechart.WithXLabelFormatter(MinuteLabeler),
			timeserieslinechart.WithYLabelFormatter(func(i int, v float64) string {
				return fmt.Sprintf("%.3f", v/1000)
			}),
		),
		mem: timeserieslinechart.New(60, 12,
			timeserieslinechart.WithXLabelFormatter(MinuteLabeler),
		),
		rps: timeserieslinechart.New(60, 12,
			timeserieslinechart.WithXLabelFormatter(MinuteLabeler),
			timeserieslinechart.WithYLabelFormatter(func(i int, v float64) string {
				return fmt.Sprintf("%.1f", v)
			}),
		),
		errorRate: timeserieslinechart.New(60, 12,
			timeserieslinechart.WithXLabelFormatter(MinuteLabeler),
			timeserieslinechart.WithYLabelFormatter(func(i int, v float64) string {
				return fmt.Sprintf("%.1f%%", v)
			}),
		),

		status: status,
		graph:  opts.Graph,
	})

	_, err = p.Run()
	return err
}

type Model struct {
	cfg    *app_v1alpha.Configuration
	cl     *app_v1alpha.AppStatusClient
	app    string
	status *app_v1alpha.ApplicationStatus
	watch  bool

	cpu       timeserieslinechart.Model
	mem       timeserieslinechart.Model
	rps       timeserieslinechart.Model
	errorRate timeserieslinechart.Model

	width        int
	stack, graph bool

	quitting bool
}

var defaultStyle = lipgloss.NewStyle().
	BorderStyle(lipgloss.RoundedBorder()).
	BorderForeground(lipgloss.Color(lightBlue)).
	PaddingLeft(1).PaddingRight(1)

var titleStyle = lipgloss.NewStyle().
	Foreground(theme.Header) // yellow

func (m Model) Init() tea.Cmd {
	return nil
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			m.quitting = true
		}
	case tea.WindowSizeMsg:
		m.width = msg.Width

		if msg.Width < 50 {
			m.stack = true

			m.cpu.Resize(msg.Width, 12)
			m.mem.Resize(msg.Width, 12)
			m.rps.Resize(msg.Width, 12)
			m.errorRate.Resize(msg.Width, 12)
		} else {
			m.stack = false

			width := (msg.Width - 8) / 2

			m.cpu.Resize(width, 12)
			m.mem.Resize(width, 12)
			m.rps.Resize(width, 12)
			m.errorRate.Resize(width, 12)
		}
	}

	res, err := m.cl.AppInfo(context.TODO(), m.app)
	if err != nil {
		return m, tea.Quit
	}

	m.cpu.Clear()
	m.mem.Clear()
	m.rps.Clear()
	m.errorRate.Clear()

	status := res.Status()
	m.status = status

	for _, s := range status.CpuOverHour() {
		t := standard.FromTimestamp(s.Start())
		m.cpu.Push(timeserieslinechart.TimePoint{
			Time:  t,
			Value: s.Cores() * 1000,
		})
	}

	for _, s := range status.MemoryOverHour() {
		t := standard.FromTimestamp(s.Timestamp())
		by := units.Bytes(s.Bytes())

		m.mem.Push(timeserieslinechart.TimePoint{
			Time:  t,
			Value: float64(by.MegaBytes()),
		})
	}

	// Add RPS and error rate data from request stats
	if status.HasRequestStats() {
		for _, s := range status.RequestStats() {
			t := standard.FromTimestamp(s.Timestamp())
			// Convert count per minute to requests per second
			rps := float64(s.Count()) / 60.0
			m.rps.Push(timeserieslinechart.TimePoint{
				Time:  t,
				Value: rps,
			})
			// Error rate as percentage
			m.errorRate.Push(timeserieslinechart.TimePoint{
				Time:  t,
				Value: s.ErrorRate() * 100,
			})
		}
	}

	m.cpu.Draw()
	m.mem.Draw()
	m.rps.Draw()
	m.errorRate.Draw()

	if !m.watch || m.quitting {
		return m, tea.Quit
	}

	return m, tea.Tick(10*time.Second, func(t time.Time) tea.Msg {
		return TickMsg{Time: t}
	})
}

type TickMsg struct {
	Time time.Time
}

const (
	format = "2006-01-02 15:04:05.999999999 -0700 MST"
	Stamp  = "Jan _2 03:04:05PM"
)

var (
	bold  = lipgloss.NewStyle().Bold(true)
	faint = lipgloss.NewStyle().Foreground(theme.Muted)
)

func (m Model) View() string {
	var (
		lastUpdate string
		laExtra    string
	)

	t := standard.FromTimestamp(m.status.LastDeploy())
	if t.IsZero() {
		lastUpdate = "never"
	} else {
		lastUpdate = t.Format(format)
		laExtra = faint.Render(
			fmt.Sprintf("(%s ago, %s)",
				time.Since(t).Round(time.Second),
				m.status.ActiveVersion(),
			))
	}

	hdr := fmt.Sprintf("       name: %s\nlast update: %s %s\n",
		bold.Render(m.status.Name()),
		bold.Render(lastUpdate),
		laExtra,
	)

	for _, ps := range m.status.Pools() {
		instanceCount := len(ps.Windows())

		isFixed := false
		if m.cfg != nil {
			for _, svc := range m.cfg.Services() {
				if svc.Service() == ps.Name() && svc.ConcurrencyMode() == "fixed" {
					isFixed = true
					break
				}
			}
		}

		var instanceInfo string
		if isFixed {
			instanceInfo = fmt.Sprintf("instances=%d (fixed)", instanceCount)
		} else if instanceCount == 0 {
			instanceInfo = "instances=0 (idle, will scale on traffic)"
		} else {
			instanceInfo = fmt.Sprintf("instances=%d (auto)", instanceCount)
		}

		hdr += fmt.Sprintf("       pool: %s %s\n", bold.Render(ps.Name()), instanceInfo)
	}

	var (
		body   string
		footer string
	)

	if !m.graph {
		cpuSamples := m.status.CpuOverHour()
		if len(cpuSamples) > 5 {
			cpuSamples = cpuSamples[len(m.status.CpuOverHour())-5:]
		}

		var lines []string

		of := time.Kitchen
		for _, s := range cpuSamples {
			t := standard.FromTimestamp(s.Start())
			lines = append(lines, fmt.Sprintf("%s: %.3f", t.Format(of), s.Cores()))
		}

		memSamples := m.status.MemoryOverHour()
		if len(memSamples) > 5 {
			memSamples = memSamples[len(m.status.MemoryOverHour())-5:]
		}

		var memlines []string

		for _, s := range memSamples {
			t := standard.FromTimestamp(s.Timestamp())

			b := units.Bytes(s.Bytes())

			memlines = append(memlines, fmt.Sprintf("%s: %s", t.Format(of), b.Short()))
		}

		// Add HTTP stats if available
		var httpStatsSection string
		if m.status.HasRequestsPerSecond() || (m.status.HasRequestStats() && len(m.status.RequestStats()) > 0) {
			var httpLines []string
			httpLines = append(httpLines, titleStyle.Render("HTTP Stats"))

			// Add current RPS and latest percentiles
			if m.status.HasRequestsPerSecond() {
				rpsStr := fmt.Sprintf("%.2f", m.status.RequestsPerSecond())
				httpLines = append(httpLines, fmt.Sprintf("Current: %s RPS (last minute)", bold.Render(rpsStr)))

				// Show latest percentiles if available
				if m.status.HasRequestStats() && len(m.status.RequestStats()) > 0 {
					latest := m.status.RequestStats()[len(m.status.RequestStats())-1]
					if latest.HasP95DurationMs() && latest.HasP99DurationMs() {
						httpLines = append(httpLines, fmt.Sprintf("Latency: %s avg, %s P95, %s P99",
							bold.Render(fmt.Sprintf("%dms", int(latest.AvgDurationMs()))),
							bold.Render(fmt.Sprintf("%dms", int(latest.P95DurationMs()))),
							bold.Render(fmt.Sprintf("%dms", int(latest.P99DurationMs())))))
					}
				}
				httpLines = append(httpLines, "") // blank line
			}

			// Add hourly stats
			if m.status.HasRequestStats() && len(m.status.RequestStats()) > 0 {
				httpLines = append(httpLines, "Recent activity:")

				// Get last 5 entries
				stats := m.status.RequestStats()
				startIdx := 0
				if len(stats) > 5 {
					startIdx = len(stats) - 5
				}

				for _, s := range stats[startIdx:] {
					t := standard.FromTimestamp(s.Timestamp())

					// Parenthetical style format
					errorStr := ""
					if s.ErrorRate() > 0 {
						errorStr = fmt.Sprintf(" | %s errors", bold.Render(fmt.Sprintf("%.1f%%", s.ErrorRate()*100)))
					}

					httpLines = append(httpLines, fmt.Sprintf("  %s: %s reqs | %sms avg (P95: %sms, P99: %sms)%s",
						t.Format(of),
						bold.Render(fmt.Sprintf("%d", s.Count())),
						bold.Render(fmt.Sprintf("%d", int(s.AvgDurationMs()))),
						bold.Render(fmt.Sprintf("%d", int(s.P95DurationMs()))),
						bold.Render(fmt.Sprintf("%d", int(s.P99DurationMs()))),
						errorStr))
				}
			}

			// Add error breakdown if available
			if m.status.HasErrorBreakdown() && len(m.status.ErrorBreakdown()) > 0 {
				httpLines = append(httpLines, "") // blank line
				httpLines = append(httpLines, "Error breakdown (last hour):")

				for _, e := range m.status.ErrorBreakdown() {
					statusText := fmt.Sprintf("%d", e.StatusCode())
					// Add common status code descriptions
					switch e.StatusCode() {
					case 400:
						statusText += " Bad Request"
					case 401:
						statusText += " Unauthorized"
					case 403:
						statusText += " Forbidden"
					case 404:
						statusText += " Not Found"
					case 429:
						statusText += " Too Many Requests"
					case 500:
						statusText += " Internal Error"
					case 502:
						statusText += " Bad Gateway"
					case 503:
						statusText += " Service Unavail"
					case 504:
						statusText += " Gateway Timeout"
					}

					line := fmt.Sprintf("  %-20s (%d reqs, %.1f%%)",
						statusText,
						e.Count(),
						e.Percentage())
					httpLines = append(httpLines, line)
				}
			}

			// Add top paths if available
			if m.status.HasTopPaths() && len(m.status.TopPaths()) > 0 {
				httpLines = append(httpLines, "") // blank line
				httpLines = append(httpLines, "Top paths:")

				for _, p := range m.status.TopPaths() {
					errorStr := ""
					if p.ErrorRate() > 0 {
						errorStr = fmt.Sprintf(", %.1f%% errors", p.ErrorRate()*100)
					}
					line := fmt.Sprintf("  %-20s (%d reqs, %dms avg%s)",
						p.Path(),
						p.Count(),
						int(p.AvgDurationMs()),
						errorStr)
					httpLines = append(httpLines, line)
				}
			}

			httpStatsSection = defaultStyle.Render(strings.Join(httpLines, "\n"))
		}

		// Join all sections
		sections := []string{
			defaultStyle.Render(titleStyle.Render("CPU (cores)") + "\n" + strings.Join(lines, "\n")),
			defaultStyle.Render(titleStyle.Render("Memory (MB)") + "\n" + strings.Join(memlines, "\n")),
		}

		if httpStatsSection != "" {
			body = lipgloss.JoinVertical(lipgloss.Top,
				lipgloss.JoinHorizontal(lipgloss.Top, sections[0], sections[1]),
				httpStatsSection,
			)
		} else {
			body = lipgloss.JoinHorizontal(lipgloss.Top, sections...)
		}
	} else if m.stack {
		body = lipgloss.JoinVertical(lipgloss.Top,
			defaultStyle.Render(titleStyle.Render("   CPU (cores)")+"\n"+m.cpu.View()),
			defaultStyle.Render(titleStyle.Render("   Memory (MB)")+"\n"+m.mem.View()),
			defaultStyle.Render(titleStyle.Render("   Requests/sec")+"\n"+m.rps.View()),
			defaultStyle.Render(titleStyle.Render("   Error Rate %")+"\n"+m.errorRate.View()),
		)
		footer =
			lipgloss.NewStyle().Width(m.width).Align(lipgloss.Right).Render(
				fmt.Sprintf("Updated: %s", time.Now().Format(Stamp)),
			)

	} else {
		// In horizontal mode, show CPU and Memory side by side, RPS and Error Rate side by side
		topRow := lipgloss.JoinHorizontal(lipgloss.Top,
			defaultStyle.Render(titleStyle.Render("   CPU (cores)")+"\n"+m.cpu.View()),
			defaultStyle.Render(titleStyle.Render("   Memory (MB)")+"\n"+m.mem.View()),
		)
		bottomRow := lipgloss.JoinHorizontal(lipgloss.Top,
			defaultStyle.Render(titleStyle.Render("   Requests/sec")+"\n"+m.rps.View()),
			defaultStyle.Render(titleStyle.Render("   Error Rate %")+"\n"+m.errorRate.View()),
		)
		body = lipgloss.JoinVertical(lipgloss.Top,
			topRow,
			bottomRow,
		)
		footer =
			faint.Width(m.width - 3).Align(lipgloss.Right).Render(
				fmt.Sprintf("Updated: %s", time.Now().Format(Stamp)),
			)
	}

	frame := lipgloss.JoinVertical(lipgloss.Top, hdr, body, footer)
	if m.quitting {
		frame += "\n"
	}

	return frame
}

func printAppJSON(status *app_v1alpha.ApplicationStatus) error {
	type poolJSON struct {
		Name      string `json:"name"`
		Instances int    `json:"instances"`
		Idle      int32  `json:"idle"`
	}

	type requestStatJSON struct {
		Timestamp     string  `json:"timestamp"`
		Count         int64   `json:"count"`
		AvgDurationMs float64 `json:"avg_duration_ms"`
		P95DurationMs float64 `json:"p95_duration_ms,omitempty"`
		P99DurationMs float64 `json:"p99_duration_ms,omitempty"`
		ErrorRate     float64 `json:"error_rate"`
	}

	type pathStatJSON struct {
		Path          string  `json:"path"`
		Count         int64   `json:"count"`
		AvgDurationMs float64 `json:"avg_duration_ms"`
		ErrorRate     float64 `json:"error_rate"`
	}

	type errorBreakdownJSON struct {
		StatusCode int32   `json:"status_code"`
		Count      int64   `json:"count"`
		Percentage float64 `json:"percentage"`
	}

	type output struct {
		Name              string               `json:"name"`
		ActiveVersion     string               `json:"active_version,omitempty"`
		LastDeploy        string               `json:"last_deploy,omitempty"`
		RequestsPerSecond float64              `json:"requests_per_second,omitempty"`
		LastMinCPU        float64              `json:"last_min_cpu,omitempty"`
		LastHourCPU       float64              `json:"last_hour_cpu,omitempty"`
		Pools             []poolJSON           `json:"pools,omitempty"`
		RequestStats      []requestStatJSON    `json:"request_stats,omitempty"`
		TopPaths          []pathStatJSON       `json:"top_paths,omitempty"`
		ErrorBreakdown    []errorBreakdownJSON `json:"error_breakdown,omitempty"`
	}

	o := output{
		Name: status.Name(),
	}

	if status.HasActiveVersion() {
		o.ActiveVersion = status.ActiveVersion()
	}

	if status.HasLastDeploy() && status.LastDeploy() != nil {
		t := standard.FromTimestamp(status.LastDeploy())
		if !t.IsZero() {
			o.LastDeploy = t.UTC().Format(time.RFC3339)
		}
	}

	if status.HasRequestsPerSecond() {
		o.RequestsPerSecond = status.RequestsPerSecond()
	}

	if status.HasLastMinCPU() {
		o.LastMinCPU = status.LastMinCPU()
	}

	if status.HasLastHourCPU() {
		o.LastHourCPU = status.LastHourCPU()
	}

	for _, ps := range status.Pools() {
		o.Pools = append(o.Pools, poolJSON{
			Name:      ps.Name(),
			Instances: len(ps.Windows()),
			Idle:      ps.Idle(),
		})
	}

	if status.HasRequestStats() {
		// Collect non-zero samples, then keep only the last 5
		var stats []requestStatJSON
		for _, s := range status.RequestStats() {
			if s.Count() == 0 {
				continue
			}
			rs := requestStatJSON{
				Count:         s.Count(),
				AvgDurationMs: s.AvgDurationMs(),
				ErrorRate:     s.ErrorRate(),
			}
			if s.HasTimestamp() && s.Timestamp() != nil {
				rs.Timestamp = standard.FromTimestamp(s.Timestamp()).UTC().Format(time.RFC3339)
			}
			if s.HasP95DurationMs() {
				rs.P95DurationMs = s.P95DurationMs()
			}
			if s.HasP99DurationMs() {
				rs.P99DurationMs = s.P99DurationMs()
			}
			stats = append(stats, rs)
		}
		if len(stats) > 5 {
			stats = stats[len(stats)-5:]
		}
		o.RequestStats = stats
	}

	if status.HasTopPaths() {
		for _, p := range status.TopPaths() {
			o.TopPaths = append(o.TopPaths, pathStatJSON{
				Path:          p.Path(),
				Count:         p.Count(),
				AvgDurationMs: p.AvgDurationMs(),
				ErrorRate:     p.ErrorRate(),
			})
		}
	}

	if status.HasErrorBreakdown() {
		for _, e := range status.ErrorBreakdown() {
			o.ErrorBreakdown = append(o.ErrorBreakdown, errorBreakdownJSON{
				StatusCode: e.StatusCode(),
				Count:      e.Count(),
				Percentage: e.Percentage(),
			})
		}
	}

	return PrintJSON(o)
}
