package commands

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"

	"miren.dev/runtime/api/usage/usage_v1alpha"
	"miren.dev/runtime/pkg/rpc/standard"
	"miren.dev/runtime/pkg/theme"
	"miren.dev/runtime/pkg/ui"
	"miren.dev/runtime/pkg/units"
)

// ServiceUsage is the cluster-wide resource usage service, served only by the
// coordinator.
const ServiceUsage = "dev.miren.runtime/usage"

// mutedStyle renders the trailing summary and warning lines that sit under a
// table, keeping them subordinate to the rows themselves. It matches the muted
// label colour the other detail views use rather than borrowing doctor's
// palette, which is scoped to that command's own output.
var mutedStyle = lipgloss.NewStyle().Foreground(theme.Muted)

// warnStyle marks a line reporting something the server could not answer.
var warnStyle = lipgloss.NewStyle().Foreground(theme.Warning)

// minWatchInterval floors how often a watch loop refreshes.
//
// Numbers reaching this command are already a few seconds old -- the collectors
// sample once a second, the writer batches, and the query engine deliberately
// lags to let late points land. Refreshing faster than that redraws the same
// figures repeatedly while making a query per redraw, so the floor protects the
// cluster from a watcher that gains nothing by asking harder.
const minWatchInterval = 2 * time.Second

type topOptions struct {
	FormatOptions
	ConfigCentric

	App     string `long:"app" description:"Only show sandboxes belonging to this app"`
	Service string `long:"service" description:"Only show sandboxes of this service (e.g. web, worker)"`
	Runner  string `long:"runner" description:"Only show sandboxes on this runner (name, ID, or short ID)"`
	Kind    string `long:"kind" description:"Only show sandboxes of this kind (app, addon, run)"`
	Status  string `long:"status" description:"Only show sandboxes in this status"`

	Nodes  bool `long:"nodes" description:"Show per-node usage instead of per-sandbox"`
	Apps   bool `long:"apps" description:"Show per-app totals instead of per-sandbox"`
	System bool `long:"system" description:"Include addon and platform sandboxes"`

	NoAddons bool `long:"no-addons" description:"With --apps, exclude each app's dedicated addons from its total"`

	Since     string `long:"since" description:"Measure over this window, e.g. 30s, 5m, 1h (default 1m)"`
	Aggregate string `long:"aggregate" description:"How to collapse the window: avg, max, min, last" default:"avg"`

	Sort  string `long:"sort" description:"Sort by: cpu, memory, name, app, service, node" default:"cpu"`
	Order string `long:"order" description:"Sort direction: desc or asc (default: desc for usage, asc for names)"`
	Limit int    `long:"limit" description:"Show at most this many rows (0 for all)"`

	Watch    bool   `short:"w" long:"watch" description:"Refresh continuously until interrupted"`
	Interval string `long:"interval" description:"Refresh interval when watching" default:"5s"`
	Samples  int    `long:"samples" description:"Stop after this many refreshes (0 for unlimited)"`
}

// Top reports what a cluster is spending its CPU and memory on.
func Top(ctx *Context, opts topOptions) error {
	client, err := ctx.RPCClient(ServiceUsage)
	if err != nil {
		return err
	}
	defer client.Close()

	uc := usage_v1alpha.NewResourceUsageClient(client)

	lookback, err := parseTopDuration(opts.Since)
	if err != nil {
		return fmt.Errorf("--since: %w", err)
	}

	q := topQuery{client: uc, opts: opts, lookback: lookback}

	// JSON is a single snapshot even under --watch: a stream of whole documents
	// is not something a consumer can parse, and the flags contradict rather
	// than compose.
	if opts.IsJSON() {
		return q.renderJSON(ctx)
	}

	// --samples implies watching: asking for six of something is asking for it
	// more than once, and requiring --watch alongside it would only be a way to
	// get the flag combination wrong.
	if !opts.Watch && opts.Samples <= 1 {
		out, err := q.render(ctx)
		if err != nil {
			return err
		}
		ctx.Printf("%s", out)
		return nil
	}

	interval, err := time.ParseDuration(opts.Interval)
	if err != nil {
		return fmt.Errorf("--interval: %w", err)
	}
	if interval < minWatchInterval {
		interval = minWatchInterval
	}

	// Two cases print successive snapshots instead of redrawing one in place.
	//
	// A bounded run keeps all of its samples, because the point of asking for
	// six of them is to compare them. And a run with no terminal has nothing to
	// redraw in: piped to a file or called from a script, the TUI library fails
	// with an error about /dev/tty that tells the reader nothing about what to
	// do, so it falls back the way top -b does.
	if opts.Samples > 0 || !term.IsTerminal(int(os.Stdout.Fd())) {
		return q.watchBatch(ctx, interval, opts.Samples)
	}

	return q.watch(ctx, interval)
}

