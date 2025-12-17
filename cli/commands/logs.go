package commands

import (
	"fmt"
	"strings"
	"time"

	"miren.dev/runtime/api/app/app_v1alpha"
	"miren.dev/runtime/appconfig"
	"miren.dev/runtime/pkg/logfilter"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/rpc/standard"
	"miren.dev/runtime/pkg/rpc/stream"
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
	serviceFilter := fmt.Sprintf("service:%q", service)
	if userFilter == "" {
		return serviceFilter
	}
	return serviceFilter + " " + userFilter
}

func Logs(ctx *Context, opts struct {
	ConfigCentric

	App     string         `short:"a" long:"app" description:"Application get logs for" env:"MIREN_APP"`
	Dir     string         `short:"d" long:"dir" description:"Directory to run from" default:"."`
	Last    *time.Duration `short:"l" long:"last" description:"Show logs from the last duration"`
	Sandbox string         `short:"s" long:"sandbox" description:"Show logs for a specific sandbox ID"`
	Follow  bool           `short:"f" long:"follow" description:"Follow log output (live tail)"`
	Filter  string         `short:"g" long:"grep" description:"Filter logs (e.g., 'error', '\"exact phrase\"', 'error -debug', '/regex/')"`
	Service string         `long:"service" description:"Filter logs by service name (e.g., 'web', 'worker')"`
}) error {
	// Check for conflicting options
	if opts.App != "" && opts.Sandbox != "" {
		return fmt.Errorf("cannot specify both --app and --sandbox")
	}

	// If neither is specified, try to load app from directory context
	if opts.App == "" && opts.Sandbox == "" {
		var ac *appconfig.AppConfig
		var err error

		if opts.Dir != "." {
			ac, err = appconfig.LoadAppConfigUnder(opts.Dir)
		} else {
			ac, err = appconfig.LoadAppConfig()
		}

		if err == nil && ac != nil && ac.Name != "" {
			opts.App = ac.Name
		} else {
			return fmt.Errorf("must specify either --app or --sandbox, or run from an app directory")
		}
	}

	cl, err := ctx.RPCClient("dev.miren.runtime/logs")
	if err != nil {
		return err
	}

	// Normalize sandbox ID to include the "sandbox/" prefix for log queries
	if opts.Sandbox != "" {
		opts.Sandbox = normalizeSandboxID(opts.Sandbox)
	}

	// Parse filter early to validate syntax
	var filter *logfilter.Filter
	if opts.Filter != "" {
		var err error
		filter, err = logfilter.Parse(opts.Filter)
		if err != nil {
			return fmt.Errorf("invalid filter: %w", err)
		}
	}

	// Build combined filter with service filter for server-side filtering
	combinedFilter := buildFilterWithService(opts.Filter, opts.Service)

	// Check if server supports streaming (prefer chunked for efficiency)
	if cl.HasMethod(ctx, "streamLogChunks") {
		return streamLogChunks(ctx, cl, opts.App, opts.Sandbox, opts.Last, opts.Follow, combinedFilter)
	}

	// Older server - warn about upgrade and limited functionality
	ctx.Printf("Warning: server does not support optimized log streaming. Upgrade your server for better performance and --service filtering.\n")
	if opts.Service != "" {
		return fmt.Errorf("--service filtering requires a newer server version")
	}

	if cl.HasMethod(ctx, "streamLogs") {
		return streamLogs(ctx, cl, opts.App, opts.Sandbox, opts.Last, opts.Follow, filter)
	}

	// Warn if --follow requested but not supported
	if opts.Follow {
		ctx.Printf("Warning: server does not support --follow, showing recent logs only\n")
	}

	// Fall back to legacy pagination
	return legacyLogs(ctx, cl, opts.App, opts.Sandbox, opts.Last, filter)
}

var streamTypePrefixes = map[string]string{
	"stdout":   "S",
	"stderr":   "E",
	"error":    "ERR",
	"user-oob": "U",
}

