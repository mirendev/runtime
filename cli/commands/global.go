package commands

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"reflect"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"golang.org/x/sys/unix"
	"miren.dev/runtime/clientconfig"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/slogfmt"
	"miren.dev/runtime/pkg/slogrus"
	"miren.dev/runtime/pkg/theme"
	"miren.dev/runtime/pkg/ui"
)

type GlobalFlags struct {
	Verbose       []bool `short:"v" long:"verbose" description:"Enable verbose output"`
	ServerAddress string `long:"server-address" description:"Server address to connect to" default:"127.0.0.1:8443"`
	// We actually process this manually, but we include it here so that it validates.
	Options string `long:"options" description:"Path to file containing options"`
}

type Context struct {
	context.Context

	verbose int
	Log     *slog.Logger

	// A separate logger for UI output, which is always at least debug level
	UILog *slog.Logger

	Stdout io.Writer
	Stderr io.Writer

	// ServerState holds explicit server dependencies
	ServerState *ServerState

	cancels []func()

	ClientConfig  *clientconfig.Config
	ClusterConfig *clientconfig.ClusterConfig
	ClusterName   string

	// CommandName is the full command path the user invoked, e.g. "route
	// list". Error messages use it to describe failures in terms of what was
	// typed rather than the internal capability name behind it.
	CommandName string

	// clusterErr records a cluster that was named but couldn't be loaded, and
	// requestedCluster the name that was asked for. Held so that anything
	// needing a connection can refuse instead of silently using another one.
	clusterErr       error
	requestedCluster string

	Config struct {
		ServerAddress string
	}

	levelVar slog.LevelVar
	exitCode int
}

func (c *Context) Level() slog.Level {
	return c.levelVar.Level()
}

func (c *Context) Verbose() bool {
	return c.verbose > 0
}

// baseLogLevel is where the -v ladder starts before any -v is applied. Each -v
// moves one step louder from here (slog levels are 4 apart), clamped at Debug.
//
// The two answers differ because the two jobs do. A one-shot command like
// `miren app list` has its result on stdout and should stay quiet on stderr
// unless something is wrong, so it starts at Warn. A daemon has no result — the
// log is the only thing it produces — and at Warn a healthy one says nothing
// about itself at all, so an operator cannot tell "working" from "never
// attempted". That ambiguity is not hypothetical: during the MIR-1483 telemetry
// cutover the line confirming the switch was invisible, and verifying it meant
// pushing a systemd override onto every runner to add -v.
//
// Info is the affirmative baseline for a daemon. -v and the SIGTTIN/SIGTTOU
// handlers below move off it when you actually need Debug, which beats baking a
// level into a unit file that then outlives the reason for it.
func baseLogLevel(daemon bool) slog.Level {
	if daemon {
		return slog.LevelInfo
	}
	return slog.LevelWarn
}