// watchBatch prints a fresh snapshot every interval, separated by a timestamp.
// It runs samples times, or until interrupted when samples is zero. Used for a
// bounded run, and whenever there is no terminal to redraw in.
func (q topQuery) watchBatch(ctx *Context, interval time.Duration, samples int) error {
	for n := 0; samples <= 0 || n < samples; n++ {
		out, err := q.render(ctx)
		if err != nil {
			return err
		}

		ctx.Printf("%s\n%s\n", mutedStyle.Render(time.Now().Format(time.RFC3339)), out)

		if samples > 0 && n == samples-1 {
			return nil
		}

		select {
		case <-ctx.Done():
			return nil
		case <-time.After(interval):
		}
	}

	return nil
}

// topQuery holds everything a single refresh needs, so the watch loop and the
// one-shot path render through exactly the same code.
type topQuery struct {
	client   *usage_v1alpha.ResourceUsageClient
	opts     topOptions
	lookback time.Duration
}

// selector, window and ordering build the three request groups once, so all
// three views ask the same way and a new flag has one place to land.
func (q topQuery) selector() *usage_v1alpha.Selector {
	o := q.opts

	var sel usage_v1alpha.Selector
	sel.SetApp(o.App)
	sel.SetService(o.Service)
	sel.SetNode(o.Runner)
	sel.SetKind(o.Kind)
	sel.SetStatus(o.Status)
	sel.SetIncludeSystem(o.System)
	sel.SetIncludeAddons(!o.NoAddons)

	return &sel
}

func (q topQuery) window() *usage_v1alpha.Window {
	var w usage_v1alpha.Window

	// Only the start is sent. The server treats an unset end as now, which is
	// what a live view wants, and sending a client-computed "now" would make
	// the window depend on clock skew between the two machines.
	if q.lookback > 0 {
		w.SetStart(standard.ToTimestamp(time.Now().Add(-q.lookback)))
	}
	w.SetAggregate(q.opts.Aggregate)

	return &w
}

func (q topQuery) ordering() *usage_v1alpha.Ordering {
	var o usage_v1alpha.Ordering
	o.SetSort(q.opts.Sort)
	o.SetOrder(q.opts.Order)
	o.SetLimit(int32(q.opts.Limit))
	return &o
}

func (q topQuery) fetchSandboxes(ctx context.Context) (*usage_v1alpha.ResourceUsageClientListSandboxesResults, error) {
	return q.client.ListSandboxes(ctx, q.selector(), q.window(), q.ordering())
}

func (q topQuery) fetchNodes(ctx context.Context) (*usage_v1alpha.ResourceUsageClientListNodesResults, error) {
	return q.client.ListNodes(ctx, q.selector(), q.window(), q.ordering())
}

func (q topQuery) fetchApps(ctx context.Context) (*usage_v1alpha.ResourceUsageClientListAppsResults, error) {
	return q.client.ListApps(ctx, q.selector(), q.window(), q.ordering())
}

func (q topQuery) renderJSON(ctx *Context) error {
	if q.opts.Apps {
		res, err := q.fetchApps(ctx)
		if err != nil {
			return err
		}
		return PrintJSON(appsJSON(res))
	}

	if q.opts.Nodes {
		res, err := q.fetchNodes(ctx)
		if err != nil {
			return err
		}
		return PrintJSON(nodesJSON(res))
	}

	res, err := q.fetchSandboxes(ctx)
	if err != nil {
		return err
	}
	return PrintJSON(sandboxesJSON(res))
}

