package commands

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"miren.dev/runtime/api/app/app_v1alpha"
	"miren.dev/runtime/api/deployment/deployment_v1alpha"
	"miren.dev/runtime/pkg/secret"
	"miren.dev/runtime/pkg/theme"
	"miren.dev/runtime/pkg/ui"
)

// envRedacted is the fixed-width placeholder shown for redacted env var values.
// It deliberately reveals neither the length nor any character of the value.
const envRedacted = "••••••••••"

// urlUserinfoRe matches the userinfo (user:pass@) component of a URL-shaped
// value: a scheme, "://", everything up to the first "@", then "@". Requiring a
// scheme avoids matching bare email addresses in non-URL values.
var urlUserinfoRe = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.-]*://)[^/?#@]+@`)

// maskURLUserinfo redacts embedded credentials in URL-shaped values so secrets
// like the password in a DATABASE_URL don't leak even when the key name gave no
// hint and nothing marked the var sensitive. Non-URL values pass through.
func maskURLUserinfo(value string) string {
	return urlUserinfoRe.ReplaceAllString(value, "${1}"+envRedacted+"@")
}

// maskEnvValue is the single source of truth for how an env-var value is
// displayed anywhere in the CLI (env list/get, deploy analysis, and any future
// surface). Sensitivity is decided by EVIDENCE, in tiers, and this function
// honors that evidence — it does not guess:
//
//   - Declared: the `sensitive` flag. Set by the user (-s / app.toml) or by the
//     producer that minted the credential (addons stamp their generated
//     DATABASE_URL/passwords). This is the authority; when set, we fully redact.
//   - Structural: even when the flag is unset, URL userinfo (user:pass@) is
//     redacted, because that is by definition a credential slot — provable
//     syntax, not a guess. This is the last-line invariant against unmarked
//     leaks like a hand-set DATABASE_URL.
//
// Deliberately absent: key-name matching. A name like "SECRET" is a *guess*,
// and a guess that silently fails at display time is exactly how DATABASE_URL
// leaked (MIR-1356). Name heuristics belong only at set-time as a *suggestion*
// the user can override (see looksLikeSensitive), never here. If a new leak
// shape appears (e.g. a JDBC "Password=..." string), the fix is to teach the
// producer to set `sensitive`, NOT to grow this function into a pile of
// format matchers. Hold this surface small.
//
// The unmask toggle (behind an explicit --unmask flag) means "give me the
// literal bytes" — no masking, no escaping — for copying or scripting.
// Without it, the value is rendered display-safe: control/ANSI sequences are
// escaped so a malicious value can't corrupt terminal or CI output.
// displayEnvValue renders what `env list`/`env get` shows for one variable.
//
// A backend-sourced variable has no local value to mask: the bytes never
// travelled here, only the reference did. Showing the reference — backend,
// path, and the version this config pinned — is both safe and the only useful
// thing to show, so --unmask has nothing extra to reveal for one.
func displayEnvValue(value, backend string, sensitive, unmask bool) string {
	if backend != "" {
		return "\u2192 " + backend + ":" + value
	}
	return maskEnvValue(value, sensitive, unmask)
}

func maskEnvValue(value string, sensitive, unmask bool) string {
	if unmask {
		return value
	}
	if sensitive {
		return envRedacted
	}
	return escapeForDisplay(maskURLUserinfo(value))
}

