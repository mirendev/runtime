package commands

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"miren.dev/runtime/pkg/progress/upload"
	"miren.dev/runtime/pkg/theme"
	"miren.dev/runtime/pkg/ui"
)

// DebugDeployEvents renders a `miren deploy --format jsonl` stream back into
// the readable form a person would have seen, so a log captured by CI or a
// script can be read without picking through JSON.
func DebugDeployEvents(ctx *Context, opts struct {
	File       string `position:"0" usage:"JSONL file written by 'miren deploy --format jsonl' (default: stdin)"`
	Timestamps bool   `short:"t" long:"timestamps" description:"Prefix each line with the time elapsed since the first event"`
	BuildLogs  bool   `long:"build-logs" description:"Show the log lines of every build step, not just the steps"`
}) error {
	var in io.Reader = os.Stdin
	if opts.File != "" && opts.File != "-" {
		f, err := os.Open(opts.File)
		if err != nil {
			return err
		}
		defer f.Close()
		in = f
	}

	r := newDeployEventRenderer(ctx, opts.Timestamps, opts.BuildLogs)
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		r.renderLine(scanner.Bytes())
	}
	return scanner.Err()
}

// deployEventRecord is the union of every field any deploy event carries. The
// events are flat, so one struct decodes them all; which fields are set
// depends on Event.
type deployEventRecord struct {
	Event string    `json:"event"`
	Time  time.Time `json:"time"`

	App     string `json:"app"`
	Cluster string `json:"cluster"`
	Message string `json:"message"`

	Bytes          int64   `json:"bytes"`
	BytesPerSecond float64 `json:"bytes_per_second"`
	Fraction       float64 `json:"fraction"`
	ETAMs          int64   `json:"eta_ms"`
	DurationMs     int64   `json:"duration_ms"`
	ReusedFiles    int     `json:"reused_files"`
	TotalFiles     int     `json:"total_files"`
	SavedBytes     int64   `json:"saved_bytes"`

	Step   string `json:"step"`
	Digest string `json:"digest"`
	Status string `json:"status"`
	Error  string `json:"error"`
	Line   string `json:"line"`
	Image  string `json:"image"`
	Steps  int    `json:"steps"`
	Cached int    `json:"cached"`

	DeployID string `json:"deploy_id"`
	Phase    string `json:"phase"`

	Detail string `json:"detail"`
	Link   string `json:"link"`

	Version string `json:"version"`
	Port    int    `json:"port"`
	Address string `json:"address"`

	Level  string         `json:"level"`
	Fields map[string]any `json:"fields"`

	AppVersion string            `json:"app_version"`
	URLs       []string          `json:"urls"`
	Ephemeral  *ephemeralSummary `json:"ephemeral"`
}

type deployEventRenderer struct {
	ctx        *Context
	timestamps bool
	buildLogs  bool

	first      time.Time
	steps      map[string]int // digest → step number, in order of first sight
	named      map[string]bool
	printedLog bool // "Recent logs:" header emitted

	faint   lipgloss.Style
	success lipgloss.Style
	warn    lipgloss.Style
	fail    lipgloss.Style
}

func newDeployEventRenderer(ctx *Context, timestamps, buildLogs bool) *deployEventRenderer {
	return &deployEventRenderer{
		ctx:        ctx,
		timestamps: timestamps,
		buildLogs:  buildLogs,
		steps:      map[string]int{},
		named:      map[string]bool{},
		faint:      lipgloss.NewStyle().Foreground(theme.Muted),
		success:    lipgloss.NewStyle().Foreground(theme.Success),
		warn:       lipgloss.NewStyle().Foreground(theme.Warning),
		fail:       phaseFailStyle,
	}
}