func (q topQuery) render(ctx context.Context) (string, error) {
	if q.opts.Apps {
		res, err := q.fetchApps(ctx)
		if err != nil {
			return "", err
		}
		return renderAppTable(res), nil
	}

	if q.opts.Nodes {
		res, err := q.fetchNodes(ctx)
		if err != nil {
			return "", err
		}
		return renderNodeTable(res), nil
	}

	res, err := q.fetchSandboxes(ctx)
	if err != nil {
		return "", err
	}
	return renderSandboxTable(res), nil
}

// --- table rendering ---

func renderSandboxTable(res *usage_v1alpha.ResourceUsageClientListSandboxesResults) string {
	sandboxes := res.Sandboxes()
	if len(sandboxes) == 0 {
		return "No sandboxes are reporting usage.\n" + renderWarnings(res.Warnings())
	}

	headers := []string{"APP", "SERVICE", "RUNNER", "CPU", "NODE%", "MEM", "STATUS"}
	rows := make([]ui.Row, 0, len(sandboxes))

	for _, w := range sandboxes {
		ref := w.Ref()

		// A stale row is one whose sandbox is running but reporting nothing.
		// Its numbers are shown as unknown rather than zero, because zero would
		// read as idle when the truth is that nobody is measuring.
		cpu, nodePct, mem := "-", "-", "-"
		if !w.Stale() {
			cpu = formatCores(w.Cpu().Cores())
			mem = units.Bytes(w.Memory().Bytes()).Short()

			// A zero share has two causes that must not look alike: the
			// sandbox is genuinely idle, or its node never reported a core
			// count to divide by. Only the second is unknown.
			switch {
			case w.Cpu().Cores() == 0:
				nodePct = "0%"
			case w.Cpu().PercentOfNode() > 0:
				nodePct = fmt.Sprintf("%.0f%%", w.Cpu().PercentOfNode())
			}
		}

		rows = append(rows, ui.Row{
			orDash(ref.App()),
			orDash(ref.Service()),
			orDash(ref.NodeName()),
			cpu,
			nodePct,
			mem,
			orDash(ref.Status()),
		})
	}

	out := renderUsageTable(headers, rows)
	out += "\n" + renderFooter(res.Cluster(), res.Window(), int(res.TotalCount()), len(sandboxes))
	out += renderWarnings(res.Warnings())

	return out
}

func renderNodeTable(res *usage_v1alpha.ResourceUsageClientListNodesResults) string {
	nodes := res.Nodes()
	if len(nodes) == 0 {
		return "No nodes are reporting usage.\n" + renderWarnings(res.Warnings())
	}

	// The sandbox and system columns are the point of this view: they say
	// whether a hot node is hot because of someone's app or because of the
	// platform underneath it.
	headers := []string{"NODE", "ROLE", "CPU", "SANDBOX", "SYSTEM", "CORES", "MEM", "TOTAL", "LOAD", "PODS", "STATUS"}
	rows := make([]ui.Row, 0, len(nodes))

	for _, n := range nodes {
		cpu, sandboxCPU, systemCPU, cores := "-", "-", "-", "-"
		mem, memTotal := "-", "-"

		if !n.Stale() {
			cpu = formatCores(n.Total().CpuCores())
			sandboxCPU = formatCores(n.Sandboxes().CpuCores())
			systemCPU = formatCores(n.System().CpuCores())
			mem = units.Bytes(n.Total().MemoryBytes()).Short()
		}
		if c := n.Capacity().CpuCores(); c > 0 {
			cores = fmt.Sprintf("%.0f", c)
		}
		if b := n.Capacity().MemoryBytes(); b > 0 {
			memTotal = units.Bytes(b).Short()
		}

		status := n.Status()
		if n.Stale() {
			status = "not reporting"
		} else if n.Scheduling() == "cordoned" {
			status += " (cordoned)"
		}

		rows = append(rows, ui.Row{
			orDash(n.NodeName()),
			orDash(n.Role()),
			cpu,
			sandboxCPU,
			systemCPU,
			cores,
			mem,
			memTotal,
			fmt.Sprintf("%.2f", n.Load1()),
			fmt.Sprintf("%d", n.SandboxCount()),
			orDash(status),
		})
	}

	out := renderUsageTable(headers, rows)
	out += "\n" + renderFooter(res.Cluster(), res.Window(), len(nodes), len(nodes))
	out += renderWarnings(res.Warnings())

	return out
}

