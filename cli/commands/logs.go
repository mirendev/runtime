package commands

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/lipgloss"

	"miren.dev/runtime/api/app/app_v1alpha"
	"miren.dev/runtime/pkg/logfilter"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/rpc/standard"
	"miren.dev/runtime/pkg/rpc/stream"
	"miren.dev/runtime/pkg/theme"
	"miren.dev/runtime/pkg/ui"
)

// normalizeSandboxID ensures the sandbox ID has the "sandbox/" prefix
// required for log queries. Logs are stored with the full entity ID.
func normalizeSandboxID(sandboxID string) string {
	if strings.HasPrefix(sandboxID, "sandbox/") {
		return sandboxID
	}
	return "sandbox/" + sandboxID
}

// buildFilterWithService combines a user filter with a service filter for LogsQL.
// Service filter is added as a field match: service:"value"
func buildFilterWithService(userFilter, service string) string {
	if service == "" {
		return userFilter
	}
	serviceFilter := fmt.Sprintf("(service:%q OR miren.service:%q)", service, service)
	if userFilter == "" {
		return serviceFilter
	}
	return serviceFilter + " " + userFilter
}

// systemExclusion is a LogsQL filter clause that excludes system logs from
// app log queries. Applied in dispatchLogs on the streamLogChunks path.
const systemExclusion = `-source:"system"`

// buildBuildFilter creates a filter for build logs of a specific version.
// Combines source:build with the version filter.
func buildBuildFilter(version, userFilter string) string {
	buildFilter := fmt.Sprintf("source:build version:%q", version)
	if userFilter == "" {
		return buildFilter
	}
	return buildFilter + " " + userFilter
}

// buildSystemFilter creates a filter for system logs, optionally scoped to a component.
func buildSystemFilter(component, userFilter string) string {
	filter := `source:"system"`
	if component != "" {
		filter += fmt.Sprintf(" module:%q", component)
	}
	if userFilter != "" {
		filter += " " + userFilter
	}
	return filter
}

// timeFlagLayouts are the absolute timestamp formats accepted by --since/--until,
// tried in order. Naive (zoneless) layouts are interpreted in the local timezone,
// matching docker and journalctl.
var timeFlagLayouts = []string{
	time.RFC3339Nano,
	time.RFC3339,
	"2006-01-02 15:04:05",
	"2006-01-02 15:04",
	"2006-01-02",
	"15:04:05",
	"15:04",
}

// parseTimeFlag resolves a --since/--until value to an absolute time. It accepts
// RFC3339 timestamps, a handful of common naive layouts (interpreted in local
// time, with time-only forms anchored to today), and Go durations like "2h" or
// "90m", which are treated as that long ago relative to now.
func parseTimeFlag(s string, now time.Time) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, fmt.Errorf("empty time value")
	}

	for _, layout := range timeFlagLayouts {
		t, err := time.ParseInLocation(layout, s, time.Local)
		if err != nil {
			continue
		}
		// Time-only layouts parse with a zero (year 0) date; anchor them to today.
		if t.Year() == 0 {
			t = time.Date(now.Year(), now.Month(), now.Day(),
				t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), time.Local)
		}
		return t, nil
	}

	if d, err := time.ParseDuration(s); err == nil {
		return now.Add(-d), nil
	}

	return time.Time{}, fmt.Errorf(
		"not a recognized timestamp or duration (try RFC3339, '2006-01-02 15:04', or a duration like '2h')")
}

// resolveLogWindow computes the from/until bounds for a log query from the
// --last/--since/--until flags, enforcing the flag-combination rules. A nil
// return means that bound is open (read from the start of retention / up to now).
func resolveLogWindow(last *time.Duration, since, until string, follow bool, now time.Time) (from, to *standard.Timestamp, err error) {
	if since != "" && last != nil {
		return nil, nil, fmt.Errorf("--since and --last both set the start of the window; use one or the other")
	}
	if until != "" && follow {
		return nil, nil, fmt.Errorf("--until cannot be combined with --follow (a live tail has no end)")
	}

	switch {
	case since != "":
		t, perr := parseTimeFlag(since, now)
		if perr != nil {
			return nil, nil, fmt.Errorf("invalid --since value %q: %w", since, perr)
		}
		from = standard.ToTimestamp(t)
	case last != nil:
		from = standard.ToTimestamp(now.Add(-*last))
	}

	if until != "" {
		t, perr := parseTimeFlag(until, now)
		if perr != nil {
			return nil, nil, fmt.Errorf("invalid --until value %q: %w", until, perr)
		}
		to = standard.ToTimestamp(t)
	}

	if from != nil && to != nil && standard.FromTimestamp(to).Before(standard.FromTimestamp(from)) {
		return nil, nil, fmt.Errorf("the window is empty: --until is before --since")
	}

	return from, to, nil
}