func setup(ctx context.Context, flags *GlobalFlags, opts any, commandName string, daemon bool) *Context {
	s := &Context{
		verbose:     len(flags.Verbose),
		Stdout:      os.Stdout,
		Stderr:      os.Stderr,
		ServerState: NewServerState(),
		CommandName: commandName,
	}

	// Initialize config from flags
	s.Config.ServerAddress = flags.ServerAddress

	level := baseLogLevel(daemon) - slog.Level(4*s.verbose)
	if level < slog.LevelDebug {
		level = slog.LevelDebug
	}

	s.levelVar.Set(level)

	s.Log = slog.New(slogfmt.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: &s.levelVar,
	}))

	// A separate logger for UI output, which is always at least debug level
	s.UILog = slog.New(slogfmt.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})).With("module", "user")

	ctx = slogrus.WithLogger(ctx, s.Log.With("module", "slogrus"))
	slogrus.OverrideGlobal(s.Log)

	if lc, ok := opts.(interface {
		LoadConfig() (*clientconfig.Config, error)
	}); ok {
		cfg, err := lc.LoadConfig()
		if cfg != nil {
			s.ClientConfig = cfg
		} else if err != nil && !errors.Is(err, clientconfig.ErrNoConfig) {
			s.Log.Warn("Failed to load client config", "error", err)
		}
	}

	// Resolve the CLI color palette once, adapting to the terminal background and
	// honoring NO_COLOR/FORCE_COLOR plus the `theme` config field. Safe to call
	// per-command: it's guarded by a sync.Once internally.
	var configuredTheme string
	if s.ClientConfig != nil {
		configuredTheme = s.ClientConfig.Theme()
	}
	theme.Init(configuredTheme)

	if lc, ok := opts.(interface {
		LoadCluster() (*clientconfig.ClusterConfig, string, error)
	}); ok {
		cfg, name, err := lc.LoadCluster()
		if cfg != nil {
			s.ClusterConfig = cfg
			s.ClusterName = name
		} else if err != nil && !errors.Is(err, clientconfig.ErrNoConfig) {
			// Remembered rather than only logged. Anything that needs a
			// connection has to refuse rather than quietly use a different
			// cluster, and a warning scrolling past is not a refusal.
			s.clusterErr = err
			if rc, ok := opts.(interface{ RequestedCluster() string }); ok {
				s.requestedCluster = rc.RequestedCluster()
			}
			s.Log.Debug("Failed to load cluster config", "error", err)
		}
	}

	sigCh := make(chan os.Signal, 1)

	signal.Notify(sigCh, os.Interrupt, unix.SIGQUIT, unix.SIGTERM,
		unix.SIGTTIN, unix.SIGTTOU,
	)

	ctx, cancel := context.WithCancel(ctx)
	s.cancels = append(s.cancels, cancel)

	sigCtx, sigCancel := context.WithCancel(ctx)
	s.cancels = append(s.cancels, sigCancel)

	go func() {
		defer close(sigCh)
		defer signal.Stop(sigCh)

		var shutdownRequests int
		for {
			select {
			case <-sigCtx.Done():
				return
			case sig, ok := <-sigCh:
				if !ok {
					return
				}

				var target slog.Level

				switch sig {
				case unix.SIGTTIN:
					target = s.levelVar.Level() - 4
				case unix.SIGTTOU:
					target = s.levelVar.Level() + 4
				case os.Interrupt, unix.SIGQUIT, unix.SIGTERM:
					shutdownRequests++
					switch shutdownRequests {
					case 1:
						s.Log.InfoContext(sigCtx, "Signal received, shutting down")
						cancel()
					case 2:
						s.Log.InfoContext(sigCtx, "Shutdown urgency detected, exitting")
						os.Exit(130)
					}

					continue
				}

				if target < slog.LevelDebug {
					continue
				}

				if target > slog.LevelError {
					continue
				}

				if s.levelVar.Level() == target {
					continue
				}

				s.levelVar.Set(target)

				s.Log.ErrorContext(sigCtx, "Log leveling changed", "level", target)
			}
		}
	}()

	s.Log.DebugContext(ctx, "Configured logging", "level", level)
	s.Log.DebugContext(ctx, "Dynamic leveling enabled via signals", "more-logging", "SIGTTIN", "less-logging", "SIGTTOU")

	s.Context = ctx
	return s
}

func (c *Context) Close() error {
	for _, cancel := range c.cancels {
		cancel()
	}

	return nil
}

func (c *Context) SetExitCode(code int) {
	c.exitCode = code
}

func (c *Context) Printf(format string, args ...interface{}) {
	fmt.Fprintf(c.Stdout, format, args...)
}

func (c *Context) Completed(format string, args ...interface{}) {
	fmt.Fprintf(c.Stdout, ui.Checkmark+" "+format+"\n", args...)
}

func (c *Context) Info(format string, args ...interface{}) {
	fmt.Fprintf(c.Stdout, "  "+format+"\n", args...)
}

func (c *Context) Warn(format string, args ...interface{}) {
	fmt.Fprintf(c.Stdout, "W "+format+"\n", args...)
}

// printConfigWarning renders a config error to stderr. Uses TerminalError
// for rich output when available, otherwise falls back to a plain warning.
//
// errors.AsType rather than a type assertion: a rich error that has been
// wrapped on its way up would otherwise silently fall back to the plain path.
func printConfigWarning(err error) {
	if se, ok := errors.AsType[ui.SeverityTerminalError](err); ok {
		se.WriteWithSeverity(os.Stderr, ui.SeverityWarning)
		return
	}

	if te, ok := errors.AsType[ui.TerminalError](err); ok {
		fmt.Fprint(os.Stderr, "warning: ")
		te.WriteForTerminal(os.Stderr)
		return
	}

	fmt.Fprintf(os.Stderr, "warning: %v\n", err)
}

func (c *Context) Begin(format string, args ...interface{}) {
	fmt.Fprintf(c.Stderr, ui.Play+" "+format+"\n", args...)
}

