package commands

import (
	"bufio"
	"fmt"
	"os"
	"slices"
	"strings"
	"syscall"

	"golang.org/x/term"
	"miren.dev/runtime/api/app/app_v1alpha"
)

func Set(ctx *Context, opts struct {
	AppCentric
	Env         []string `short:"e" long:"env" description:"Set environment variables (use KEY to prompt, KEY=VALUE to set directly)"`
	Sensitive   []string `short:"s" long:"sensitive" description:"Set sensitive environment variables (use KEY to prompt with masking, KEY=VALUE to set directly)"`
	Delete      []string `short:"D" long:"delete" description:"Delete environment variables"`
	Concurrency *int     `short:"c" long:"concurrency" description:"Set maximum concurrency of application instances"`
}) error {
	cl, err := ctx.RPCClient("dev.miren.runtime/app")
	if err != nil {
		return err
	}

	ac := app_v1alpha.NewCrudClient(cl)

	res, err := ac.GetConfiguration(ctx, opts.App)
	if err != nil {
		return err
	}

	cfg := res.Configuration()

	var changes bool

	var envvars []*app_v1alpha.NamedValue

	if cfg.HasEnvVars() {
		envvars = cfg.EnvVars()
	}

	// Process all environment variables
	// Note: go-flags doesn't preserve the exact order of mixed flags,
	// so we process regular env vars first, then sensitive ones.
	// Within each group, the order is preserved.
	type envVar struct {
		spec      string
		sensitive bool
	}

	var allVars []envVar

	// Process regular env vars first
	for _, v := range opts.Env {
		allVars = append(allVars, envVar{spec: v, sensitive: false})
	}
	// Then process sensitive env vars
	for _, v := range opts.Sensitive {
		allVars = append(allVars, envVar{spec: v, sensitive: true})
	}

	// Process all variables
	for _, ev := range allVars {
		var key, value string
		var wasFile, wasPrompt bool

		// Check if it's just a key (for prompting) or key=value
		parts := strings.SplitN(ev.spec, "=", 2)
		key = parts[0]

		if len(parts) == 1 {
			// No value provided, prompt for it
			if ev.sensitive {
				promptedValue, err := promptForSensitiveValue(ctx, key)
				if err != nil {
					return fmt.Errorf("failed to read sensitive value for %s: %w", key, err)
				}
				value = promptedValue
			} else {
				promptedValue, err := promptForValue(ctx, key)
				if err != nil {
					return fmt.Errorf("failed to read value for %s: %w", key, err)
				}
				value = promptedValue
			}
			wasPrompt = true
		} else {
			// Value was provided
			value = parts[1]

			if strings.HasPrefix(value, "@") {
				if _, err := os.Stat(value[1:]); err == nil {
					data, err := os.ReadFile(value[1:])
					if err != nil {
						return fmt.Errorf("failed to read env var from file %s: %w", parts[1][1:], err)
					}

					wasFile = true
					value = string(data)
				} else if ev.sensitive {
					ctx.Log.Warn("sensitive env var starts with @ but file does not exist", "file", value[1:])
				}
			}
		}

		// Simply append the new variable - server will handle deduplication with last-value-wins
		changes = true

		if wasFile {
			ctx.Printf("setting %s from file %s...\n", key, parts[1][1:])
		} else if wasPrompt {
			if ev.sensitive {
				ctx.Printf("setting %s (sensitive, from prompt)...\n", key)
			} else {
				ctx.Printf("setting %s (from prompt)...\n", key)
			}
		} else {
			if ev.sensitive {
				ctx.Printf("setting %s (sensitive)...\n", key)
			} else {
				ctx.Printf("setting %s...\n", key)
			}
		}

		var nv app_v1alpha.NamedValue
		nv.SetKey(key)
		nv.SetValue(value)
		nv.SetSensitive(ev.sensitive)

		envvars = append(envvars, &nv)
	}

	for _, v := range opts.Delete {
		envvars = slices.DeleteFunc(envvars, func(nv *app_v1alpha.NamedValue) bool {
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

func promptForValue(ctx *Context, key string) (string, error) {
	// Print prompt message
	fmt.Fprintf(os.Stderr, "Enter value for variable '%s': ", key)

	// Read line from stdin
	reader := bufio.NewReader(os.Stdin)
	value, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(value), nil
}

func promptForSensitiveValue(ctx *Context, key string) (string, error) {
	// Print prompt message
	fmt.Fprintf(os.Stderr, "Enter value for sensitive variable '%s': ", key)

	// Read password with terminal masking
	bytePassword, err := term.ReadPassword(int(syscall.Stdin))
	if err != nil {
		return "", err
	}

	// Print newline after password input
	fmt.Fprintln(os.Stderr)

	return strings.TrimSpace(string(bytePassword)), nil
}