// LogsApp shows application logs. This is the default subcommand for `miren logs`.
func LogsApp(ctx *Context, opts struct {
	AppCentric
	FormatOptions

	Last    *time.Duration `short:"l" long:"last" description:"Show logs from the last duration"`
	Since   string         `long:"since" description:"Show logs since a time (RFC3339, '2006-01-02 15:04', or a duration like '2h' ago)"`
	Until   string         `long:"until" description:"Show logs until a time (RFC3339, '2006-01-02 15:04', or a duration like '30m' ago); not valid with --follow"`
	Follow  bool           `short:"f" long:"follow" description:"Follow log output (live tail)"`
	Filter  string         `short:"g" long:"grep" description:"Filter logs (e.g., 'error', '\"exact phrase\"', 'error -debug', '/regex/')"`
	Service string         `long:"service" description:"Filter logs by service name (e.g., 'web', 'worker')"`
}) error {
	cl, err := ctx.RPCClient("dev.miren.runtime/logs")
	if err != nil {
		return err
	}

	from, until, err := resolveLogWindow(opts.Last, opts.Since, opts.Until, opts.Follow, time.Now())
	if err != nil {
		return err
	}

	combinedFilter := buildFilterWithService(opts.Filter, opts.Service)
	return dispatchLogs(ctx, cl, logDispatchArgs{
		app:            opts.App,
		from:           from,
		until:          until,
		follow:         opts.Follow,
		rawFilter:      opts.Filter,
		combinedFilter: combinedFilter,
		json:           opts.IsJSON(),
	})
}

// LogsSandbox shows logs for a specific sandbox.
func LogsSandbox(ctx *Context, opts struct {
	ConfigCentric
	FormatOptions

	SandboxID string         `position:"0" usage:"Sandbox ID" required:"true"`
	Last      *time.Duration `short:"l" long:"last" description:"Show logs from the last duration"`
	Since     string         `long:"since" description:"Show logs since a time (RFC3339, '2006-01-02 15:04', or a duration like '2h' ago)"`
	Until     string         `long:"until" description:"Show logs until a time (RFC3339, '2006-01-02 15:04', or a duration like '30m' ago); not valid with --follow"`
	Follow    bool           `short:"f" long:"follow" description:"Follow log output (live tail)"`
	Filter    string         `short:"g" long:"grep" description:"Filter logs (e.g., 'error', '\"exact phrase\"', 'error -debug', '/regex/')"`
}) error {
	sandbox := normalizeSandboxID(opts.SandboxID)

	cl, err := ctx.RPCClient("dev.miren.runtime/logs")
	if err != nil {
		return err
	}

	from, until, err := resolveLogWindow(opts.Last, opts.Since, opts.Until, opts.Follow, time.Now())
	if err != nil {
		return err
	}

	return dispatchLogs(ctx, cl, logDispatchArgs{
		sandbox:        sandbox,
		from:           from,
		until:          until,
		follow:         opts.Follow,
		rawFilter:      opts.Filter,
		combinedFilter: opts.Filter,
		json:           opts.IsJSON(),
	})
}

// LogsBuild shows build logs for a specific version.
func LogsBuild(ctx *Context, opts struct {
	AppCentric
	FormatOptions

	Version string         `position:"0" usage:"Build version (e.g., v3)" required:"true"`
	Last    *time.Duration `short:"l" long:"last" description:"Show logs from the last duration"`
	Since   string         `long:"since" description:"Show logs since a time (RFC3339, '2006-01-02 15:04', or a duration like '2h' ago)"`
	Until   string         `long:"until" description:"Show logs until a time (RFC3339, '2006-01-02 15:04', or a duration like '30m' ago); not valid with --follow"`
	Follow  bool           `short:"f" long:"follow" description:"Follow log output (live tail)"`
	Filter  string         `short:"g" long:"grep" description:"Filter logs (e.g., 'error', '\"exact phrase\"', 'error -debug', '/regex/')"`
}) error {
	cl, err := ctx.RPCClient("dev.miren.runtime/logs")
	if err != nil {
		return err
	}

	from, until, err := resolveLogWindow(opts.Last, opts.Since, opts.Until, opts.Follow, time.Now())
	if err != nil {
		return err
	}

	combinedFilter := buildBuildFilter(opts.Version, opts.Filter)
	return dispatchLogs(ctx, cl, logDispatchArgs{
		app:            opts.App,
		from:           from,
		until:          until,
		follow:         opts.Follow,
		rawFilter:      opts.Filter,
		combinedFilter: combinedFilter,
		json:           opts.IsJSON(),
	})
}

