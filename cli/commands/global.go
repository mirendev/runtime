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
	"miren.dev/runtime/pkg/asm"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/slogfmt"
	"miren.dev/runtime/pkg/slogrus"
	"miren.dev/runtime/pkg/ui"
)

type GlobalFlags struct {
	Verbose       []bool `short:"v" long:"verbose" description:"Enable verbose output"`
	ServerAddress string `long:"server-address" description:"Server address to connect to" default:"127.0.0.1:8443" asm:"server-addr"`
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

	Client *asm.Registry
	Server *asm.Registry

	cancels []func()

	ClientConfig  *clientconfig.Config
	ClusterConfig *clientconfig.ClusterConfig
	ClusterName   string

	Config struct {
		ServerAddress string `asm:"server-addr"`
	}

	levelVar slog.LevelVar
	exitCode int
}

func (c *Context) Level() slog.Level {
	return c.levelVar.Level()
}

func setup(ctx context.Context, flags *GlobalFlags, opts any) *Context {
	s := &Context{
		verbose: len(flags.Verbose),
		Stdout:  os.Stdout,
		Stderr:  os.Stderr,

		Server: &asm.Registry{},
		Client: &asm.Registry{},
	}

	var level slog.Level

	switch s.verbose {
	case 0:
		level = slog.LevelWarn
	case 1:
		level = slog.LevelInfo
	case 2:
		level = slog.LevelDebug
	default:
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

	s.Server.Log = s.Log
	s.Client.Log = s.Log

	s.setupServerComponents(ctx, s.Server)

	s.Server.InferFrom(opts, true)
	s.Client.InferFrom(flags, true)

	s.Client.Populate(&s.Config)

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

	if lc, ok := opts.(interface {
		LoadCluster() (*clientconfig.ClusterConfig, string, error)
	}); ok {
		cfg, name, err := lc.LoadCluster()
		if cfg != nil {
			s.ClusterConfig = cfg
			s.ClusterName = name
		} else if err != nil && !errors.Is(err, clientconfig.ErrNoConfig) {
			s.Log.Warn("Failed to load cluster config", "error", err)
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
				if field.Kind() == reflect.Ptr && field.IsNil() {
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
					if result[0].Kind() == reflect.Ptr && result[0].IsNil() {
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
		Foreground(lipgloss.Color("12")). // Blue
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
		Foreground(lipgloss.Color("8")). // Gray
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
	var opts []rpc.StateOption

	opts = append(opts, rpc.WithLogger(c.Log))

	var (
		cs  *rpc.State
		err error
	)

	if c.ClusterConfig != nil && c.ClientConfig != nil {
		cs, err = c.ClusterConfig.State(c, c.ClientConfig, opts...)
		if err == nil {
			client, err := cs.Client(name)
			if err != nil {
				return nil, c.wrapRPCError(err)
			}
			return client, nil
		}
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

// wrapRPCError wraps RPC errors with user-friendly messages
func (c *Context) wrapRPCError(err error) error {
	var resolveErr *rpc.ResolveError
	if errors.As(err, &resolveErr) && resolveErr.StatusCode == 401 {
		clusterName := c.ClusterName
		if clusterName == "" {
			clusterName = "the cluster"
		}

		c.Warn("access denied: you don't have permission to access %s\nPlease check your credentials or request access from the cluster administrator", clusterName)
		return ErrAccessDenied
	}
	return err
}