func renderUsageTable(headers []string, rows []ui.Row) string {
	columns := ui.AutoSizeColumns(headers, rows, nil)
	table := ui.NewTable(ui.WithColumns(columns), ui.WithRows(rows))
	return table.Render() + "\n"
}

func renderFooter(cluster *usage_v1alpha.ResourceTotals, w *usage_v1alpha.UsageWindow, total, shown int) string {
	var b strings.Builder

	fmt.Fprintf(&b, "%s CPU, %s memory",
		formatCores(cluster.CpuCores()), units.Bytes(cluster.MemoryBytes()).Short())

	if w != nil && w.HasStart() && w.HasEnd() {
		span := standard.FromTimestamp(w.End()).Sub(standard.FromTimestamp(w.Start()))
		fmt.Fprintf(&b, " over %s (%s)", formatDuration(span), w.Aggregate())
	}

	if total > shown {
		fmt.Fprintf(&b, ", showing %d of %d", shown, total)
	}

	return mutedStyle.Render(b.String()) + "\n"
}

// renderWarnings surfaces what the server could not answer. These are printed
// rather than swallowed because a missing column has a cause, and an operator
// staring at blank numbers deserves to know it was the query and not the app.
func renderWarnings(warnings []string) string {
	if len(warnings) == 0 {
		return ""
	}

	var b strings.Builder
	for _, w := range warnings {
		fmt.Fprintf(&b, "%s %s\n", warnStyle.Render("warning:"), w)
	}
	return b.String()
}

// --- JSON output ---

type sandboxUsageJSON struct {
	Sandbox      string  `json:"sandbox"`
	SandboxShort string  `json:"sandbox_short_id,omitempty"`
	App          string  `json:"app,omitempty"`
	Service      string  `json:"service,omitempty"`
	Kind         string  `json:"kind,omitempty"`
	Runner       string  `json:"runner,omitempty"`
	Node         string  `json:"node,omitempty"`
	Status       string  `json:"status,omitempty"`
	CPUCores     float64 `json:"cpu_cores"`
	CPUNodePct   float64 `json:"cpu_percent_of_node"`
	MemoryBytes  int64   `json:"memory_bytes"`
	Stale        bool    `json:"stale"`
	StartedAt    string  `json:"started_at,omitempty"`
}

type sandboxListJSON struct {
	Sandboxes   []sandboxUsageJSON `json:"sandboxes"`
	TotalCount  int32              `json:"total_count"`
	WindowStart string             `json:"window_start,omitempty"`
	WindowEnd   string             `json:"window_end,omitempty"`
	Aggregate   string             `json:"aggregate,omitempty"`
	Warnings    []string           `json:"warnings,omitempty"`
}

func sandboxesJSON(res *usage_v1alpha.ResourceUsageClientListSandboxesResults) sandboxListJSON {
	out := sandboxListJSON{
		Sandboxes:  make([]sandboxUsageJSON, 0, len(res.Sandboxes())),
		TotalCount: res.TotalCount(),
		Warnings:   res.Warnings(),
	}

	if w := res.Window(); w != nil {
		out.WindowStart = timestampRFC3339(w.Start())
		out.WindowEnd = timestampRFC3339(w.End())
		out.Aggregate = w.Aggregate()
	}

	for _, w := range res.Sandboxes() {
		ref := w.Ref()
		out.Sandboxes = append(out.Sandboxes, sandboxUsageJSON{
			Sandbox:      ref.Sandbox(),
			SandboxShort: ref.SandboxShortId(),
			App:          ref.App(),
			Service:      ref.Service(),
			Kind:         ref.Kind(),
			Runner:       ref.NodeName(),
			Node:         ref.Node(),
			Status:       ref.Status(),
			CPUCores:     w.Cpu().Cores(),
			CPUNodePct:   w.Cpu().PercentOfNode(),
			MemoryBytes:  w.Memory().Bytes(),
			Stale:        w.Stale(),
			StartedAt:    timestampRFC3339(ref.StartedAt()),
		})
	}

	return out
}

