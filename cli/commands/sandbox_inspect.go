package commands

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"miren.dev/runtime/pkg/theme"

	"miren.dev/runtime/api/usage/usage_v1alpha"
	"miren.dev/runtime/pkg/rpc/standard"
	"miren.dev/runtime/pkg/units"
)

// SandboxInspect reports one sandbox in detail: what it is using, what it runs,
// and why it has been failing if it has.
//
// This is the last step before reading logs. A listing says which sandbox is
// hot; this says whether that is normal for it, and whether the pool behind it
// has given up replacing it.
func SandboxInspect(ctx *Context, opts struct {
	Sandbox string `position:"0" usage:"ID or short ID of the sandbox to inspect" required:"true"`

	Since  string `long:"since" description:"Measure over this window, e.g. 30s, 5m, 1h (default 1m)"`
	Series bool   `long:"series" description:"Include CPU and memory history"`

	FormatOptions
	ConfigCentric
}) error {
	if strings.TrimSpace(opts.Sandbox) == "" {
		return fmt.Errorf("a sandbox ID is required")
	}

	client, err := ctx.RPCClient(ServiceUsage)
	if err != nil {
		return err
	}
	defer client.Close()

	lookback, err := parseTopDuration(opts.Since)
	if err != nil {
		return fmt.Errorf("--since: %w", err)
	}

	uc := usage_v1alpha.NewResourceUsageClient(client)

	var w usage_v1alpha.Window
	if lookback > 0 {
		w.SetStart(standard.ToTimestamp(time.Now().Add(-lookback)))
	}

	// Containers are left off: the stored series sum a sandbox's containers
	// together before they are written, so a per-container split cannot be
	// recovered from them. It needs the live per-node probe.
	var detail usage_v1alpha.DetailOptions
	detail.SetIncludeSeries(opts.Series)

	res, err := uc.GetSandbox(ctx, opts.Sandbox, &w, &detail)
	if err != nil {
		return err
	}

	if opts.IsJSON() {
		return PrintJSON(inspectJSON(res))
	}

	ctx.Printf("%s", renderInspect(res))
	return nil
}

func renderInspect(res *usage_v1alpha.ResourceUsageClientGetSandboxResults) string {
	// Same styles and shape as `miren app status`, which is the other
	// single-resource detail view: a bold title, then bold-muted labels
	// carrying their own colon, then title-case section headings.
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(theme.Info)
	labelStyle := lipgloss.NewStyle().Bold(true).Foreground(theme.Muted)

	u := res.Usage()
	ref := u.Ref()

	var b strings.Builder

	name := ref.SandboxShortId()
	if name == "" {
		name = ref.Sandbox()
	}
	fmt.Fprintf(&b, "%s\n\n", headerStyle.Render("Usage for sandbox "+name))

	field := func(label, value string) {
		if value == "" || value == "-" {
			return
		}
		fmt.Fprintf(&b, "%s %s\n", labelStyle.Render(label+":"), value)
	}

	field("Sandbox", ref.Sandbox())
	field("App", ref.App())
	field("Service", ref.Service())
	field("Kind", ref.Kind())
	field("Runner", ref.NodeName())
	field("Status", ref.Status())
	field("Image", res.Image())
	field("Restart policy", res.RestartPolicy())
	if ts := ref.StartedAt(); ts != nil {
		if t := standard.FromTimestamp(ts); !t.IsZero() {
			field("Started", formatDuration(time.Since(t))+" ago")
		}
	}

	fmt.Fprintf(&b, "\n%s\n", labelStyle.Render("Usage:"))
	if u.Stale() {
		// Distinguishing "not measured" from "measured as zero" is the whole
		// point here: one means the app is quiet, the other means the telemetry
		// pipeline is broken, and they need opposite responses.
		fmt.Fprintf(&b, "  no samples in this window - the sandbox may have just started, or metrics collection may be down\n")
	} else {
		fmt.Fprintf(&b, "  CPU:     %s\n", formatCores(u.Cpu().Cores()))
		if p := u.Cpu().PercentOfNode(); p > 0 {
			fmt.Fprintf(&b, "  Of node: %.1f%%\n", p)
		}
		fmt.Fprintf(&b, "  Memory:  %s\n", units.Bytes(u.Memory().Bytes()).Short())
	}

	// Miren does not set per-sandbox CPU or memory ceilings, so there is nothing
	// to be throttled against. Saying so beats printing a zero that reads as
	// "never throttled".
	fmt.Fprintf(&b, "  Limits:  none set (Miren does not cap sandbox CPU or memory)\n")

	if cl := res.CrashLoop(); cl != nil && cl.ConsecutiveCrashes() > 0 {
		fmt.Fprintf(&b, "\n%s\n", labelStyle.Render("Crash loop:"))
		fmt.Fprintf(&b, "  Consecutive crashes: %d\n", cl.ConsecutiveCrashes())
		if t := standard.FromTimestamp(cl.LastCrashAt()); !t.IsZero() {
			fmt.Fprintf(&b, "  Last crash:          %s ago\n", formatDuration(time.Since(t)))
		}
		if t := standard.FromTimestamp(cl.CooldownUntil()); t.After(time.Now()) {
			fmt.Fprintf(&b, "  Cooldown:            restarts paused for %s\n", formatDuration(time.Until(t)))
		}
	}

	if e := res.Exit(); e != nil {
		fmt.Fprintf(&b, "\n%s\n", labelStyle.Render("Exit:"))
		fmt.Fprintf(&b, "  Code:      %d\n", e.Code())
		if c := e.Container(); c != "" {
			fmt.Fprintf(&b, "  Container: %s\n", c)
		}
		if t := standard.FromTimestamp(e.At()); !t.IsZero() {
			fmt.Fprintf(&b, "  At:        %s ago\n", formatDuration(time.Since(t)))
		}
	}

	if containers := res.Containers(); len(containers) > 0 {
		fmt.Fprintf(&b, "\n%s\n", labelStyle.Render("Containers:"))
		for _, c := range containers {
			cname := c.Name()
			if cname == "" {
				cname = "(pause)"
			}
			fmt.Fprintf(&b, "  %s: %s CPU, %s\n", cname,
				formatCores(c.Cpu().Cores()), units.Bytes(c.Memory().Bytes()).Short())
		}
	}

	if series := res.Series(); len(series) > 0 {
		fmt.Fprintf(&b, "\n%s\n", labelStyle.Render("History:"))
		for _, sr := range series {
			fmt.Fprintf(&b, "  %s: %d points\n", sr.Metric(), len(sr.Points()))
		}
	}

	if w := res.Window(); w != nil && w.HasStart() && w.HasEnd() {
		span := standard.FromTimestamp(w.End()).Sub(standard.FromTimestamp(w.Start()))
		fmt.Fprintf(&b, "\n%s\n", mutedStyle.Render(
			fmt.Sprintf("measured over %s (%s)", formatDuration(span), w.Aggregate())))
	}

	b.WriteString(renderWarnings(res.Warnings()))

	return b.String()
}

