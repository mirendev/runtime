package commands

import (
	"fmt"
	"runtime/debug"

	"miren.dev/runtime/version"
)

func Version(ctx *Context, opts struct {
	Deps bool `long:"deps" description:"Show dependencies"`
}) error {
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

		if opts.Deps {
			for _, dep := range bi.Deps {
				ctx.Printf("%s (%s)\n", dep.Path, dep.Version)
			}

			return nil
		}
	}

	fmt.Println(version.Version)
	return nil
}
