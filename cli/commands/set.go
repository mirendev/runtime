package commands

import (
	"fmt"
	"slices"
	"strings"

	"miren.dev/runtime/app"
)

func Set(ctx *Context, opts struct {
	App         string   `short:"a" long:"app" description:"Application to set"`
	Env         []string `short:"e" long:"env" description:"Set environment variables"`
	Sensitive   []string `short:"s" long:"sensitive" description:"Set sensitive environment variables"`
	Delete      []string `short:"d" long:"delete" description:"Delete environment variables"`
	Concurrency *int     `short:"c" long:"concurrency" description:"Set maximum concurrency of application instances"`
}) error {
	cl, err := ctx.RPCClient("app")
	if err != nil {
		return err
	}

	ac := app.CrudClient{Client: cl}

	res, err := ac.GetConfiguration(ctx, opts.App)
	if err != nil {
		return err
	}

	cfg := res.Configuration()

	var changes bool

	var envvars []*app.NamedValue

	if cfg.HasEnvVars() {
		envvars = cfg.EnvVars()
	}

	if len(opts.Env) > 0 {
		for _, v := range opts.Env {
			parts := strings.SplitN(v, "=", 2)
			if len(parts) != 2 {
				return fmt.Errorf("invalid env var: %s", v)
			}

			idx := slices.IndexFunc(envvars, func(nv *app.NamedValue) bool {
				return nv.Key() == parts[0]
			})

			if idx == -1 {
				changes = true
				ctx.Printf("adding %s...\n", parts[0])

				var nv app.NamedValue

				nv.SetKey(parts[0])
				nv.SetValue(parts[1])

				envvars = append(envvars, &nv)
			} else if envvars[idx].Value() != parts[1] {
				changes = true
				ctx.Printf("updating %s...\n", parts[0])
				envvars[idx].SetValue(parts[1])
			}
		}
	}

	if len(opts.Sensitive) > 0 {
		for _, v := range opts.Sensitive {
			parts := strings.SplitN(v, "=", 2)
			if len(parts) != 2 {
				return fmt.Errorf("invalid env var: %s", v)
			}

			idx := slices.IndexFunc(envvars, func(nv *app.NamedValue) bool {
				return nv.Key() == parts[0] && nv.Sensitive()
			})

			if idx == -1 {
				changes = true
				ctx.Printf("adding %s...\n", parts[0])

				var nv app.NamedValue

				nv.SetKey(parts[0])
				nv.SetValue(parts[1])
				nv.SetSensitive(true)

				envvars = append(envvars, &nv)
			} else if envvars[idx].Value() != parts[1] {
				changes = true
				ctx.Printf("updating %s...\n", parts[0])
				envvars[idx].SetValue(parts[1])
			}
		}
	}

	for _, v := range opts.Delete {
		envvars = slices.DeleteFunc(envvars, func(nv *app.NamedValue) bool {
			if nv.Key() == v {
				changes = true
				ctx.Printf("deleting %s...\n", v)
				return true
			}

			return false
		})
	}

	if opts.Concurrency != nil && cfg.Concurrency() != int32(*opts.Concurrency) {
		changes = true
		ctx.Printf("setting concurrency to %d...\n", *opts.Concurrency)
		cfg.SetConcurrency(int32(*opts.Concurrency))
	}

	if !changes {
		ctx.Printf("no changes to configuration\n")
		return nil
	}

	cfg.SetEnvVars(envvars)

	setres, err := ac.SetConfiguration(ctx, opts.App, cfg)
	if err != nil {
		return err
	}

	ctx.Printf("new version id: %s\n", setres.VersionId())

	return nil
}