// LogsSystem shows system/server logs, optionally filtered by component.
func LogsSystem(ctx *Context, opts struct {
	ConfigCentric
	FormatOptions

	Component string         `position:"0" usage:"System component to filter by (e.g., 'etcd', 'scheduler')"`
	Last      *time.Duration `short:"l" long:"last" description:"Show logs from the last duration"`
	Since     string         `long:"since" description:"Show logs since a time (RFC3339, '2006-01-02 15:04', or a duration like '2h' ago)"`
	Until     string         `long:"until" description:"Show logs until a time (RFC3339, '2006-01-02 15:04', or a duration like '30m' ago); not valid with --follow"`
	Follow    bool           `short:"f" long:"follow" description:"Follow log output (live tail)"`
	Filter    string         `short:"g" long:"grep" description:"Filter logs (e.g., 'error', '\"exact phrase\"', 'error -debug', '/regex/')"`
}) error {
	cl, err := ctx.RPCClient("dev.miren.runtime/logs")
	if err != nil {
		return err
	}

	if !cl.HasMethod(ctx, "streamLogChunks") {
		return fmt.Errorf("system logs require a newer server version")
	}

	from, until, err := resolveLogWindow(opts.Last, opts.Since, opts.Until, opts.Follow, time.Now())
	if err != nil {
		return err
	}
	// When no time bounds and no --follow, from is nil → server returns last 100 lines

	if until != nil && !cl.HasMethodParam(ctx, "streamLogChunks", "to") {
		return fmt.Errorf("--until requires a newer server version")
	}

	target := &app_v1alpha.LogTarget{}
	target.SetSystem(true)

	combinedFilter := buildSystemFilter(opts.Component, opts.Filter)

	printer, flush := logPrinter(ctx, opts.IsJSON(), opts.Follow)
	defer flush()

	ac := app_v1alpha.LogsClient{Client: cl}
	callback := stream.Callback(func(chunk *app_v1alpha.LogChunk) error {
		for _, l := range chunk.Entries() {
			printer(l)
		}
		return nil
	})

	_, err = ac.StreamLogChunks(ctx, target, from, opts.Follow, combinedFilter, callback, until)
	return err
}

// logDispatchArgs holds the parameters for dispatching log requests across
// different server protocol versions.
type logDispatchArgs struct {
	app            string
	sandbox        string
	from           *standard.Timestamp
	until          *standard.Timestamp
	follow         bool
	rawFilter      string
	combinedFilter string
	json           bool
}