// DisplayTableTemplate renders a table using a template string to infer headers and data
// Template format: "HEADER1:field1,HEADER2:method2,HEADER3:field3"
func (c *Context) DisplayTableTemplate(template string, items []any) {
	// Parse template
	columns := strings.Split(template, ",")
	headers := make([]string, len(columns))
	accessors := make([]string, len(columns))

	for i, col := range columns {
		parts := strings.Split(col, ":")
		if len(parts) != 2 {
			c.Log.Error("invalid template column format", "column", col)
			return
		}
		headers[i] = parts[0]
		accessors[i] = parts[1]
	}

	// Generate rows
	rows := make([][]string, len(items))
	for i, item := range items {
		row := make([]string, len(columns))
		val := reflect.ValueOf(item)

		for j, accessor := range accessors {
			// Try field first
			field := reflect.Indirect(val).FieldByName(accessor)
			if field.IsValid() {
				// Check if the field is nil
				if field.Kind() == reflect.Pointer && field.IsNil() {
					row[j] = "<nil>"
					continue
				}
				row[j] = fmt.Sprint(field.Interface())
				continue
			}

			// Try method if field not found
			method := val.MethodByName(accessor)
			if method.IsValid() {
				result := method.Call(nil)
				if len(result) > 0 {
					// Check if the result is nil
					if result[0].Kind() == reflect.Pointer && result[0].IsNil() {
						row[j] = "<nil>"
						continue
					}
					row[j] = fmt.Sprint(result[0].Interface())
				}
				continue
			}

			// Neither found
			c.Log.Error("field or method not found", "accessor", accessor, "value", val.Type())
			row[j] = "<error>"
		}
		rows[i] = row
	}

	c.DisplayTable(headers, rows)
}

// DisplayTable renders a formatted table with headers and rows
func (c *Context) DisplayTable(headers []string, rows [][]string) {
	// Validate row lengths
	for i, row := range rows {
		if len(row) != len(headers) {
			c.Log.Error("row has incorrect number of columns",
				"row", i,
				"expected", len(headers),
				"actual", len(row))
			// Pad or truncate the row to match header length
			if len(row) < len(headers) {
				newRow := make([]string, len(headers))
				copy(newRow, row)
				rows[i] = newRow
			} else {
				rows[i] = row[:len(headers)]
			}
		}
	}

	// Define styles
	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(theme.Info). // Blue
		PaddingRight(2)

	cellStyle := lipgloss.NewStyle().
		PaddingRight(2)

	// Calculate column widths
	colWidths := make([]int, len(headers))

	// Check header lengths
	for i, h := range headers {
		colWidths[i] = len(h)
	}

	// Check data lengths
	for _, row := range rows {
		for i, cell := range row {
			if len(cell) > colWidths[i] {
				colWidths[i] = len(cell)
			}
		}
	}

	// Render headers
	headerRow := ""
	for i, header := range headers {
		headerRow += headerStyle.
			Width(colWidths[i] + 2).
			Render(header)
	}
	fmt.Fprintln(c.Stdout, headerRow)

	// Render separator
	sep := ""
	for _, width := range colWidths {
		sep += strings.Repeat("─", width+2)
	}
	fmt.Fprintln(c.Stdout, lipgloss.NewStyle().
		Foreground(theme.Muted). // Gray
		Render(sep))

	// Render data rows
	for _, row := range rows {
		renderedRow := ""
		for i, cell := range row {
			renderedRow += cellStyle.
				Width(colWidths[i] + 2).
				Render(cell)
		}
		fmt.Fprintln(c.Stdout, renderedRow)
	}
}

func (c *Context) RPCClient(name string) (*rpc.NetworkClient, error) {
	// Nothing is printed unless this takes long enough to look like a hang.
	defer c.startConnectNotice().Stop()

	// A cluster was named and we couldn't load it. Connecting to anything else
	// now would answer a question about one cluster with another's data.
	if c.clusterErr != nil {
		return nil, c.unknownClusterError()
	}

	var opts []rpc.StateOption

	opts = append(opts, rpc.WithLogger(c.Log))

	var (
		cs  *rpc.State
		err error
	)

	// A selected cluster is authoritative. If its configuration can't produce a
	// connection, that is the answer — we must not fall through to the branches
	// below, because they connect somewhere else entirely.
	//
	// This used to fall through, and the result was genuinely dangerous:
	// `miren -C some-broken-cluster route list` would quietly answer with the
	// *active* cluster's routes, so you'd be reading production data while
	// believing you were looking at something else. An error about the cluster
	// you named beats correct-looking data about a cluster you didn't.
	if c.ClusterConfig != nil && c.ClientConfig != nil {
		cs, err = c.ClusterConfig.State(c, c.ClientConfig, opts...)
		if err != nil {
			return nil, c.unusableClusterError(err)
		}

		client, err := cs.Client(name)
		if err != nil {
			return nil, c.wrapRPCError(err)
		}
		return client, nil
	}

	if c.ClientConfig != nil {
		cs, err = c.ClientConfig.State(c, opts...)
		if err == nil {
			client, err := cs.Client(name)
			if err != nil {
				return nil, c.wrapRPCError(err)
			}
			return client, nil
		}
		c.Log.Warn("Client config could not provide RPC state", "error", err)
	}

	cs, err = rpc.NewState(c, append(opts, rpc.WithSkipVerify)...)
	if err != nil {
		return nil, err
	}

	client, err := cs.Connect(c.Config.ServerAddress, name)
	if err != nil {
		return nil, c.wrapRPCError(err)
	}

	return client, nil
}

var ErrAccessDenied = errors.New("access denied")