func streamLogs(ctx *Context, cl *rpc.NetworkClient, app, sandbox string, last *time.Duration, follow bool, filter *logfilter.Filter) error {
	ac := app_v1alpha.LogsClient{Client: cl}

	// Build target
	target := &app_v1alpha.LogTarget{}
	if sandbox != "" {
		target.SetSandbox(sandbox)
	} else {
		target.SetApp(app)
	}

	// Determine start time
	var ts *standard.Timestamp
	if last != nil {
		ts = standard.ToTimestamp(time.Now().Add(-*last))
	} else if !follow {
		// For non-follow mode without explicit --last, default to today
		start := time.Now()
		start = time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, start.Location())
		ts = standard.ToTimestamp(start)
	}
	// For follow mode without --last, ts is nil which means start from now

	// Create callback to print logs as they arrive
	callback := stream.Callback(func(l *app_v1alpha.LogEntry) error {
		// Apply local filter if provided
		if filter != nil && !filter.Match(l.Line()) {
			return nil
		}

		prefix := ""
		if l.HasSource() && l.Source() != "" {
			source := l.Source()
			if len(source) > 12 {
				source = source[:3] + "…" + source[len(source)-8:]
			}
			prefix = "[" + source + "] "
		}
		ctx.Printf("%s %s: %s%s\n",
			streamTypePrefixes[l.Stream()],
			standard.FromTimestamp(l.Timestamp()).Format("2006-01-02 15:04:05"),
			prefix,
			l.Line())
		return nil
	})

	_, err := ac.StreamLogs(ctx, target, ts, follow, callback)
	return err
}

func streamLogChunks(ctx *Context, cl *rpc.NetworkClient, app, sandbox string, last *time.Duration, follow bool, filter string) error {
	ac := app_v1alpha.LogsClient{Client: cl}

	// Build target
	target := &app_v1alpha.LogTarget{}
	if sandbox != "" {
		target.SetSandbox(sandbox)
	} else {
		target.SetApp(app)
	}

	// Determine start time
	var ts *standard.Timestamp
	if last != nil {
		ts = standard.ToTimestamp(time.Now().Add(-*last))
	} else if !follow {
		// For non-follow mode without explicit --last, default to today
		start := time.Now()
		start = time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, start.Location())
		ts = standard.ToTimestamp(start)
	}
	// For follow mode without --last, ts is nil which means start from now

	// Create callback to print logs as they arrive in chunks
	callback := stream.Callback(func(chunk *app_v1alpha.LogChunk) error {
		for _, l := range chunk.Entries() {
			prefix := ""
			if l.HasSource() && l.Source() != "" {
				source := l.Source()
				if len(source) > 12 {
					source = source[:3] + "…" + source[len(source)-8:]
				}
				prefix = "[" + source + "] "
			}
			ctx.Printf("%s %s: %s%s\n",
				streamTypePrefixes[l.Stream()],
				standard.FromTimestamp(l.Timestamp()).Format("2006-01-02 15:04:05"),
				prefix,
				l.Line())
		}
		return nil
	})

	_, err := ac.StreamLogChunks(ctx, target, ts, follow, filter, callback)
	return err
}

func legacyLogs(ctx *Context, cl *rpc.NetworkClient, app, sandbox string, last *time.Duration, filter *logfilter.Filter) error {
	ac := app_v1alpha.LogsClient{Client: cl}

	var ts *standard.Timestamp

	if last != nil {
		ts = standard.ToTimestamp(time.Now().Add(-*last))
	} else {
		start := time.Now()
		start = time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, start.Location())
		ts = standard.ToTimestamp(start)
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

			prefix := ""
			if l.HasSource() && l.Source() != "" {
				source := l.Source()
				if len(source) > 12 {
					source = source[:3] + "…" + source[len(source)-8:]
				}
				prefix = "[" + source + "] "
			}
			ctx.Printf("%s %s: %s%s\n",
				streamTypePrefixes[l.Stream()],
				standard.FromTimestamp(l.Timestamp()).Format("2006-01-02 15:04:05"),
				prefix,
				l.Line())
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