// dispatchLogs handles protocol negotiation and dispatches to the appropriate
// log streaming method based on server capabilities.
func dispatchLogs(ctx *Context, cl *rpc.NetworkClient, args logDispatchArgs) error {
	printer, flush := logPrinter(ctx, args.json, args.follow)
	defer flush()

	// For app queries, wrap the printer to skip system logs that may have
	// leaked into app log storage due to entity field collisions. The wrapper
	// sits in front of the coalescer so filtered-out lines aren't counted.
	if args.app != "" {
		inner := printer
		printer = func(l *app_v1alpha.LogEntry) {
			if l.HasSource() && l.Source() == "system" {
				return
			}
			inner(l)
		}
	}

	// Check if server supports streaming (prefer chunked for efficiency)
	if cl.HasMethod(ctx, "streamLogChunks") {
		// The `to` bound (from --until) is a parameter added to streamLogChunks
		// after it first shipped. A server with the method but not the parameter
		// would silently ignore the bound, so detect that and error instead of
		// quietly returning a wider window than asked for.
		if args.until != nil && !cl.HasMethodParam(ctx, "streamLogChunks", "to") {
			return fmt.Errorf("--until requires a newer server version")
		}

		// Append system exclusion for server-side filtering on app queries
		filter := args.combinedFilter
		if args.app != "" {
			if filter == "" {
				filter = systemExclusion
			} else {
				filter = systemExclusion + " " + filter
			}
		}
		return streamLogChunks(ctx, cl, args.app, args.sandbox, args.from, args.until, args.follow, filter, printer)
	}

	// Servers without streamLogChunks can't honor an upper bound at all.
	if args.until != nil {
		return fmt.Errorf("--until requires a newer server version")
	}

	// Older server - warn about upgrade and limited functionality
	ctx.Printf("Warning: server does not support optimized log streaming. Upgrade your server for better performance and --service/--build filtering.\n")

	// Server-side filtering (--service, --build) requires streamLogChunks.
	// If the combined filter differs from the raw user filter, one of these
	// was applied and we must error rather than silently dropping it.
	if args.rawFilter != args.combinedFilter {
		return fmt.Errorf("--service and --build filtering require a newer server version")
	}

	// Parse filter for client-side filtering on older protocol
	var filter *logfilter.Filter
	if args.rawFilter != "" {
		var err error
		filter, err = logfilter.Parse(args.rawFilter)
		if err != nil {
			return fmt.Errorf("invalid filter: %w", err)
		}
	}

	if cl.HasMethod(ctx, "streamLogs") {
		return streamLogs(ctx, cl, args.app, args.sandbox, args.from, args.follow, filter, printer)
	}

	// Warn if --follow requested but not supported
	if args.follow {
		ctx.Printf("Warning: server does not support --follow, showing recent logs only\n")
	}

	// Fall back to legacy pagination
	return legacyLogs(ctx, cl, args.app, args.sandbox, args.from, filter, printer)
}

// Log line styling (MIR-772). Every text log line is composed as a styled
// display string plus an unstyled signature (used to collapse repeated lines in
// follow mode). Styling is applied with lipgloss, which degrades to plain text
// when color is off (NO_COLOR, piped, non-TTY), so peek and pipe render the same
// content and only color adapts. Alignment padding is applied unconditionally so
// columns line up in either mode.
const logPrefixWidth = 14 // fixed cap for the aligned service.id / router column

var (
	// The timestamp is chrome: the whole thing is muted, and the date is dimmed
	// one notch further than the time so HH:MM:SS leads the eye. These log-local
	// shades exist because the theme has no "dimmer/brighter than Muted" role;
	// like the theme roles they pin explicit 256/16 anchors so they degrade
	// deliberately instead of quantizing into a muddy bucket. The date keeps a
	// distinct dim step at 256; the value shade collapses onto Muted below
	// truecolor, so 256-color terminals get a clean two-level (message vs muted
	// metadata) hierarchy instead of two indistinguishable grays.
	logTimeStyle = lipgloss.NewStyle().Foreground(theme.Muted)
	logDateStyle = lipgloss.NewStyle().Foreground(lipgloss.CompleteAdaptiveColor{
		Light: lipgloss.CompleteColor{TrueColor: "#AEB4BB", ANSI256: "250", ANSI: "8"},
		Dark:  lipgloss.CompleteColor{TrueColor: "#6A7079", ANSI256: "240", ANSI: "8"},
	})

	logIDStyle  = lipgloss.NewStyle().Foreground(theme.Muted) // instance id after service.
	logKeyStyle = lipgloss.NewStyle().Foreground(theme.Muted) // attribute key= names
	logValStyle = lipgloss.NewStyle().Foreground(lipgloss.CompleteAdaptiveColor{
		Light: lipgloss.CompleteColor{TrueColor: "#4B5563", ANSI256: "243", ANSI: "8"},
		Dark:  lipgloss.CompleteColor{TrueColor: "#C3C9D2", ANSI256: "246", ANSI: "8"},
	})

	logRouterTag = lipgloss.NewStyle().Foreground(theme.Router).Bold(true)
	logMethod    = lipgloss.NewStyle().Bold(true) // HTTP method, bright default weight

	// Status codes get semantic color, but 2xx stays quiet: healthy is the
	// default, the number itself carries the meaning, and reserving color for
	// anomalies sidesteps the red/green colorblindness trap.
	logStatus2xx = lipgloss.NewStyle().Foreground(theme.Muted)
	logStatus3xx = lipgloss.NewStyle().Foreground(theme.Info)
	logStatus4xx = lipgloss.NewStyle().Foreground(theme.Warning)
	logStatus5xx = lipgloss.NewStyle().Foreground(theme.Error)

	// app.* fields promoted onto the router line (RFD-92) read as the app's own
	// contribution, so they take the Highlight role.
	logAppKeyStyle = lipgloss.NewStyle().Foreground(theme.Highlight).Bold(true)
	logAppValStyle = lipgloss.NewStyle().Foreground(theme.Highlight)
)