type inspectResultJSON struct {
	Sandbox       sandboxUsageJSON  `json:"sandbox"`
	Image         string            `json:"image,omitempty"`
	RestartPolicy string            `json:"restart_policy,omitempty"`
	ExitCode      *int32            `json:"exit_code,omitempty"`
	ExitAt        string            `json:"exit_at,omitempty"`
	ExitContainer string            `json:"exit_container,omitempty"`
	CrashLoop     *crashLoopJSON    `json:"crash_loop,omitempty"`
	Containers    []containerJSON   `json:"containers,omitempty"`
	Series        []usageSeriesJSON `json:"series,omitempty"`
	WindowStart   string            `json:"window_start,omitempty"`
	WindowEnd     string            `json:"window_end,omitempty"`
	Warnings      []string          `json:"warnings,omitempty"`
}

type crashLoopJSON struct {
	ConsecutiveCrashes int32  `json:"consecutive_crashes"`
	LastCrashAt        string `json:"last_crash_at,omitempty"`
	CooldownUntil      string `json:"cooldown_until,omitempty"`
}

type containerJSON struct {
	Name        string  `json:"name"`
	CPUCores    float64 `json:"cpu_cores"`
	MemoryBytes int64   `json:"memory_bytes"`
}

type usageSeriesJSON struct {
	Metric string           `json:"metric"`
	Points []usagePointJSON `json:"points"`
}

type usagePointJSON struct {
	At    string  `json:"at"`
	Value float64 `json:"value"`
}

func inspectJSON(res *usage_v1alpha.ResourceUsageClientGetSandboxResults) inspectResultJSON {
	w := res.Usage()
	ref := w.Ref()

	out := inspectResultJSON{
		Sandbox: sandboxUsageJSON{
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
		},
		Image:         res.Image(),
		RestartPolicy: res.RestartPolicy(),
		Warnings:      res.Warnings(),
	}

	if win := res.Window(); win != nil {
		out.WindowStart = timestampRFC3339(win.Start())
		out.WindowEnd = timestampRFC3339(win.End())
	}

	if e := res.Exit(); e != nil {
		code := e.Code()
		out.ExitCode = &code
		out.ExitAt = timestampRFC3339(e.At())
		out.ExitContainer = e.Container()
	}

	if cl := res.CrashLoop(); cl != nil {
		out.CrashLoop = &crashLoopJSON{
			ConsecutiveCrashes: cl.ConsecutiveCrashes(),
			LastCrashAt:        timestampRFC3339(cl.LastCrashAt()),
			CooldownUntil:      timestampRFC3339(cl.CooldownUntil()),
		}
	}

	for _, c := range res.Containers() {
		out.Containers = append(out.Containers, containerJSON{
			Name:        c.Name(),
			CPUCores:    c.Cpu().Cores(),
			MemoryBytes: c.Memory().Bytes(),
		})
	}

	for _, s := range res.Series() {
		sj := usageSeriesJSON{Metric: s.Metric()}
		for _, p := range s.Points() {
			sj.Points = append(sj.Points, usagePointJSON{
				At:    timestampRFC3339(p.At()),
				Value: p.Value(),
			})
		}
		out.Series = append(out.Series, sj)
	}

	return out
}
