package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/NimbleMarkets/ntcharts/linechart/timeserieslinechart"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"miren.dev/runtime/api/app/app_v1alpha"
	"miren.dev/runtime/appconfig"
	"miren.dev/runtime/pkg/rpc/standard"
	"miren.dev/runtime/pkg/units"
)

type AppCentric struct {
	ConfigCentric

	App string `short:"a" long:"app" env:"RUNTIME_APP" description:"Application get info about"`
	Dir string `short:"d" long:"dir" description:"Directory to run from" default:"."`

	config *appconfig.AppConfig
}

func (a *AppCentric) Validate(glbl *GlobalFlags) error {
	var ac *appconfig.AppConfig
	var err error

	if a.Dir != "." {
		ac, err = appconfig.LoadAppConfigUnder(a.Dir)
	} else {
		ac, err = appconfig.LoadAppConfig()
	}

	if err == nil {
		a.config = ac
	}

	if a.App == "" {
		if a.config != nil && a.config.Name != "" {
			a.App = a.config.Name
		} else {
			return fmt.Errorf("app is required")
		}
	}

	return nil
}

func MinuteLabeler(i int, v float64) string {
	t := time.Unix(int64(v), 0).Local()
	return t.Format("15:04")
}

func App(ctx *Context, opts struct {
	AppCentric
	Watch bool `short:"w" long:"watch" description:"Watch the app stats"`
	Graph bool `short:"g" long:"graph" description:"Graph the app stats"`

	ConfigOnly bool `long:"config-only" description:"Only show the configuration"`
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

	if opts.ConfigOnly {
		data, err := json.MarshalIndent(cfgres.Configuration(), "", "  ")
		if err != nil {
			return err
		}

		ctx.Printf("%s\n", data)
		return nil
	}

	cl, err := ctx.RPCClient("dev.miren.runtime/app-status")
	if err != nil {
		return err
	}

	ac := app_v1alpha.NewAppStatusClient(cl)

	res, err := ac.AppInfo(ctx, opts.App)
	if err != nil {
		return err
	}

	status := res.Status()

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

		status: status,
		graph:  opts.Graph,
	})

	_, err = p.Run()
	return err
}

const (
	columnKeyName    = "name"
	columnKeyVersion = "version"
	columnKeyLeases  = "leases"
	columnKeyIdle    = "idle"
	columnKeyUsage   = "usage"

	colorNormal   = "#fa0"
	colorFire     = "#f64"
	colorElectric = "#ff0"
	colorWater    = "#44f"
	colorPlant    = "#8b8"
)

var (
	styleSubtle = lipgloss.NewStyle().Foreground(lipgloss.Color("#888"))

	styleBase = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#a7a")).
			BorderForeground(lipgloss.Color("#a38")).
			Align(lipgloss.Right)
)

type Model struct {
	cfg    *app_v1alpha.Configuration
	cl     *app_v1alpha.AppStatusClient
	app    string
	status *app_v1alpha.ApplicationStatus
	watch  bool

	cpu timeserieslinechart.Model
	mem timeserieslinechart.Model
	rps timeserieslinechart.Model
	max float64

	width        int
	stack, graph bool

	quitting bool
}

var randomFloat64 float64

var defaultStyle = lipgloss.NewStyle().
	BorderStyle(lipgloss.RoundedBorder()).
	BorderForeground(lipgloss.Color(lightBlue)).
	PaddingLeft(1).PaddingRight(1)

var titleStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color("3")) // yellow

var blockStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color("63")) // purple

var blockStyle2 = lipgloss.NewStyle().
	Foreground(lipgloss.Color("9")). // red
	Background(lipgloss.Color("2"))  // green

var blockStyle3 = lipgloss.NewStyle().
	Foreground(lipgloss.Color("6")). // cyan
	Background(lipgloss.Color("3"))  // yellow