type nodeJSON struct {
	Node               string  `json:"node"`
	Name               string  `json:"name,omitempty"`
	RunnerID           string  `json:"runner_id,omitempty"`
	Role               string  `json:"role,omitempty"`
	Status             string  `json:"status,omitempty"`
	Scheduling         string  `json:"scheduling,omitempty"`
	CPUCoresTotal      float64 `json:"cpu_cores_total"`
	CPUCoresUsed       float64 `json:"cpu_cores_used"`
	CPUCoresSandbox    float64 `json:"cpu_cores_sandbox"`
	CPUCoresSystem     float64 `json:"cpu_cores_system"`
	MemoryBytesTotal   int64   `json:"memory_bytes_total"`
	MemoryBytesUsed    int64   `json:"memory_bytes_used"`
	MemoryBytesSandbox int64   `json:"memory_bytes_sandbox"`
	MemoryBytesSystem  int64   `json:"memory_bytes_system"`
	Load1              float64 `json:"load1"`
	Load5              float64 `json:"load5"`
	Load15             float64 `json:"load15"`
	SandboxCount       int64   `json:"sandbox_count"`
	Stale              bool    `json:"stale"`
}

type nodeListJSON struct {
	Nodes       []nodeJSON `json:"nodes"`
	WindowStart string     `json:"window_start,omitempty"`
	WindowEnd   string     `json:"window_end,omitempty"`
	Aggregate   string     `json:"aggregate,omitempty"`
	Warnings    []string   `json:"warnings,omitempty"`
}

func nodesJSON(res *usage_v1alpha.ResourceUsageClientListNodesResults) nodeListJSON {
	out := nodeListJSON{
		Nodes:    make([]nodeJSON, 0, len(res.Nodes())),
		Warnings: res.Warnings(),
	}

	if w := res.Window(); w != nil {
		out.WindowStart = timestampRFC3339(w.Start())
		out.WindowEnd = timestampRFC3339(w.End())
		out.Aggregate = w.Aggregate()
	}

	for _, n := range res.Nodes() {
		out.Nodes = append(out.Nodes, nodeJSON{
			Node:               n.Node(),
			Name:               n.NodeName(),
			RunnerID:           n.RunnerId(),
			Role:               n.Role(),
			Status:             n.Status(),
			Scheduling:         n.Scheduling(),
			CPUCoresTotal:      n.Capacity().CpuCores(),
			CPUCoresUsed:       n.Total().CpuCores(),
			CPUCoresSandbox:    n.Sandboxes().CpuCores(),
			CPUCoresSystem:     n.System().CpuCores(),
			MemoryBytesTotal:   n.Capacity().MemoryBytes(),
			MemoryBytesUsed:    n.Total().MemoryBytes(),
			MemoryBytesSandbox: n.Sandboxes().MemoryBytes(),
			MemoryBytesSystem:  n.System().MemoryBytes(),
			Load1:              n.Load1(),
			Load5:              n.Load5(),
			Load15:             n.Load15(),
			SandboxCount:       n.SandboxCount(),
			Stale:              n.Stale(),
		})
	}

	return out
}

// --- watch mode ---

type topModel struct {
	q        topQuery
	interval time.Duration

	// ctx bounds every refresh this model issues. Without it a hung query
	// would run to completion after the user has already quit, and there would
	// be no way to interrupt it.
	ctx context.Context

	body     string
	err      error
	quitting bool
}

