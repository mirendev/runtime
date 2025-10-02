package commands

import (
	"fmt"
	"runtime/debug"

	"miren.dev/runtime/version"
)

type VersionOptions struct {
	Deps   bool   `long:"deps" description:"Show dependencies"`
	Format string `long:"format" description:"Output format (text, json)" default:"text"`
}

func Version(ctx *Context, opts VersionOptions) error {
	// Get version info
	info := version.GetInfo()

	// Handle JSON format
	if opts.Format == "json" {
		jsonStr, err := info.JSON()
		if err != nil {
			return fmt.Errorf("failed to marshal version info: %w", err)
		}
		fmt.Println(jsonStr)
		return nil
	}

	// Handle dependencies flag
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

	// Default text format
	fmt.Println(info.String())
	return nil
}
