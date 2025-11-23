package commands

import (
	"fmt"
	"os"
	"slices"
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

	res, err := ac.GetConfiguration(ctx, opts.App)
	if err != nil {
		return err
	}

	cfg := res.Configuration()

	var changes bool

	// Determine if we're setting global or per-service env vars
	isServiceSpecific := opts.Service != ""

	// Create a map to track existing env vars for efficient lookup
	envMap := make(map[string]*app_v1alpha.NamedValue)
	var envvars []*app_v1alpha.NamedValue

	if isServiceSpecific {
		// Validate that the service exists by checking the actual deployed services
		// This includes both statically defined and dynamically detected services (like "web" from Procfile)
		if cfg.HasServices() || cfg.HasCommands() {
			serviceExists := false

			// Check services list
			for _, svc := range cfg.Services() {
				if svc.Service() == opts.Service {
					serviceExists = true
					break
				}
			}

			// Check commands list (services with commands are valid services)
			if !serviceExists {
				for _, cmd := range cfg.Commands() {
					if cmd.Service() == opts.Service {
						serviceExists = true
						break
					}
				}
			}

			if !serviceExists {
				// Build a helpful error message with available services
				var availableServices []string
				seenServices := make(map[string]bool)

				for _, svc := range cfg.Services() {
					if !seenServices[svc.Service()] {
						availableServices = append(availableServices, svc.Service())
						seenServices[svc.Service()] = true
					}
				}
				for _, cmd := range cfg.Commands() {
					if !seenServices[cmd.Service()] {
						availableServices = append(availableServices, cmd.Service())
						seenServices[cmd.Service()] = true
					}
				}

				if len(availableServices) > 0 {
					return fmt.Errorf("service %q not found. Available services: %s", opts.Service, strings.Join(availableServices, ", "))
				} else {
					return fmt.Errorf("service %q not found (no services detected in app)", opts.Service)
				}
			}
		}

		// Find the service and get its existing env vars
		for _, svc := range cfg.Services() {
			if svc.Service() == opts.Service {
				if svc.HasServiceEnv() {
					for _, ev := range svc.ServiceEnv() {
						envMap[ev.Key()] = ev
						envvars = append(envvars, ev)
					}
				}
				break
			}
		}
	} else {
		// Global env vars
		if cfg.HasEnvVars() {
			// Build map from existing env vars
			for _, ev := range cfg.EnvVars() {
				envMap[ev.Key()] = ev
				envvars = append(envvars, ev)
			}
		}
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

		// Validate that the key is not empty
		if key == "" {
			return fmt.Errorf("invalid environment variable: key cannot be empty")
		}

		if len(parts) == 1 {
			// No value provided, prompt for it
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
			// Value was provided
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

		// Check if this key already exists
		existingVar, exists := envMap[key]
		isUpdate := exists

		// Only mark as changed if value or sensitivity actually changed
		if !exists || existingVar.Value() != value || existingVar.Sensitive() != ev.sensitive {
			changes = true

			// Log the action
			action := "setting"
			if isUpdate {
				action = "updating"
			}

			if wasFile {
				ctx.Printf("%s %s from file %s...\n", action, key, parts[1][1:])
			} else if wasPrompt {
				if ev.sensitive {
					ctx.Printf("%s %s (sensitive, from prompt)...\n", action, key)
				} else {
					ctx.Printf("%s %s (from prompt)...\n", action, key)
				}
			} else {
				if ev.sensitive {
					ctx.Printf("%s %s (sensitive)...\n", action, key)
				} else {
					ctx.Printf("%s %s...\n", action, key)
				}
			}

			// Create or update the variable
			var nv app_v1alpha.NamedValue
			nv.SetKey(key)
			nv.SetValue(value)
			nv.SetSensitive(ev.sensitive)

			if exists {
				// Update existing entry in place
				for i, v := range envvars {
					if v.Key() == key {
						envvars[i] = &nv
						break
					}
				}
			} else {
				// Add new entry
				envvars = append(envvars, &nv)
			}

			// Update the map for future lookups
			envMap[key] = &nv
		}
	}

	if !changes {
		ctx.Printf("no changes to configuration\n")
		return nil
	}

	// Update configuration based on whether it's global or per-service
	if isServiceSpecific {
		// Update the service's env vars
		services := cfg.Services()
		found := false
		for i, svc := range services {
			if svc.Service() == opts.Service {
				svc.SetServiceEnv(envvars)
				services[i] = svc
				found = true
				break
			}
		}
		if !found {
			newSvc := &app_v1alpha.ServiceConfig{}
			newSvc.SetService(opts.Service)
			newSvc.SetServiceEnv(envvars)
			services = append(services, newSvc)
		}
		cfg.SetServices(services)
	} else {
		// Update global env vars
		cfg.SetEnvVars(envvars)
	}

	setres, err := ac.SetConfiguration(ctx, opts.App, cfg)
	if err != nil {
		return err
	}

	ctx.Printf("new version id: %s\n", setres.VersionId())

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
	Force bool `short:"f" long:"force" description:"Skip confirmation prompt"`
	Args  struct {
		Keys []string `positional-arg-name:"KEY" description:"Environment variable key to delete" required:"1"`
	} `positional-args:"yes"`
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
		ctx.Printf("No environment variables to delete\n")
		return nil
	}

	envvars := cfg.EnvVars()

	// First, verify ALL variables exist before we delete anything
	var toDelete []string

	for _, key := range opts.Args.Keys {
		found := false
		// Check if the key exists
		for _, nv := range envvars {
			if nv.Key() == key {
				found = true
				toDelete = append(toDelete, key)
				break
			}
		}
		if !found {
			// Bail immediately if any variable is not found
			return fmt.Errorf("environment variable '%s' not found", key)
		}
	}

	// Ask for confirmation unless --force is used
	if !opts.Force {
		var message string
		if len(toDelete) == 1 {
			message = fmt.Sprintf("Delete environment variable '%s'?", toDelete[0])
		} else {
			message = fmt.Sprintf("Delete %d environment variables: %s?",
				len(toDelete), strings.Join(toDelete, ", "))
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

	// Now perform the actual deletion
	for _, key := range toDelete {
		envvars = slices.DeleteFunc(envvars, func(nv *app_v1alpha.NamedValue) bool {
			if nv.Key() == key {
				ctx.Printf("deleting %s...\n", key)
				return true
			}
			return false
		})
	}

	cfg.SetEnvVars(envvars)

	setres, err := ac.SetConfiguration(ctx, opts.App, cfg)
	if err != nil {
		return err
	}

	ctx.Printf("new version id: %s\n", setres.VersionId())

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