// topResultMsg carries a completed refresh back to Update.
//
// The result travels in the message rather than being written to the model
// because bubbletea runs every tea.Cmd on its own goroutine. Assigning to the
// model from inside a command would race with View reading it, and Update is
// the only place bubbletea guarantees exclusive access.
type topResultMsg struct {
	body string
	err  error
}

type topRefreshMsg struct{}

func (q topQuery) watch(ctx *Context, interval time.Duration) error {
	m := &topModel{q: q, interval: interval, ctx: ctx}

	prog := tea.NewProgram(m)
	if _, err := prog.Run(); err != nil {
		return err
	}

	return m.err
}

func (m *topModel) Init() tea.Cmd {
	return m.refresh()
}

func (m *topModel) refresh() tea.Cmd {
	ctx := m.ctx
	q := m.q

	return func() tea.Msg {
		body, err := q.render(ctx)
		return topResultMsg{body: body, err: err}
	}
}

func (m *topModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "esc", "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		case "r":
			return m, m.refresh()
		}

	case topResultMsg:
		if msg.err != nil {
			m.err = msg.err
			return m, tea.Quit
		}

		m.body = msg.body

		if m.quitting {
			return m, tea.Quit
		}

		return m, tea.Tick(m.interval, func(time.Time) tea.Msg {
			return topRefreshMsg{}
		})

	case topRefreshMsg:
		if m.quitting {
			return m, tea.Quit
		}
		return m, m.refresh()
	}

	return m, nil
}

func (m *topModel) View() string {
	if m.err != nil {
		return fmt.Sprintf("error: %v\n", m.err)
	}
	if m.body == "" {
		return "Collecting usage...\n"
	}
	return m.body + mutedStyle.Render(fmt.Sprintf("refreshing every %s - q to quit, r to refresh now", m.interval)) + "\n"
}

// --- helpers ---

// formatCores renders CPU the way top does: as a percentage of one core, which
// exceeds 100% for a sandbox using more than one core.
func formatCores(cores float64) string {
	return fmt.Sprintf("%.0f%%", cores*100)
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func timestampRFC3339(ts *standard.Timestamp) string {
	if ts == nil {
		return ""
	}
	t := standard.FromTimestamp(ts)
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}

// parseTopDuration reads a window flag. An empty value yields zero, which
// leaves the server to pick its own default.
func parseTopDuration(s string) (time.Duration, error) {
	if strings.TrimSpace(s) == "" {
		return 0, nil
	}

	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, err
	}
	if d <= 0 {
		return 0, fmt.Errorf("must be positive, got %s", s)
	}

	return d, nil
}

const topDescription = `Show what a cluster is spending its CPU and memory on, ordered by the busiest.

Each row is one live sandbox. Three replicas of a service are three rows, and
dead sandboxes are left out entirely. By default only app sandboxes are listed;
addon servers and one-off task runs are hidden unless you pass --system, so the
listing answers "what are my apps doing".

CPU is reported the way top reports it: as a percentage of one core. A sandbox
using two full cores reads 200%. NODE% is the same figure against the host it
runs on, which is what says whether 200% matters.

Four flags cover most investigations:

  --nodes     Which machine is hot, split into what sandboxes are using and what
              Miren's own moving parts are using underneath them. Start here.
  --apps      One row per app instead of per sandbox, summed across everything
              that app owns. See below.
  --runner    Narrow to one host once you know which.
  --since     Look back. A live view only shows now, so a spike that has already
              passed is invisible; --since 1h --aggregate max finds it.

--apps answers "what is app X costing me" without you adding up rows. An app's
total includes the dedicated addons it owns -- a database exists because the app
asked for it -- and the SERVICES and ADDONS columns show how the total divides,
so "2.1 cores, 1.6 of it Postgres" is one line rather than an investigation.
Pass --no-addons to count only the app's own code.

There is no restart column. Miren replaces a failed sandbox rather than
restarting it, so no single sandbox accumulates a restart count; repeated
failure shows up as a crash loop on the pool, which "miren sandbox inspect"
reports.

A row showing "-" for its numbers is running but reporting nothing. That is a
finding rather than an omission: either it has just started, or metrics
collection is down for it.
`