// escapeForDisplay replaces ASCII control characters (including ESC used for
// ANSI sequences, newlines, and carriage returns) with their Go-quoted form so
// a malicious env value can't break out of a log line or spoof CLI/CI output.
func escapeForDisplay(s string) string {
	if s == "" {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\t':
			b.WriteString(`\t`)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, `\x%02x`, r)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// EnvVarSpec represents a parsed environment variable specification
type EnvVarSpec struct {
	Key       string
	Value     string
	Sensitive bool
	FromFile  bool   // true if value was read from a file
	FromFile_ string // original filename if FromFile is true
}

// ParseEnvVarSpecs parses environment variable specifications from -e and -s flags.
// Each spec can be: KEY=VALUE, KEY=@file, or KEY (to prompt interactively).
func ParseEnvVarSpecs(envSpecs, sensitiveSpecs []string) ([]EnvVarSpec, error) {
	var specs []EnvVarSpec

	// Process regular env vars
	for _, spec := range envSpecs {
		parsed, err := parseEnvVarSpec(spec, false)
		if err != nil {
			return nil, err
		}
		specs = append(specs, parsed)
	}

	// Process sensitive env vars
	for _, spec := range sensitiveSpecs {
		parsed, err := parseEnvVarSpec(spec, true)
		if err != nil {
			return nil, err
		}
		specs = append(specs, parsed)
	}

	return specs, nil
}

// parseEnvVarSpec parses a single env var spec (KEY, KEY=VALUE, or KEY=@file)
func parseEnvVarSpec(spec string, sensitive bool) (EnvVarSpec, error) {
	parts := strings.SplitN(spec, "=", 2)
	key := parts[0]

	if key == "" {
		return EnvVarSpec{}, fmt.Errorf("invalid environment variable: key cannot be empty")
	}

	result := EnvVarSpec{
		Key:       key,
		Sensitive: sensitive,
	}

	if len(parts) == 1 {
		// No value provided - will need to prompt
		var label string
		if sensitive {
			label = fmt.Sprintf("Enter value for sensitive variable '%s'", key)
		} else {
			label = fmt.Sprintf("Enter value for variable '%s'", key)
		}

		promptedValue, err := ui.PromptForInput(
			ui.WithLabel(label),
			ui.WithSensitive(sensitive),
		)
		if err != nil {
			return EnvVarSpec{}, fmt.Errorf("failed to read value for %s: %w", key, err)
		}
		result.Value = promptedValue
	} else {
		value := parts[1]

		if strings.HasPrefix(value, "@") {
			filename := value[1:]
			data, err := os.ReadFile(filename)
			if err != nil {
				if os.IsNotExist(err) {
					return EnvVarSpec{}, fmt.Errorf("env var references file %s which does not exist", filename)
				}
				return EnvVarSpec{}, fmt.Errorf("failed to read env var from file %s: %w", filename, err)
			}
			result.Value = strings.TrimRight(string(data), "\r\n")
			result.FromFile = true
			result.FromFile_ = filename
		} else {
			result.Value = value
		}
	}

	return result, nil
}

func EnvSet(ctx *Context, opts struct {
	AppCentric
	Service   string   `short:"S" long:"service" description:"Set env var for specific service only (if not specified, sets for all services)"`
	Env       []string `short:"e" long:"env" description:"Set environment variables (use KEY to prompt, KEY=VALUE to set directly, KEY=@file to read from file)"`
	Sensitive []string `short:"s" long:"sensitive" description:"Set sensitive environment variables (use KEY to prompt with masking, KEY=VALUE to set directly, KEY=@file to read from file)"`
	Backend   string   `short:"b" long:"backend" description:"Source the value from a secret backend instead of setting it literally (default: cluster, with --ref)"`
	Ref       string   `long:"ref" description:"Backend-relative reference to the secret, e.g. payments/stripe-key"`
}) error {
	if opts.Ref != "" || opts.Backend != "" {
		return envSetReference(ctx, opts.App, opts.Service, opts.Env, opts.Sensitive, opts.Backend, opts.Ref)
	}

	if len(opts.Env) == 0 && len(opts.Sensitive) == 0 {
		return fmt.Errorf("no environment variables specified")
	}

	// Parse all env var specs
	specs, err := ParseEnvVarSpecs(opts.Env, opts.Sensitive)
	if err != nil {
		return err
	}

	depCl, err := ctx.RPCClient("dev.miren.runtime/deployment")
	if err != nil {
		return fmt.Errorf("failed to connect to deployment service: %w", err)
	}
	depClient := deployment_v1alpha.NewDeploymentClient(depCl)

	var vars []*deployment_v1alpha.EnvironmentVariable

	for _, spec := range specs {
		if spec.FromFile {
			ctx.Printf("setting %s from file %s...\n", spec.Key, spec.FromFile_)
		} else if spec.Sensitive {
			ctx.Printf("setting %s (sensitive)...\n", spec.Key)
		} else {
			ctx.Printf("setting %s...\n", spec.Key)
		}

		ev := &deployment_v1alpha.EnvironmentVariable{}
		ev.SetKey(spec.Key)
		ev.SetValue(spec.Value)
		ev.SetSensitive(spec.Sensitive)
		vars = append(vars, ev)
	}

	return applyEnvVars(ctx, depClient, opts.App, opts.Service, vars)
}

// envSetReference points a variable at a secret backend rather than giving it a
// literal value. The value itself never travels here: only the reference does,
// and the server pins it to a concrete version as it mints the new config.
func envSetReference(ctx *Context, app, service string, env, sensitive []string, backend, ref string) error {
	keys := append(append([]string{}, env...), sensitive...)

	switch {
	case ref == "":
		return fmt.Errorf("--backend needs --ref naming the secret, e.g. --ref payments/stripe-key")
	case len(keys) != 1:
		return fmt.Errorf("--ref sets exactly one variable; pass a single -e KEY")
	case strings.Contains(keys[0], "="):
		// A literal alongside a reference is a contradiction, and guessing which
		// one the user meant is how the wrong thing ends up deployed.
		return fmt.Errorf("use -e KEY with --ref, not KEY=VALUE: the value comes from the backend")
	}

	if backend == "" {
		backend = secret.ClusterBackendName
	}

	depCl, err := ctx.RPCClient("dev.miren.runtime/deployment")
	if err != nil {
		return fmt.Errorf("failed to connect to deployment service: %w", err)
	}
	depClient := deployment_v1alpha.NewDeploymentClient(depCl)

	ctx.Printf("pointing %s at %s:%s...\n", keys[0], backend, ref)

	ev := &deployment_v1alpha.EnvironmentVariable{}
	ev.SetKey(keys[0])
	ev.SetValue(ref)
	ev.SetBackend(backend)
	ev.SetSensitive(true)

	return applyEnvVars(ctx, depClient, app, service, []*deployment_v1alpha.EnvironmentVariable{ev})
}

// applyEnvVars pushes a set of variables and waits for the version they mint to
// come up healthy, shared by the literal and reference paths so both report the
// same way.
func applyEnvVars(ctx *Context, depClient *deployment_v1alpha.DeploymentClient,
	app, service string, vars []*deployment_v1alpha.EnvironmentVariable) error {

	res, err := depClient.SetEnvVars(ctx, app, ctx.ClusterName, vars, service)
	if err != nil {
		return err
	}

	if res.HasError() && res.Error() != "" {
		if res.HasLockInfo() && res.LockInfo() != nil {
			displayLockInfo(ctx, "env set", res.LockInfo())
		} else {
			ctx.Printf("\nenv set failed: %s\n", res.Error())
		}
		return fmt.Errorf("env set failed")
	}

	versionDisplay := ui.DisplayShortID(res.Deployment().AppVersionShortId(), res.VersionId())
	ctx.Printf("Setting env vars on %s — new version: %s\n", app, versionDisplay)

	if err := awaitHealthy(ctx, app, res.VersionId(), versionDisplay); err != nil {
		return err
	}

	if res.HasAccessInfo() && res.AccessInfo() != nil {
		displayDeployVersionAccessInfo(ctx, app, res.AccessInfo())
	}

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

	ctx.Printf("%s\n", displayEnvValue(found.Value(), found.Backend(), found.Sensitive(), opts.Unmask))
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
			Name        string `json:"name"`
			Value       string `json:"value,omitempty"`
			Sensitive   bool   `json:"sensitive"`
			Service     string `json:"service,omitempty"`
			Source      string `json:"source,omitempty"`
			Required    bool   `json:"required,omitempty"`
			Description string `json:"description,omitempty"`
		}

		var vars []EnvVar
		for _, entry := range entries {
			vars = append(vars, EnvVar{
				Name:        entry.nv.Key(),
				Value:       displayEnvValue(entry.nv.Value(), entry.nv.Backend(), entry.nv.Sensitive(), false),
				Sensitive:   entry.nv.Sensitive(),
				Service:     entry.service,
				Source:      entry.nv.Source(),
				Required:    entry.nv.Required(),
				Description: entry.nv.Description(),
			})
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

	depCl, err := ctx.RPCClient("dev.miren.runtime/deployment")
	if err != nil {
		return fmt.Errorf("failed to connect to deployment service: %w", err)
	}
	depClient := deployment_v1alpha.NewDeploymentClient(depCl)

	for _, key := range opts.Keys {
		ctx.Printf("deleting %s...\n", key)
	}

	res, err := depClient.DeleteEnvVars(ctx, opts.App, ctx.ClusterName, opts.Keys, opts.Service)
	if err != nil {
		return err
	}

	if res.HasError() && res.Error() != "" {
		if res.HasLockInfo() && res.LockInfo() != nil {
			displayLockInfo(ctx, "env delete", res.LockInfo())
		} else {
			ctx.Printf("\nenv delete failed: %s\n", res.Error())
		}
		return fmt.Errorf("env delete failed")
	}

	versionDisplay := ui.DisplayShortID(res.Deployment().AppVersionShortId(), res.VersionId())
	ctx.Printf("Deleting env vars from %s — new version: %s\n", opts.App, versionDisplay)

	// Warn about config vars that will reappear on next deploy
	if res.HasDeletedSources() {
		var configVarsDeleted []string
		deletedSources := res.DeletedSources()
		for i, source := range deletedSources {
			if source == "config" && i < len(opts.Keys) {
				configVarsDeleted = append(configVarsDeleted, opts.Keys[i])
			}
		}

		if len(configVarsDeleted) > 0 {
			if len(configVarsDeleted) == 1 {
				ctx.Printf("\nWarning: %s was defined in app.toml and will reappear on next deploy.\n", configVarsDeleted[0])
				ctx.Printf("To permanently remove it, delete it from .miren/app.toml.\n")
			} else {
				ctx.Printf("\nWarning: %s were defined in app.toml and will reappear on next deploy.\n", strings.Join(configVarsDeleted, ", "))
				ctx.Printf("To permanently remove them, delete them from .miren/app.toml.\n")
			}
		}
	}

	if err := awaitHealthy(ctx, opts.App, res.VersionId(), versionDisplay); err != nil {
		return err
	}

	if res.HasAccessInfo() && res.AccessInfo() != nil {
		displayDeployVersionAccessInfo(ctx, opts.App, res.AccessInfo())
	}

	return nil
}

// printEnvTable prints a formatted table of environment variables. Values are
// always masked; there is deliberately no bulk-reveal flag — revealing a value
// is a single-secret, explicit operation via `miren env get <key> --unmask`.
func printEnvTable(ctx *Context, entries []envVarEntry) {
	// Create a gray style for masked values
	grayStyle := lipgloss.NewStyle().Foreground(theme.Muted)

	// Build rows
	var rows []ui.Row
	for _, entry := range entries {
		value := displayEnvValue(entry.nv.Value(), entry.nv.Backend(), entry.nv.Sensitive(), false)
		// Gray out values we masked so it's clear the redaction is intentional.
		if value != entry.nv.Value() {
			value = grayStyle.Render(value)
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

		description := entry.nv.Description()

		rows = append(rows, ui.Row{entry.nv.Key(), value, service, source, description})
	}

	// Auto-size columns with reasonable maximums
	columns := ui.AutoSizeColumns(
		[]string{"NAME", "VALUE", "SERVICE", "SOURCE", "DESCRIPTION"},
		rows,
		ui.Columns().
			MaxWidth(0, 30). // NAME
			MaxWidth(1, 40). // VALUE
			MaxWidth(2, 15). // SERVICE
			MaxWidth(3, 12). // SOURCE
			MaxWidth(4, 40), // DESCRIPTION
	)

	// Create and render the table
	table := ui.NewTable(
		ui.WithColumns(columns),
		ui.WithRows(rows),
	)

	ctx.Printf("%s\n", table.Render())
}