// renderLine handles one line of input. Anything that is not a JSON event
// (stderr text that got mixed in, say) is echoed faintly rather than dropped,
// so the reader still sees everything the file holds.
func (r *deployEventRenderer) renderLine(line []byte) {
	trimmed := strings.TrimSpace(string(line))
	if trimmed == "" {
		return
	}
	var ev deployEventRecord
	if err := json.Unmarshal([]byte(trimmed), &ev); err != nil || ev.Event == "" {
		r.ctx.Printf("%s\n", r.faint.Render(trimmed))
		return
	}
	if r.first.IsZero() && !ev.Time.IsZero() {
		r.first = ev.Time
	}
	for _, out := range r.render(ev, trimmed) {
		if out == "" {
			// Spacer lines stay blank; a stamp on nothing is just noise.
			r.ctx.Printf("\n")
			continue
		}
		r.ctx.Printf("%s%s\n", r.stamp(ev), out)
	}
}

func (r *deployEventRenderer) stamp(ev deployEventRecord) string {
	if !r.timestamps {
		return ""
	}
	if ev.Time.IsZero() || r.first.IsZero() {
		return r.faint.Render("        ") + " "
	}
	return r.faint.Render(fmt.Sprintf("+%7.3fs", ev.Time.Sub(r.first).Seconds())) + " "
}

// render returns the lines for one event. It leans on the same helpers the
// live deploy uses so the two read alike. raw is the original input line, kept
// for events this renderer does not know so nothing is lost or invented.
func (r *deployEventRenderer) render(ev deployEventRecord, raw string) []string {
	switch ev.Event {
	case "start":
		return []string{fmt.Sprintf("  ✓ %s: %s %s %s", r.success.Render("Deploying"), ev.App, r.faint.Render("→"), ev.Cluster)}

	case "message":
		return []string{"  " + r.faint.Render(ev.Message)}

	case "upload":
		return []string{"  " + uploadProgressLine(upload.Progress{
			BytesRead:      ev.Bytes,
			BytesPerSecond: ev.BytesPerSecond,
			Fraction:       ev.Fraction,
			ETA:            time.Duration(ev.ETAMs) * time.Millisecond,
		})}

	case "upload_complete":
		details := upload.FormatBytes(ev.Bytes)
		if ev.TotalFiles > 0 {
			details += fmt.Sprintf(", reused %d/%d files", ev.ReusedFiles, ev.TotalFiles)
		}
		if ev.SavedBytes > 0 {
			details += fmt.Sprintf(" (saved %s)", upload.FormatBytes(ev.SavedBytes))
		}
		return []string{renderPhaseSummary(phaseSummary{
			name:     "Upload artifacts",
			duration: time.Duration(ev.DurationMs) * time.Millisecond,
			details:  details,
		})}

	case "build_step":
		return r.renderBuildStep(ev)

	case "build_log":
		if !r.buildLogs {
			return nil
		}
		return []string{r.faint.Render(fmt.Sprintf("    #%d ", r.stepNumber(ev.Digest))) + ev.Line}

	case "build_error":
		return []string{"  " + r.fail.Render("✗ "+ev.Message)}

	case "build_complete":
		return []string{renderPhaseSummary(buildPhaseSummary(ev.Image, ev.Steps, ev.Cached, time.Duration(ev.DurationMs)*time.Millisecond))}

	case "deployment":
		return []string{"  " + r.faint.Render(fmt.Sprintf("deployment %s: %s", ev.DeployID, ev.Phase))}

	case "warning":
		lines := []string{"  " + r.warn.Render("⚠ "+ev.Message)}
		if ev.Detail != "" {
			lines = append(lines, "    "+r.warn.Render(ev.Detail))
		}
		if ev.Link != "" {
			lines = append(lines, "    "+r.warn.Render("See: "+ev.Link))
		}
		return lines

	case "health":
		switch ev.Status {
		case "waiting":
			return []string{fmt.Sprintf("  Waiting for version %s to become healthy...", ev.Version)}
		case "healthy":
			return []string{healthSummaryLine(true, ev.Message, time.Duration(ev.DurationMs)*time.Millisecond)}
		default:
			return []string{healthSummaryLine(false, ev.Message, time.Duration(ev.DurationMs)*time.Millisecond)}
		}

	case "port_warning":
		addr := ""
		if ev.Address != "" {
			addr = " on " + ev.Address
		}
		return []string{
			r.warn.Render(fmt.Sprintf("⚠ Heads up: your app bound port %d%s, not the port Miren configured via $PORT.", ev.Port, addr)),
			"  Traffic is being auto-routed there. Set $PORT (or [services.web] port) to match to remove this warning.",
		}

	case "app_log":
		lines := []string{"  " + ev.Line}
		if !r.printedLog {
			r.printedLog = true
			lines = append([]string{"", "Recent logs:"}, lines...)
		}
		return lines

	case "log":
		return []string{r.renderLog(ev)}

	case "result":
		return r.renderResult(ev)

	default:
		return []string{"  " + r.faint.Render(raw)}
	}
}

