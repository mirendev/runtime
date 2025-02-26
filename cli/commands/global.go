package commands

import (
	"context"
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

	Stdout io.Writer
	Stderr io.Writer

	Client *asm.Registry
	Server *asm.Registry

	cancels []func()

	ClientConfig  *clientconfig.Config
	ClusterConfig *clientconfig.ClusterConfig

	Config struct {
		ServerAddress string `asm:"server-addr"`
	}

	exitCode int
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

	dynLevel := new(slog.LevelVar)
	dynLevel.Set(level)

	s.Log = slog.New(slogfmt.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: dynLevel,
	}))

	ctx = slogrus.WithLogger(ctx, s.Log)
	slogrus.OverrideGlobal(s.Log)

	s.Server.Log = s.Log
	s.Client.Log = s.Log

	s.setupServerComponents(ctx, s.Server)

	s.Server.InferFrom(opts)
	s.Client.InferFrom(flags)

	s.Client.Populate(&s.Config)

	if lc, ok := opts.(interface {
		LoadConfig() (*clientconfig.Config, error)
	}); ok {
		cfg, err := lc.LoadConfig()
		if err == nil {
			s.ClientConfig = cfg
		} else {
			s.Log.Warn("Failed to load client config", "error", err)
		}
	}

	if lc, ok := opts.(interface {
		LoadCluster() (*clientconfig.ClusterConfig, error)
	}); ok {
		cfg, err := lc.LoadCluster()
		if err == nil {
			s.ClusterConfig = cfg
		} else {
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
					target = dynLevel.Level() - 4
				case unix.SIGTTOU:
					target = dynLevel.Level() + 4
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

				if dynLevel.Level() == target {
					continue
				}

				dynLevel.Set(target)

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

func (c *Context) RPCClient(name string) (*rpc.Client, error) {
	var opts []rpc.StateOption

	opts = append(opts, rpc.WithLogger(c.Log))

	if c.ClusterConfig != nil {
		opts = append(opts,
			rpc.WithCertPEMs([]byte(c.ClusterConfig.ClientCert), []byte(c.ClusterConfig.ClientKey)),
			rpc.WithCertificateVerification([]byte(c.ClusterConfig.CACert)),
		)
	} else {
		opts = append(opts, rpc.WithSkipVerify)
	}

	cs, err := rpc.NewState(c, opts...)
	if err != nil {
		return nil, err
	}

	client, err := cs.Connect(c.Config.ServerAddress, name)
	if err != nil {
		return nil, err
	}

	return client, nil
}