var blockStyle4 = lipgloss.NewStyle().
	Foreground(lipgloss.Color("3")) // yellow

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
		} else {
			m.stack = false

			width := (msg.Width - 8) / 2

			m.cpu.Resize(width, 12)
			m.mem.Resize(width, 12)
			m.rps.Resize(msg.Width-4, 12) // Account for borders and padding
		}
	}

	res, err := m.cl.AppInfo(context.TODO(), m.app)
	if err != nil {
		return m, tea.Quit
	}

	m.cpu.Clear()
	m.mem.Clear()
	m.rps.Clear()

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

	// Add RPS data from request stats
	if status.HasRequestStats() {
		for _, s := range status.RequestStats() {
			t := standard.FromTimestamp(s.Timestamp())
			// Convert count per minute to requests per second
			rps := float64(s.Count()) / 60.0
			m.rps.Push(timeserieslinechart.TimePoint{
				Time:  t,
				Value: rps,
			})
		}
	}

	m.cpu.Draw()
	m.mem.Draw()
	m.rps.Draw()

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
	faint = lipgloss.NewStyle().Faint(true)
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

	envvars := []string{}

	if m.cfg != nil {
		for _, v := range m.cfg.EnvVars() {
			if v.Sensitive() {
				envvars = append(envvars, fmt.Sprintf("%s=****", v.Key()))
			} else {
				envvars = append(envvars, fmt.Sprintf("%s=%s", v.Key(), v.Value()))
			}
		}
	}

	sort.Strings(envvars)

	var concurrency string

	if m.cfg != nil {
		if m.cfg.HasAutoConcurrency() {
			if m.cfg.AutoConcurrency().HasFactor() {
				concurrency = fmt.Sprintf("auto (factor of %d)",
					m.cfg.AutoConcurrency().Factor(),
				)
			} else {
				concurrency = "auto"
			}
		} else if m.cfg.HasConcurrency() {
			concurrency = fmt.Sprintf("%d", m.cfg.Concurrency())
		} else {
			// Really shouldn't happen
			concurrency = "unknown"
		}
	} else {
		concurrency = "unknown"
	}

	var addons []string

	for _, a := range m.status.Addons() {
		addons = append(addons, a.Name())
	}

	hdr := fmt.Sprintf("       name: %s\nlast update: %s %s\nconcurrency: %s\n   env vars: %s\n    addons: %s\n",
		bold.Render(m.status.Name()),
		bold.Render(lastUpdate),
		laExtra,
		bold.Render(concurrency),
		bold.Render(strings.Join(envvars, ", ")),
		bold.Render(strings.Join(addons, ", ")),
	)

	for _, ps := range m.status.Pools() {
		hdr += fmt.Sprintf("       pool: %s instances=%d idle=%d\n", bold.Render(ps.Name()), len(ps.Windows()), ps.Idle())
	}

	var (
		body   string
		footer string
	)

	if !m.graph {
		cpuSamples := m.status.CpuOverHour()[len(m.status.CpuOverHour())-5:]

		var lines []string

		of := time.Kitchen
		for _, s := range cpuSamples {
			t := standard.FromTimestamp(s.Start())
			lines = append(lines, fmt.Sprintf("%s: %.3f", t.Format(of), s.Cores()))
		}

		memSamples := m.status.MemoryOverHour()[len(m.status.MemoryOverHour())-5:]

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

			// Add current RPS
			if m.status.HasRequestsPerSecond() {
				rpsStr := fmt.Sprintf("%.2f", m.status.RequestsPerSecond())
				httpLines = append(httpLines, fmt.Sprintf("Current: %s RPS (last minute)", bold.Render(rpsStr)))
				httpLines = append(httpLines, "") // blank line
			}

			// Add hourly stats
			if m.status.HasRequestStats() && len(m.status.RequestStats()) > 0 {
				httpLines = append(httpLines, "Last hour:")

				// Get last 5 entries
				stats := m.status.RequestStats()
				startIdx := 0
				if len(stats) > 5 {
					startIdx = len(stats) - 5
				}

				for _, s := range stats[startIdx:] {
					t := standard.FromTimestamp(s.Timestamp())
					errorRateStr := fmt.Sprintf("%.1f%%", s.ErrorRate()*100)
					if s.ErrorRate() == 0 {
						errorRateStr = "0%"
					}
					line := fmt.Sprintf("%s: %d reqs, %dms avg, %s errors",
						t.Format(of),
						s.Count(),
						int(s.AvgDurationMs()),
						errorRateStr)
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
		)
		footer =
			lipgloss.NewStyle().Width(m.width).Align(lipgloss.Right).Render(
				fmt.Sprintf("Updated: %s", time.Now().Format(Stamp)),
			)

	} else {
		// In horizontal mode, show CPU and Memory side by side, RPS below
		topRow := lipgloss.JoinHorizontal(lipgloss.Top,
			defaultStyle.Render(titleStyle.Render("   CPU (cores)")+"\n"+m.cpu.View()),
			defaultStyle.Render(titleStyle.Render("   Memory (MB)")+"\n"+m.mem.View()),
		)
		body = lipgloss.JoinVertical(lipgloss.Top,
			topRow,
			defaultStyle.Render(titleStyle.Render("   Requests/sec")+"\n"+m.rps.View()),
		)
		footer =
			lipgloss.NewStyle().Width(m.width - 3).Align(lipgloss.Right).Faint(true).Render(
				fmt.Sprintf("Updated: %s", time.Now().Format(Stamp)),
			)
	}

	frame := lipgloss.JoinVertical(lipgloss.Top, hdr, body, footer)
	if m.quitting {
		frame += "\n"
	}

	return frame
}