func (r *deployEventRenderer) stepNumber(digest string) int {
	if n, ok := r.steps[digest]; ok {
		return n
	}
	n := len(r.steps) + 1
	r.steps[digest] = n
	return n
}

// renderBuildStep mirrors BuildKit's plain printer: a step is named the first
// time it appears, then each state change is one short line.
func (r *deployEventRenderer) renderBuildStep(ev deployEventRecord) []string {
	n := r.stepNumber(ev.Digest)
	var lines []string
	if !r.named[ev.Digest] {
		r.named[ev.Digest] = true
		lines = append(lines, r.faint.Render(fmt.Sprintf("    #%d %s", n, ev.Step)))
	}
	switch ev.Status {
	case "done":
		lines = append(lines, r.faint.Render(fmt.Sprintf("    #%d DONE", n)))
	case "cached":
		lines = append(lines, r.faint.Render(fmt.Sprintf("    #%d CACHED", n)))
	case "error":
		lines = append(lines, r.fail.Render(fmt.Sprintf("    #%d ERROR: %s", n, ev.Error)))
	}
	return lines
}

func (r *deployEventRenderer) renderLog(ev deployEventRecord) string {
	style := r.faint
	switch strings.ToUpper(ev.Level) {
	case "WARN":
		style = r.warn
	case "ERROR":
		style = r.fail
	}
	keys := make([]string, 0, len(ev.Fields))
	for k := range ev.Fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	fmt.Fprintf(&b, "  [%s] %s", strings.ToUpper(ev.Level), ev.Message)
	for _, k := range keys {
		fmt.Fprintf(&b, " %s=%v", k, ev.Fields[k])
	}
	return style.Render(b.String())
}

func (r *deployEventRenderer) renderResult(ev deployEventRecord) []string {
	var lines []string
	if ev.Ephemeral != nil {
		lines = append(lines, "",
			fmt.Sprintf("Ephemeral version %s created.", ev.AppVersion),
			fmt.Sprintf("  Label: %s", ev.Ephemeral.Label),
			fmt.Sprintf("  TTL:   %s", ev.Ephemeral.TTL))
		for _, u := range ev.URLs {
			lines = append(lines, "  URL:   "+u)
		}
	} else if len(ev.URLs) > 0 {
		lines = append(lines, "", "Your app is available at:")
		for _, u := range ev.URLs {
			lines = append(lines, "  "+u)
		}
	}

	switch ev.Status {
	case "success":
		lines = append(lines, "", "✓ Deploy successful")
	case "cancelled":
		lines = append(lines, "", "❌ Deploy cancelled")
	default:
		lines = append(lines, "", "✗ Deploy failed")
	}
	if ev.AppVersion != "" {
		// Same cleaning as the live deploy, so the replay matches it.
		lines = append(lines, "Version: "+ui.CleanEntityID(ev.AppVersion))
	}
	if ev.Error != "" {
		lines = append(lines, r.fail.Render("Error: "+ev.Error))
	}
	return lines
}
