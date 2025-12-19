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
	Key     string `position:"0" usage:"Environment variable key to get" required:"true"`
	Service string `short:"S" long:"service" description:"Get env var for specific service (if not specified, gets global env var)"`
	Unmask  bool   `short:"u" long:"unmask" description:"Show actual value of sensitive variables instead of masking them"`
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

	var found *app_v1alpha.NamedValue

	if opts.Service != "" {
		// Look in service-specific env vars
		if cfg.HasServices() {
			for _, svc := range cfg.Services() {
				if svc.Service() == opts.Service && svc.HasServiceEnv() {
					for _, nv := range svc.ServiceEnv() {
						if nv.Key() == opts.Key {
							found = nv
							break
						}
					}
					break
				}
			}
		}
		if found == nil {
			return fmt.Errorf("environment variable %s not found for service %s", opts.Key, opts.Service)
		}
	} else {
		// Look in global env vars
		if cfg.HasEnvVars() {
			for _, nv := range cfg.EnvVars() {
				if nv.Key() == opts.Key {
					found = nv
					break
				}
			}
		}
		if found == nil {
			return fmt.Errorf("environment variable %s not found", opts.Key)
		}
	}

	if found.Sensitive() {
		if opts.Unmask {
			ctx.Printf("%s\n", found.Value())
		} else {
			ctx.Printf("••••••••••\n")
		}
	} else {
		ctx.Printf("%s\n", found.Value())
	}
	return nil
}

// envVarEntry combines a NamedValue with its service scope
type envVarEntry struct {
	nv      *app_v1alpha.NamedValue
	service string // empty string means global (all services)
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

	// Collect all env vars: global + per-service
	var entries []envVarEntry

	// Add global env vars
	if cfg.HasEnvVars() {
		for _, nv := range cfg.EnvVars() {
			entries = append(entries, envVarEntry{nv: nv, service: ""})
		}
	}

	// Add per-service env vars
	if cfg.HasServices() {
		for _, svc := range cfg.Services() {
			if svc.HasServiceEnv() {
				for _, nv := range svc.ServiceEnv() {
					entries = append(entries, envVarEntry{nv: nv, service: svc.Service()})
				}
			}
		}
	}

	if len(entries) == 0 {
		if opts.IsJSON() {
			return PrintJSON([]any{})
		}
		ctx.Printf("No environment variables set\n")
		return nil
	}

	// Sort by key, then by service for consistent output
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].nv.Key() != entries[j].nv.Key() {
			return entries[i].nv.Key() < entries[j].nv.Key()
		}
		// Global (empty service) comes before specific services
		if entries[i].service == "" && entries[j].service != "" {
			return true
		}
		if entries[i].service != "" && entries[j].service == "" {
			return false
		}
		return entries[i].service < entries[j].service
	})

	// For JSON output
	if opts.IsJSON() {
		type EnvVar struct {
			Name      string `json:"name"`
			Value     string `json:"value,omitempty"`
			Sensitive bool   `json:"sensitive"`
			Service   string `json:"service,omitempty"`
			Source    string `json:"source,omitempty"`
		}

		var vars []EnvVar
		for _, entry := range entries {
			ev := EnvVar{
				Name:      entry.nv.Key(),
				Sensitive: entry.nv.Sensitive(),
				Service:   entry.service,
				Source:    entry.nv.Source(),
			}
			// Only include value for non-sensitive variables in JSON
			if !entry.nv.Sensitive() {
				ev.Value = entry.nv.Value()
			}
			vars = append(vars, ev)
		}
		return PrintJSON(vars)
	}

	// Create and print the table
	printEnvTable(ctx, entries)

	return nil
}

func EnvDelete(ctx *Context, opts struct {
	Keys    []string `rest:"true" usage:"Environment variable keys to delete" required:"true"`
	Service string   `short:"S" long:"service" description:"Delete env var from specific service only (if not specified, deletes global env var)"`
	Force   bool     `short:"f" long:"force" description:"Skip confirmation prompt"`
	AppCentric
}) error {
	if len(opts.Keys) == 0 {
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
		if len(opts.Keys) == 1 {
			message = fmt.Sprintf("Delete environment variable '%s'?", opts.Keys[0])
		} else {
			message = fmt.Sprintf("Delete %d environment variables: %s?",
				len(opts.Keys), strings.Join(opts.Keys, ", "))
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
	for _, key := range opts.Keys {
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
func printEnvTable(ctx *Context, entries []envVarEntry) {
	// Create a gray style for sensitive values
	grayStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))

	// Build rows
	var rows []ui.Row
	for _, entry := range entries {
		var value string

		if entry.nv.Sensitive() {
			value = grayStyle.Render("••••••••••")
		} else {
			value = entry.nv.Value()
		}

		// Get source with backward compatibility
		source := entry.nv.Source()
		if source == "" {
			source = "config"
		}

		// Display service scope
		service := "(all)"
		if entry.service != "" {
			service = entry.service
		}

		rows = append(rows, ui.Row{entry.nv.Key(), value, service, source})
	}

	// Auto-size columns with reasonable maximums
	columns := ui.AutoSizeColumns(
		[]string{"NAME", "VALUE", "SERVICE", "SOURCE"},
		rows,
		ui.Columns().
			MaxWidth(0, 30). // NAME
			MaxWidth(1, 40). // VALUE
			MaxWidth(2, 15). // SERVICE
			MaxWidth(3, 12), // SOURCE
	)

	// Create and render the table
	table := ui.NewTable(
		ui.WithColumns(columns),
		ui.WithRows(rows),
	)

	ctx.Printf("%s\n", table.Render())
}