func renderAppTable(res *usage_v1alpha.ResourceUsageClientListAppsResults) string {
	apps := res.Apps()
	if len(apps) == 0 {
		return "No apps are reporting usage.\n" + renderWarnings(res.Warnings())
	}

	// SERVICES and ADDONS break the total down, so a busy app can be attributed
	// to the code someone wrote or to the database it depends on.
	headers := []string{"APP", "CPU", "SERVICES", "ADDONS", "MEM", "SANDBOXES"}
	rows := make([]ui.Row, 0, len(apps))

	for _, a := range apps {
		cpu, services, addons, mem := "-", "-", "-", "-"
		if !a.Stale() {
			cpu = formatCores(a.Total().CpuCores())
			services = formatCores(a.Services().CpuCores())
			addons = formatCores(a.Addons().CpuCores())
			mem = units.Bytes(a.Total().MemoryBytes()).Short()
		}

		// An app with no addons shows a dash rather than 0%, so an empty column
		// reads as "none of these" rather than "these are idle".
		if a.AddonCount() == 0 {
			addons = "-"
		}

		counts := fmt.Sprintf("%d", a.SandboxCount())
		if a.AddonCount() > 0 {
			counts = fmt.Sprintf("%d (+%d addon)", a.ServiceCount(), a.AddonCount())
		}

		rows = append(rows, ui.Row{orDash(a.App()), cpu, services, addons, mem, counts})
	}

	out := renderUsageTable(headers, rows)
	out += "\n" + renderFooter(res.Cluster(), res.Window(), int(res.TotalCount()), len(apps))
	out += renderWarnings(res.Warnings())

	return out
}

type appUsageJSON struct {
	App                string  `json:"app"`
	AppID              string  `json:"app_id,omitempty"`
	CPUCores           float64 `json:"cpu_cores"`
	CPUCoresServices   float64 `json:"cpu_cores_services"`
	CPUCoresAddons     float64 `json:"cpu_cores_addons"`
	MemoryBytes        int64   `json:"memory_bytes"`
	MemoryBytesService int64   `json:"memory_bytes_services"`
	MemoryBytesAddons  int64   `json:"memory_bytes_addons"`
	SandboxCount       int64   `json:"sandbox_count"`
	ServiceCount       int64   `json:"service_count"`
	AddonCount         int64   `json:"addon_count"`
	Stale              bool    `json:"stale"`
}

type appListJSON struct {
	Apps        []appUsageJSON `json:"apps"`
	WindowStart string         `json:"window_start,omitempty"`
	WindowEnd   string         `json:"window_end,omitempty"`
	Aggregate   string         `json:"aggregate,omitempty"`
	Warnings    []string       `json:"warnings,omitempty"`
}

func appsJSON(res *usage_v1alpha.ResourceUsageClientListAppsResults) appListJSON {
	out := appListJSON{
		Apps:     make([]appUsageJSON, 0, len(res.Apps())),
		Warnings: res.Warnings(),
	}

	if w := res.Window(); w != nil {
		out.WindowStart = timestampRFC3339(w.Start())
		out.WindowEnd = timestampRFC3339(w.End())
		out.Aggregate = w.Aggregate()
	}

	for _, a := range res.Apps() {
		out.Apps = append(out.Apps, appUsageJSON{
			App:                a.App(),
			AppID:              a.AppId(),
			CPUCores:           a.Total().CpuCores(),
			CPUCoresServices:   a.Services().CpuCores(),
			CPUCoresAddons:     a.Addons().CpuCores(),
			MemoryBytes:        a.Total().MemoryBytes(),
			MemoryBytesService: a.Services().MemoryBytes(),
			MemoryBytesAddons:  a.Addons().MemoryBytes(),
			SandboxCount:       a.SandboxCount(),
			ServiceCount:       a.ServiceCount(),
			AddonCount:         a.AddonCount(),
			Stale:              a.Stale(),
		})
	}

	return out
}
