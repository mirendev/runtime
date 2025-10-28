package commands

import (
	"fmt"
	"time"

	"miren.dev/runtime/api/app/app_v1alpha"
	"miren.dev/runtime/api/compute"
	"miren.dev/runtime/appconfig"
	"miren.dev/runtime/pkg/rpc/standard"
)

func Logs(ctx *Context, opts struct {
	ConfigCentric

	App     string         `short:"a" long:"app" description:"Application get logs for" env:"MIREN_APP"`
	Dir     string         `short:"d" long:"dir" description:"Directory to run from" default:"."`
	Last    *time.Duration `short:"l" long:"last" description:"Show logs from the last duration"`
	Sandbox string         `short:"s" long:"sandbox" description:"Show logs for a specific sandbox ID"`
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

	ac := app_v1alpha.LogsClient{Client: cl}

	// If sandbox ID is provided, resolve it to the full entity ID
	// (logs are stored with the full entity ID, not the prefixed name)
	if opts.Sandbox != "" {
		entityClient, err := ctx.RPCClient("entities")
		if err != nil {
			return err
		}

		computeClient := compute.NewClient(ctx.Log, entityClient)

		// Get the sandbox to retrieve its full entity ID
		// This will try both Get (name-based) and GetById (ID-based) lookups
		sandbox, err := computeClient.GetSandbox(ctx, opts.Sandbox)
		if err != nil {
			return fmt.Errorf("failed to find sandbox %s: %w", opts.Sandbox, err)
		}

		// Use the full entity ID for log queries
		opts.Sandbox = sandbox.ID.String()
	}

	typ := map[string]string{
		"stdout":   "S",
		"stderr":   "E",
		"error":    "ERR",
		"user-oob": "U",
	}

	var ts *standard.Timestamp

	if opts.Last != nil {
		ts = standard.ToTimestamp(time.Now().Add(-*opts.Last))
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

		if opts.Sandbox != "" {
			// opts.Sandbox has already been resolved to the full entity ID above
			res, err = ac.SandboxLogs(ctx, opts.Sandbox, ts, false)
		} else {
			res, err = ac.AppLogs(ctx, opts.App, ts, false)
		}

		if err != nil {
			return err
		}

		logs := res.Logs()

		for _, l := range logs {
			ctx.Printf("%s %s: %s\n",
				typ[l.Stream()],
				standard.FromTimestamp(l.Timestamp()).Format("2006-01-02 15:04:05"),
				l.Line())
		}

		if len(logs) != 100 {
			break
		}

		// For pagination, use the last log's timestamp + 1 microsecond to avoid duplicates
		lastTime := standard.FromTimestamp(logs[len(logs)-1].Timestamp())
		// Add 1 microsecond to exclude the last log from the next query
		nextTime := lastTime.Add(time.Microsecond)
		ts = standard.ToTimestamp(nextTime)
	}

	return nil
}
