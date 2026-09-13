package commands

import (
	"encoding/json"
	"errors"
	"fmt"
	"runtime/debug"
	"time"

	"miren.dev/runtime/version"
)

type VersionOptions struct {
	ConfigCentric
	Deps   bool   `long:"deps" description:"Show dependencies"`
	Format string `long:"format" description:"Output format (text, json)" default:"text"`
	Server bool   `short:"s" long:"server" description:"Also report the version of the active cluster's server"`
}

// versionReport is the --server shape. Without --server the output stays the
// flat build info pkg/release already parses.
type versionReport struct {
	CLI         version.Info   `json:"cli"`
	Cluster     string         `json:"cluster,omitempty"`
	Server      *serverVersion `json:"server,omitempty"`
	ServerError string         `json:"server_error,omitempty"`
	Skew        string         `json:"skew,omitempty"`
	Advice      string         `json:"advice,omitempty"`
}

func Version(ctx *Context, opts VersionOptions) error {
	info := version.GetInfo()

	if opts.Server {
		if opts.Deps {
			return fmt.Errorf("--deps and --server cannot be combined")
		}
		return versionWithServer(ctx, info, opts.Format == "json")
	}

	if opts.Format == "json" {
		jsonStr, err := info.JSON()
		if err != nil {
			return fmt.Errorf("failed to marshal version info: %w", err)
		}
		fmt.Println(jsonStr)
		return nil
	}

	if opts.Deps {
		bi, ok := debug.ReadBuildInfo()
		if ok {
			ctx.Log.Info("go build info",
				"main", bi.Main.Path,
				"version", bi.Main.Version,
				"go", bi.GoVersion,
			)

			for _, setting := range bi.Settings {
				ctx.Log.Debug("build setting", "key", setting.Key, "value", setting.Value)
			}

			for _, dep := range bi.Deps {
				ctx.Printf("%s (%s)\n", dep.Path, dep.Version)
			}
		}
		return nil
	}

	fmt.Println(info.String())
	return nil
}

func versionWithServer(ctx *Context, cli version.Info, asJSON bool) error {
	report := versionReport{CLI: cli, Cluster: ctx.ClusterName}

	server, err := fetchServerVersion(ctx)
	if err != nil {
		report.ServerError = err.Error()
		if errors.Is(err, errServerVersionUnsupported) {
			report.Skew = "server_behind"
			report.Advice = serverBehindAdvice
		}
	} else {
		report.Server = server
		report.Skew, report.Advice = describeSkew(compareVersions(cli.Version, server.Version))
	}

	if asJSON {
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
		return nil
	}

	ctx.Printf("CLI\n")
	printBuildLines(ctx, cli.Version, cli.Commit, cli.BuildDate)

	if report.Cluster != "" {
		ctx.Printf("\nServer (%s)\n", report.Cluster)
	} else {
		ctx.Printf("\nServer\n")
	}
	if server == nil {
		if errors.Is(err, errServerVersionUnsupported) {
			ctx.Printf("  Version: not reported (server predates version reporting)\n")
		} else {
			ctx.Printf("  Version: unavailable\n")
			ctx.Printf("  Error:   %v\n", err)
		}
	} else {
		printBuildLines(ctx, server.Version, server.Commit, server.BuildDate)
		ctx.Printf("  Instance: %s\n", server.InstanceID)
		if !server.StartedAt.IsZero() {
			started := server.StartedAt.Local().Format("2006-01-02 15:04:05 MST")
			// Clock skew between client and server can make this negative.
			if uptime := time.Since(server.StartedAt); uptime >= 0 {
				ctx.Printf("  Started:  %s (up %s)\n", started, roundUptime(uptime))
			} else {
				ctx.Printf("  Started:  %s\n", started)
			}
		}
		if server.Ready {
			ctx.Printf("  Ready:    yes\n")
		} else {
			ctx.Printf("  Ready:    no (still starting)\n")
		}
		if server.InstallKind != "" {
			ctx.Printf("  Install:  %s\n", server.InstallKind)
		}
	}

	if report.Advice != "" {
		ctx.Printf("\n")
		ctx.Warn("%s", report.Advice)
	}
	return nil
}

func printBuildLines(ctx *Context, ver, commit string, built time.Time) {
	ctx.Printf("  Version:  %s\n", ver)
	if commit != "" && commit != "unknown" {
		ctx.Printf("  Commit:   %s\n", commit)
	}
	if !built.IsZero() {
		ctx.Printf("  Built:    %s\n", built.Format("2006-01-02 15:04:05 UTC"))
	}
}

const (
	serverBehindAdvice = "The server is older than this CLI. Run 'sudo miren server upgrade' on the server host to update it."
	cliBehindAdvice    = "This CLI is older than the server. Run 'miren upgrade' to update it."
)

func describeSkew(skew versionSkew) (name, advice string) {
	switch skew {
	case skewServerBehind:
		return "server_behind", serverBehindAdvice
	case skewCLIBehind:
		return "cli_behind", cliBehindAdvice
	case skewUnordered:
		return "different", ""
	case skewNone:
	}
	return "none", ""
}

func roundUptime(d time.Duration) time.Duration {
	switch {
	case d > 24*time.Hour:
		return d.Round(time.Hour)
	case d > time.Hour:
		return d.Round(time.Minute)
	default:
		return d.Round(time.Second)
	}
}