// routerBodyHidden are the fields the router already prints in its logfmt body,
// so re-surfacing them from attributes would double-print. Suppressed only for
// router lines; other attributes (e.g. future app.* promoted fields) still show.
var routerBodyHidden = map[string]bool{"method": true, "path": true, "host": true, "access": true}

// logStatusStyle picks a status color by class. Unrecognized codes render quiet.
func logStatusStyle(code string) lipgloss.Style {
	if code == "" {
		return logStatus2xx
	}
	switch code[0] {
	case '3':
		return logStatus3xx
	case '4':
		return logStatus4xx
	case '5':
		return logStatus5xx
	default:
		return logStatus2xx
	}
}

// colorizeRouterBody styles a router logfmt body the same way app-line
// attributes are styled: keys muted, values a touch brighter, with status
// colored by class and the method bolded (app.* promoted fields take Highlight).
// It tokenizes the body but only wraps spans in styles, never rewriting bytes, so
// stripping the ANSI yields the original body verbatim (peek == pipe).
func colorizeRouterBody(body string) string {
	var b strings.Builder
	i, n := 0, len(body)
	for i < n {
		if body[i] == ' ' { // whitespace between tokens is preserved verbatim
			b.WriteByte(' ')
			i++
			continue
		}
		// Read the key: up to '=' or a space.
		ks := i
		for i < n && body[i] != '=' && body[i] != ' ' {
			i++
		}
		key := body[ks:i]
		if i >= n || body[i] != '=' {
			b.WriteString(key) // bare token (no value): emit verbatim
			continue
		}
		i++ // consume '='

		// Read the value: a quoted string (honoring \" escapes) or a bare run. An
		// unterminated quote (malformed/truncated body) just consumes to the end
		// of the string as the value, so no bytes are dropped.
		vs := i
		if i < n && body[i] == '"' {
			i++
			for i < n {
				if body[i] == '\\' && i+1 < n {
					i += 2
					continue
				}
				if body[i] == '"' {
					i++
					break
				}
				i++
			}
		} else {
			for i < n && body[i] != ' ' {
				i++
			}
		}
		value := body[vs:i]

		isApp := strings.HasPrefix(key, "app.")
		if isApp {
			b.WriteString(logAppKeyStyle.Render(key + "="))
		} else {
			b.WriteString(logKeyStyle.Render(key + "="))
		}
		switch {
		case key == "status":
			b.WriteString(logStatusStyle(value).Render(value))
		case key == "method":
			b.WriteString(logMethod.Render(value))
		case isApp:
			b.WriteString(logAppValStyle.Render(value))
		default:
			b.WriteString(logValStyle.Render(value))
		}
	}
	return b.String()
}

// laneStyleCache memoizes the bold lane style per color so the hot render path
// (a line for every log entry, worse under --follow) doesn't rebuild an
// identical lipgloss.Style each time. theme.Lane is a pure hash, so a given
// service always maps to the same color and thus the same cached style.
var laneStyleCache sync.Map // lipgloss.CompleteAdaptiveColor -> lipgloss.Style

func laneStyle(c lipgloss.CompleteAdaptiveColor) lipgloss.Style {
	if s, ok := laneStyleCache.Load(c); ok {
		return s.(lipgloss.Style)
	}
	s := lipgloss.NewStyle().Foreground(c).Bold(true)
	laneStyleCache.Store(c, s)
	return s
}

// padColumn right-pads a styled string to logPrefixWidth visible columns. plainW
// is the visible width of the unstyled text (ANSI is invisible to width). A
// prefix already at or over the cap is returned unpadded.
func padColumn(styled string, plainW int) string {
	if plainW >= logPrefixWidth {
		return styled
	}
	return styled + strings.Repeat(" ", logPrefixWidth-plainW)
}

