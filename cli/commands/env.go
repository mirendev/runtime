package commands

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"miren.dev/runtime/api/app/app_v1alpha"
	"miren.dev/runtime/pkg/ui"
)

func EnvSet(ctx *Context, opts struct {
	AppCentric
	Service   string   `short:"S" long:"service" description:"Set env var for specific service only (if not specified, sets for all services)"`
	Env       []string `short:"e" long:"env" description:"Set environment variables (use KEY to prompt, KEY=VALUE to set directly, KEY=@file to read from file)"`
	Sensitive []string `short:"s" long:"sensitive" description:"Set sensitive environment variables (use KEY to prompt with masking, KEY=VALUE to set directly, KEY=@file to read from file)"`
}) error {
	cl, err := ctx.RPCClient("dev.miren.runtime/app")
	if err != nil {
		return err
	}

	ac := app_v1alpha.NewCrudClient(cl)

	// Collect all env vars to set
	type envVar struct {
		spec      string
		sensitive bool
	}

	var allVars []envVar
	for _, v := range opts.Env {
		allVars = append(allVars, envVar{spec: v, sensitive: false})
	}
	for _, v := range opts.Sensitive {
		allVars = append(allVars, envVar{spec: v, sensitive: true})
	}

	if len(allVars) == 0 {
		return fmt.Errorf("no environment variables specified")
	}

	var lastVersionId string

	for _, ev := range allVars {
		var key, value string
		var wasFile, wasPrompt bool

		parts := strings.SplitN(ev.spec, "=", 2)
		key = parts[0]

		if key == "" {
			return fmt.Errorf("invalid environment variable: key cannot be empty")
		}

		if len(parts) == 1 {
			// Prompt for value
			var label string
			if ev.sensitive {
				label = fmt.Sprintf("Enter value for sensitive variable '%s'", key)
			} else {
				label = fmt.Sprintf("Enter value for variable '%s'", key)
			}

			promptedValue, err := ui.PromptForInput(
				ui.WithLabel(label),
				ui.WithSensitive(ev.sensitive),
			)
			if err != nil {
				return fmt.Errorf("failed to read value for %s: %w", key, err)
			}
			value = promptedValue
			wasPrompt = true
		} else {
			value = parts[1]

			if strings.HasPrefix(value, "@") {
				filename := value[1:]
				data, err := os.ReadFile(filename)
				if err != nil {
					if os.IsNotExist(err) {
						return fmt.Errorf("env var references file %s which does not exist", filename)
					}
					return fmt.Errorf("failed to read env var from file %s: %w", filename, err)
				}
				wasFile = true
				value = string(data)
			}
		}

		// Log what we're doing
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

		// Use the targeted SetEnvVar RPC - server handles source tracking
		res, err := ac.SetEnvVar(ctx, opts.App, key, value, ev.sensitive, opts.Service)
		if err != nil {
			return err
		}
		lastVersionId = res.VersionId()
	}

	ctx.Printf("new version id: %s\n", lastVersionId)
	return nil
}