// logPrefix returns the unstyled and styled forms of a line's lane prefix: the
// slate "router" tag for router lines, else "service.id" (service hued by a
// stable hash, instance id muted), falling back to the raw source when a line
// carries no service metadata.
func logPrefix(l *app_v1alpha.LogEntry) (plain, styled string) {
	if l.HasSource() && l.Source() == "router" {
		return "router", logRouterTag.Render("router")
	}

	var service, shortID string
	if l.HasAttributes() {
		a := l.Attributes()
		service = a["miren.service"]
		shortID = a["miren.short_id"]
	}

	switch {
	case service != "":
		hue := laneStyle(theme.Lane(service))
		if shortID != "" {
			return service + "." + shortID, hue.Render(service) + logIDStyle.Render("."+shortID)
		}
		return service, hue.Render(service)
	case shortID != "":
		hue := laneStyle(theme.Lane(shortID))
		return shortID, hue.Render(shortID)
	case l.HasSource() && l.Source() != "":
		// No service metadata, so fall back to the raw source (a sandbox entity
		// id). We never clip it: the readable part is at the front, so truncating
		// would drop the useful bit and keep the random suffix. A long id just
		// overflows its column, like an outlier service name does.
		source := l.Source()
		hue := laneStyle(theme.Lane(source))
		return source, hue.Render(source)
	}
	return "", ""
}

// logPrinter returns a function that prints a log entry (text or JSON) and a
// flush function to call once the stream ends. For interactive text follow on a
// TTY it collapses runs of repeated lines into a live-updated counter line; in
// every other mode it prints each entry verbatim and flush is a no-op.
func logPrinter(ctx *Context, jsonOutput, follow bool) (func(*app_v1alpha.LogEntry), func()) {
	noop := func() {}

	if jsonOutput {
		return func(l *app_v1alpha.LogEntry) {
			printLogEntryJSON(ctx, l)
		}, noop
	}

	if follow && ui.IsTTY() {
		c := newLogCoalescer(ctx)
		return c.print, c.flush
	}

	return func(l *app_v1alpha.LogEntry) {
		printLogEntry(ctx, l)
	}, noop
}

// logEntryJSON is the JSON representation of a log entry.
type logEntryJSON struct {
	Timestamp  string            `json:"timestamp"`
	Stream     string            `json:"stream"`
	Source     string            `json:"source,omitempty"`
	Message    string            `json:"message"`
	Attributes map[string]string `json:"attributes,omitempty"`
}

func printLogEntryJSON(ctx *Context, l *app_v1alpha.LogEntry) {
	entry := logEntryJSON{
		Timestamp: standard.FromTimestamp(l.Timestamp()).UTC().Format(time.RFC3339Nano),
		Stream:    l.Stream(),
		Message:   l.Line(),
	}
	if l.HasSource() && l.Source() != "" {
		entry.Source = l.Source()
	}
	if l.HasAttributes() {
		attrs := l.Attributes()
		if len(attrs) > 0 {
			entry.Attributes = attrs
		}
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return
	}
	ctx.Printf("%s\n", data)
}

// renderLogEntry returns the full display line (without a trailing newline) and
// a signature that is identical for entries that differ only by their timestamp.
// The signature lets callers collapse runs of repeated lines (see logCoalescer).
func renderLogEntry(l *app_v1alpha.LogEntry) (display, signature string) {
	isRouter := l.HasSource() && l.Source() == "router"

	prefixPlain, prefixStyled := logPrefix(l)

	// Body: router lines colorize status/method in place; app lines print the
	// message at full brightness (it's the content that should read first).
	bodyPlain := l.Line()
	bodyStyled := bodyPlain
	if isRouter {
		bodyStyled = colorizeRouterBody(bodyPlain)
	}

	// Attributes: for router lines, suppress the fields already in the body so we
	// don't double-print them; other attributes (future app.* promoted fields)
	// still render.
	var hide map[string]bool
	if isRouter {
		hide = routerBodyHidden
	}
	attrsPlain, attrsStyled := renderAttrs(attrsOf(l), hide)

	// Timestamp is chrome: full date+time always (so peek matches pipe and day
	// boundaries stay visible), with the date dimmed so the time leads.
	ts := standard.FromTimestamp(l.Timestamp()).Format("2006-01-02 15:04:05")
	tsStyled := logDateStyle.Render(ts[:10]) + " " + logTimeStyle.Render(ts[11:])

	var b strings.Builder
	b.WriteString(tsStyled)
	if prefixStyled != "" {
		b.WriteByte(' ')
		// lipgloss.Width counts terminal display cells (wide runes as 2), so the
		// column aligns even for a service name with CJK or other wide characters.
		b.WriteString(padColumn(prefixStyled, lipgloss.Width(prefixPlain)))
	}
	b.WriteByte(' ')
	b.WriteString(bodyStyled)
	b.WriteString(attrsStyled)
	display = b.String()

	// Signature ignores the timestamp and all styling, so runs that differ only by
	// time collapse. The NUL separators keep field boundaries unambiguous.
	signature = l.Stream() + "\x00" + prefixPlain + "\x00" + bodyPlain + "\x00" + attrsPlain
	return display, signature
}

// attrsOf returns an entry's attributes, or nil when it has none.
func attrsOf(l *app_v1alpha.LogEntry) map[string]string {
	if l.HasAttributes() {
		return l.Attributes()
	}
	return nil
}

func printLogEntry(ctx *Context, l *app_v1alpha.LogEntry) {
	display, _ := renderLogEntry(l)
	ctx.Printf("%s\n", display)
}

// logCoalescer collapses consecutive log lines that differ only by their
// timestamp. The first occurrence of a line is printed in full and committed;
// subsequent identical lines are summarized by a single live-updated line
// printed beneath it ("[ Repeated Nx over <span> ]") instead of scrolling the
// terminal.
//
// It is only used for interactive --follow on a TTY; piped, JSON, and non-follow
// output never coalesce so machine consumers see every line verbatim.
type logCoalescer struct {
	ctx     *Context
	sig     string    // signature of the current run
	count   int       // number of lines seen in the current run
	firstTS time.Time // timestamp of the first line in the current run
	live    bool      // a summary line is currently on screen with no trailing newline
}

func newLogCoalescer(ctx *Context) *logCoalescer {
	return &logCoalescer{ctx: ctx}
}

func (c *logCoalescer) print(l *app_v1alpha.LogEntry) {
	display, signature := renderLogEntry(l)
	ts := standard.FromTimestamp(l.Timestamp())

	if c.count > 0 && signature == c.sig {
		c.count++
		// Redraw the summary line in place beneath the repeated log line.
		c.ctx.Printf("\r\033[K[ Repeated %dx%s ]", c.count, runSpanSuffix(c.firstTS, ts))
		c.live = true
		return
	}

	// Distinct line: commit any in-progress summary, then print in full.
	if c.live {
		c.ctx.Printf("\n")
	}
	c.ctx.Printf("%s\n", display)
	c.sig = signature
	c.count = 1
	c.firstTS = ts
	c.live = false
}

// runSpanSuffix returns " over <dur>" describing how long a collapsed run has
// been repeating, or "" when the span is under a second. Timestamps are
// truncated to the second first so the span matches the difference between the
// second-resolution timestamps the user actually sees; bursts within one
// displayed second show no span, keeping the summary clean.
func runSpanSuffix(first, last time.Time) string {
	span := last.Truncate(time.Second).Sub(first.Truncate(time.Second))
	if span < time.Second {
		return ""
	}
	return " over " + formatRunSpan(span)
}

// formatRunSpan renders a whole-second duration compactly (e.g. "14s", "1m15s",
// "2h5m"). Input is expected to be pre-rounded to a second.
func formatRunSpan(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		m := int(d.Minutes())
		if s := int(d.Seconds()) % 60; s > 0 {
			return fmt.Sprintf("%dm%ds", m, s)
		}
		return fmt.Sprintf("%dm", m)
	default:
		h := int(d.Hours())
		if m := int(d.Minutes()) % 60; m > 0 {
			return fmt.Sprintf("%dh%dm", h, m)
		}
		return fmt.Sprintf("%dh", h)
	}
}

// flush commits a dangling summary line with a newline so the final run isn't
// left without one. Safe to call when nothing is pending.
func (c *logCoalescer) flush() {
	if c.live {
		c.ctx.Printf("\n")
		c.live = false
	}
}

var hiddenAttributes = map[string]bool{
	"source": true,
}

// formatAttributes renders attributes as an unstyled " key=val" logfmt tail,
// skipping hidden and miren.* keys. It is the plain form used in signatures.
func formatAttributes(m map[string]string) string {
	plain, _ := renderAttrs(m, nil)
	return plain
}