func EnvGet(ctx *Context, opts struct {
	AppCentric
	Unmask bool `short:"u" long:"unmask" description:"Show actual value of sensitive variables instead of masking them"`
	Args   struct {
		Key string `positional-arg-name:"KEY" description:"Environment variable key to get" required:"1"`
	} `positional-args:"yes" required:"true"`
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

	if !cfg.HasEnvVars() {
		return fmt.Errorf("environment variable %s not found", opts.Args.Key)
	}

	// Find the variable with the specified key
	envvars := cfg.EnvVars()
	for _, nv := range envvars {
		if nv.Key() == opts.Args.Key {
			if nv.Sensitive() {
				if opts.Unmask {
					ctx.Printf("%s\n", nv.Value())
				} else {
					ctx.Printf("••••••••••\n")
				}
			} else {
				ctx.Printf("%s\n", nv.Value())
			}
			return nil
		}
	}

	return fmt.Errorf("environment variable %s not found", opts.Args.Key)
}

func EnvList(ctx *Context, opts struct {
	FormatOptions
	AppCentric
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

	if !cfg.HasEnvVars() {
		if opts.IsJSON() {
			return PrintJSON([]any{})
		}
		ctx.Printf("No environment variables set\n")
		return nil
	}

	// Get all environment variables
	envvars := cfg.EnvVars()

	// Sort by key for consistent output
	sort.Slice(envvars, func(i, j int) bool {
		return envvars[i].Key() < envvars[j].Key()
	})

	// For JSON output
	if opts.IsJSON() {
		type EnvVar struct {
			Name      string `json:"name"`
			Value     string `json:"value,omitempty"`
			Sensitive bool   `json:"sensitive"`
		}

		var vars []EnvVar
		for _, nv := range envvars {
			ev := EnvVar{
				Name:      nv.Key(),
				Sensitive: nv.Sensitive(),
			}
			// Only include value for non-sensitive variables in JSON
			if !nv.Sensitive() {
				ev.Value = nv.Value()
			}
			vars = append(vars, ev)
		}
		return PrintJSON(vars)
	}

	// Create and print the table
	printEnvTable(ctx, envvars)

	return nil
}

func EnvDelete(ctx *Context, opts struct {
	AppCentric
	Service string `short:"S" long:"service" description:"Delete env var from specific service only (if not specified, deletes global env var)"`
	Force   bool   `short:"f" long:"force" description:"Skip confirmation prompt"`
	Args    struct {
		Keys []string `positional-arg-name:"KEY" description:"Environment variable key to delete" required:"1"`
	} `positional-args:"yes"`
}) error {
	if len(opts.Args.Keys) == 0 {
		return fmt.Errorf("no environment variables specified")
	}

	cl, err := ctx.RPCClient("dev.miren.runtime/app")
	if err != nil {
		return err
	}

	ac := app_v1alpha.NewCrudClient(cl)

	// Ask for confirmation unless --force is used
	if !opts.Force {
		var message string
		if len(opts.Args.Keys) == 1 {
			message = fmt.Sprintf("Delete environment variable '%s'?", opts.Args.Keys[0])
		} else {
			message = fmt.Sprintf("Delete %d environment variables: %s?",
				len(opts.Args.Keys), strings.Join(opts.Args.Keys, ", "))
		}

		confirmed, err := ui.Confirm(
			ui.WithMessage(message),
			ui.WithDefault(false),
		)
		if err != nil {
			return fmt.Errorf("confirmation cancelled: %w", err)
		}
		if !confirmed {
			ctx.Printf("deletion cancelled\n")
			return nil
		}
	}

	var lastVersionId string
	var configVarsDeleted []string

	// Delete each variable using the targeted RPC
	for _, key := range opts.Args.Keys {
		ctx.Printf("deleting %s...\n", key)
		res, err := ac.DeleteEnvVar(ctx, opts.App, key, opts.Service)
		if err != nil {
			return err
		}
		lastVersionId = res.VersionId()

		// Track config-sourced vars for warning
		if res.DeletedSource() == "config" {
			configVarsDeleted = append(configVarsDeleted, key)
		}
	}

	ctx.Printf("new version id: %s\n", lastVersionId)

	// Warn about config vars that will reappear on next deploy
	if len(configVarsDeleted) > 0 {
		if len(configVarsDeleted) == 1 {
			ctx.Printf("\nWarning: %s was defined in app.toml and will reappear on next deploy.\n", configVarsDeleted[0])
			ctx.Printf("To permanently remove it, delete it from .miren/app.toml.\n")
		} else {
			ctx.Printf("\nWarning: %s were defined in app.toml and will reappear on next deploy.\n", strings.Join(configVarsDeleted, ", "))
			ctx.Printf("To permanently remove them, delete them from .miren/app.toml.\n")
		}
	}

	return nil
}

// printEnvTable prints a formatted table of environment variables
func printEnvTable(ctx *Context, envvars []*app_v1alpha.NamedValue) {
	// Create a gray style for sensitive values
	grayStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))

	// Build rows
	var rows []ui.Row
	for _, nv := range envvars {
		var value string

		if nv.Sensitive() {
			value = grayStyle.Render("••••••••••")
		} else {
			value = nv.Value()
		}

		// Get source with backward compatibility
		source := nv.Source()
		if source == "" {
			source = "config"
		}

		rows = append(rows, ui.Row{nv.Key(), value, "(all)", source})
	}

	// Auto-size columns with reasonable maximums
	columns := ui.AutoSizeColumns(
		[]string{"NAME", "VALUE", "SERVICE", "SOURCE"},
		rows,
		30, // max width for NAME column
		40, // max width for VALUE column
		15, // max width for SERVICE column
		12, // max width for SOURCE column
	)

	// Create and render the table
	table := ui.NewTable(
		ui.WithColumns(columns),
		ui.WithRows(rows),
	)

	ctx.Printf("%s\n", table.Render())
}