// renderAttrs renders attributes as a " key=val" tail in both plain and styled
// forms. Keys are muted and values a touch brighter; app.* promoted fields take
// the Highlight role. Hidden and miren.* keys are always skipped, plus any key in
// extraHidden (used to drop router body duplicates). The styled form degrades to
// exactly the plain form when color is off, so the two never diverge.
func renderAttrs(m map[string]string, extraHidden map[string]bool) (plain, styled string) {
	if len(m) == 0 {
		return "", ""
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		if hiddenAttributes[k] || strings.HasPrefix(k, "miren.") {
			continue
		}
		if extraHidden[k] {
			continue
		}
		keys = append(keys, k)
	}
	if len(keys) == 0 {
		return "", ""
	}
	slices.Sort(keys)

	var pb, sb strings.Builder
	for _, k := range keys {
		v := m[k]
		if strings.ContainsAny(v, " \t\"\n\r") {
			v = fmt.Sprintf("%q", v)
		}
		pb.WriteByte(' ')
		pb.WriteString(k)
		pb.WriteByte('=')
		pb.WriteString(v)

		sb.WriteByte(' ')
		if strings.HasPrefix(k, "app.") {
			sb.WriteString(logAppKeyStyle.Render(k + "="))
			sb.WriteString(logAppValStyle.Render(v))
		} else {
			sb.WriteString(logKeyStyle.Render(k + "="))
			sb.WriteString(logValStyle.Render(v))
		}
	}
	return pb.String(), sb.String()
}

func streamLogs(ctx *Context, cl *rpc.NetworkClient, app, sandbox string, from *standard.Timestamp, follow bool, filter *logfilter.Filter, printer func(*app_v1alpha.LogEntry)) error {
	ac := app_v1alpha.LogsClient{Client: cl}

	// Build target
	target := &app_v1alpha.LogTarget{}
	if sandbox != "" {
		target.SetSandbox(sandbox)
	} else {
		target.SetApp(app)
	}

	// from is resolved by the caller from --last/--since. When nil: follow
	// starts from now, non-follow returns the last 100 lines. This protocol has
	// no end bound, so --until is rejected before reaching here.

	// Create callback to print logs as they arrive
	callback := stream.Callback(func(l *app_v1alpha.LogEntry) error {
		// Apply local filter if provided
		if filter != nil && !filter.Match(l.Line()) {
			return nil
		}

		printer(l)
		return nil
	})

	_, err := ac.StreamLogs(ctx, target, from, follow, callback)
	return err
}

func streamLogChunks(ctx *Context, cl *rpc.NetworkClient, app, sandbox string, from, until *standard.Timestamp, follow bool, filter string, printer func(*app_v1alpha.LogEntry)) error {
	ac := app_v1alpha.LogsClient{Client: cl}

	// Build target
	target := &app_v1alpha.LogTarget{}
	if sandbox != "" {
		target.SetSandbox(sandbox)
	} else {
		target.SetApp(app)
	}

	// from/until are resolved by the caller from --last/--since/--until. When
	// from is nil: follow starts from now, non-follow returns the last 100 lines.
	// When until is nil, the server reads up to now.

	// Create callback to print logs as they arrive in chunks
	callback := stream.Callback(func(chunk *app_v1alpha.LogChunk) error {
		for _, l := range chunk.Entries() {
			printer(l)
		}
		return nil
	})

	_, err := ac.StreamLogChunks(ctx, target, from, follow, filter, callback, until)
	return err
}

func legacyLogs(ctx *Context, cl *rpc.NetworkClient, app, sandbox string, from *standard.Timestamp, filter *logfilter.Filter, printer func(*app_v1alpha.LogEntry)) error {
	ac := app_v1alpha.LogsClient{Client: cl}

	ts := from
	if ts == nil {
		// Legacy protocol can't do server-side limit=100, so default to
		// last 1 hour as a reasonable bounded window of recent logs.
		ts = standard.ToTimestamp(time.Now().Add(-1 * time.Hour))
	}

	for {
		var (
			res interface {
				Logs() []*app_v1alpha.LogEntry
			}
			err error
		)

		if sandbox != "" {
			res, err = ac.SandboxLogs(ctx, sandbox, ts, false)
		} else {
			res, err = ac.AppLogs(ctx, app, ts, false)
		}

		if err != nil {
			return err
		}

		logs := res.Logs()

		for _, l := range logs {
			// Apply local filter if provided
			if filter != nil && !filter.Match(l.Line()) {
				continue
			}

			printer(l)
		}

		if len(logs) != 100 {
			break
		}

		// For pagination, use the last log's timestamp + 1 microsecond to avoid duplicates
		lastTime := standard.FromTimestamp(logs[len(logs)-1].Timestamp())
		nextTime := lastTime.Add(time.Microsecond)
		ts = standard.ToTimestamp(nextTime)
	}

	return nil
}
