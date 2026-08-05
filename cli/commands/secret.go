package commands

import (
	"fmt"
	"os"
	"strings"
	"time"

	"miren.dev/runtime/api/secret/secret_v1alpha"
	"miren.dev/runtime/pkg/secret"
	"miren.dev/runtime/pkg/ui"
)

// secretsClient connects to the cluster's secret store. Values are stored and
// resolved server-side, where the keyring lives, so the CLI never holds key
// material.
func secretsClient(ctx *Context) (*secret_v1alpha.SecretsClient, error) {
	cl, err := ctx.RPCClient("dev.miren.runtime/secrets")
	if err != nil {
		return nil, fmt.Errorf("failed to connect to secrets service: %w", err)
	}
	return secret_v1alpha.NewSecretsClient(cl), nil
}

func SecretSet(ctx *Context, opts struct {
	ConfigCentric
	Path    string `position:"0" usage:"Secret path, e.g. payments/stripe-key" required:"true"`
	Backend string `short:"b" long:"backend" description:"Backend instance to store into (default: cluster)"`
	Value   string `long:"value" description:"Secret value, or @file to read one from a file. Prompts with masking when omitted, which is safer — a literal here is recorded in shell history. A trailing newline is trimmed"`
}) error {
	value, err := readSecretValue(opts.Path, opts.Value)
	if err != nil {
		return err
	}

	client, err := secretsClient(ctx)
	if err != nil {
		return err
	}

	res, err := client.Set(ctx, opts.Backend, opts.Path, value)
	if err != nil {
		return err
	}

	backend := displayBackend(opts.Backend)
	ref := secret.FormatRef(opts.Path, res.Version())

	if res.Unchanged() {
		// Say so rather than implying a rotation happened: nothing that pins
		// this secret needs to move.
		ctx.Printf("%s is already stored at that value (%s, enabled)\n", ref, backend)
		return nil
	}

	ctx.Printf("Stored %s  (%s, enabled)\n", ref, backend)
	return nil
}

func SecretList(ctx *Context, opts struct {
	ConfigCentric
	FormatOptions
	Backend string `short:"b" long:"backend" description:"Backend instance to list (default: cluster)"`
}) error {
	client, err := secretsClient(ctx)
	if err != nil {
		return err
	}

	res, err := client.List(ctx, opts.Backend)
	if err != nil {
		return err
	}
	secrets := res.Secrets()

	if opts.IsJSON() {
		return PrintJSON(secretsToJSON(secrets))
	}

	if len(secrets) == 0 {
		ctx.Printf("No secrets found\n")
		return nil
	}

	headers := []string{"PATH", "BACKEND", "CURRENT", "VERSIONS"}
	var rows []ui.Row
	for _, s := range secrets {
		rows = append(rows, ui.Row{
			s.Path(),
			s.Backend(),
			versionHandle(s.CurrentVersion()),
			summarizeVersions(s.Versions()),
		})
	}

	columns := ui.AutoSizeColumns(headers, rows, ui.Columns().NoTruncate(0))
	table := ui.NewTable(ui.WithColumns(columns), ui.WithRows(rows))
	ctx.Printf("%s\n", table.Render())
	return nil
}

func SecretVersions(ctx *Context, opts struct {
	ConfigCentric
	FormatOptions
	Path    string `position:"0" usage:"Secret path, e.g. payments/stripe-key" required:"true"`
	Backend string `short:"b" long:"backend" description:"Backend instance to read (default: cluster)"`
}) error {
	client, err := secretsClient(ctx)
	if err != nil {
		return err
	}

	res, err := client.ListVersions(ctx, opts.Backend, opts.Path)
	if err != nil {
		return err
	}
	info := res.Secret()

	if opts.IsJSON() {
		return PrintJSON(secretToJSON(info))
	}

	versions := info.Versions()
	if len(versions) == 0 {
		ctx.Printf("%s has no versions\n", info.Path())
		return nil
	}

	headers := []string{"VERSION", "STATE", "CURRENT", "CREATED"}
	var rows []ui.Row
	for _, v := range versions {
		current := "-"
		if v.Current() {
			current = "✓"
		}
		rows = append(rows, ui.Row{
			versionHandle(v.Version()),
			v.State(),
			current,
			humanFriendlyTimestamp(time.UnixMilli(v.CreatedAt())),
		})
	}

	columns := ui.AutoSizeColumns(headers, rows, ui.Columns())
	table := ui.NewTable(ui.WithColumns(columns), ui.WithRows(rows))
	ctx.Printf("%s\n", table.Render())
	return nil
}

func SecretDisable(ctx *Context, opts struct {
	ConfigCentric
	Ref     string `position:"0" usage:"Reference naming a version, e.g. payments/stripe-key@x1A" required:"true"`
	Backend string `short:"b" long:"backend" description:"Backend instance holding the secret (default: cluster)"`
}) error {
	return setSecretState(ctx, opts.Backend, opts.Ref, secret.StateDisabled)
}

func SecretEnable(ctx *Context, opts struct {
	ConfigCentric
	Ref     string `position:"0" usage:"Reference naming a version, e.g. payments/stripe-key@x1A" required:"true"`
	Backend string `short:"b" long:"backend" description:"Backend instance holding the secret (default: cluster)"`
}) error {
	return setSecretState(ctx, opts.Backend, opts.Ref, secret.StateEnabled)
}

func SecretDestroy(ctx *Context, opts struct {
	ConfigCentric
	Ref     string `position:"0" usage:"Reference naming a version, e.g. payments/stripe-key@x1A" required:"true"`
	Backend string `short:"b" long:"backend" description:"Backend instance holding the secret (default: cluster)"`
	Force   bool   `short:"f" long:"force" description:"Skip the confirmation prompt"`
}) error {
	if !opts.Force {
		// Destroying drops the payload, so unlike disabling it cannot be undone
		// and anything still pinned to the version can never resolve again.
		confirmed, err := ui.Confirm(
			ui.WithMessage(fmt.Sprintf(
				"Destroy %s? The value is deleted and anything pinned to it can never resolve again. Continue?", opts.Ref)),
			ui.WithDefault(false),
		)
		if err != nil {
			return err
		}
		if !confirmed {
			ctx.Printf("Aborted\n")
			return nil
		}
	}

	return setSecretState(ctx, opts.Backend, opts.Ref, secret.StateDestroyed)
}

func setSecretState(ctx *Context, backend, ref string, state secret.VersionState) error {
	if !secret.IsPinned(ref) {
		return fmt.Errorf("%s does not name a version; use %s@<version> (see: miren secret versions %s)", ref, ref, ref)
	}

	client, err := secretsClient(ctx)
	if err != nil {
		return err
	}

	if _, err := client.SetState(ctx, backend, ref, string(state)); err != nil {
		return err
	}

	ctx.Printf("%s is now %s\n", ref, state)
	return nil
}

// readSecretValue resolves the value to store: a literal, a file, or a masked
// prompt. The value is never echoed and never appears in the output.
func readSecretValue(path, spec string) ([]byte, error) {
	if spec == "" {
		prompted, err := ui.PromptForInput(
			ui.WithLabel(fmt.Sprintf("Enter value for %s", path)),
			ui.WithSensitive(true),
		)
		if err != nil {
			return nil, fmt.Errorf("failed to read value for %s: %w", path, err)
		}
		if prompted == "" {
			return nil, fmt.Errorf("no value entered for %s", path)
		}
		return []byte(prompted), nil
	}

	if filename, ok := strings.CutPrefix(spec, "@"); ok {
		data, err := os.ReadFile(filename)
		if err != nil {
			if os.IsNotExist(err) {
				return nil, fmt.Errorf("secret value references file %s which does not exist", filename)
			}
			return nil, fmt.Errorf("failed to read secret value from file %s: %w", filename, err)
		}
		// Trim the trailing newline an editor leaves behind; a credential with
		// one appended is the most common way a copied token stops working.
		return []byte(strings.TrimRight(string(data), "\r\n")), nil
	}

	return []byte(spec), nil
}

// displayBackend names the backend a command acted on, filling in the default.
func displayBackend(name string) string {
	if name == "" {
		return secret.ClusterBackendName
	}
	return name
}

// versionHandle renders a version the way it is written in a reference, so what
// an operator reads is what they can paste back.
func versionHandle(version string) string {
	if version == "" {
		return "-"
	}
	return "@" + version
}

// summarizeVersions renders a secret's versions inline, marking anything that
// is no longer resolvable so a revoked version is visible at a glance.
func summarizeVersions(versions []*secret_v1alpha.SecretVersionInfo) string {
	if len(versions) == 0 {
		return "-"
	}

	parts := make([]string, 0, len(versions))
	for _, v := range versions {
		part := versionHandle(v.Version())
		if v.State() != string(secret.StateEnabled) {
			part += "(" + v.State() + ")"
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, " ")
}

type secretVersionJSON struct {
	Version   string `json:"version"`
	State     string `json:"state"`
	Current   bool   `json:"current"`
	CreatedAt string `json:"created_at"`
}

type secretJSON struct {
	Path           string              `json:"path"`
	Backend        string              `json:"backend"`
	CurrentVersion string              `json:"current_version"`
	Versions       []secretVersionJSON `json:"versions"`
}

func secretToJSON(info *secret_v1alpha.SecretInfo) secretJSON {
	out := secretJSON{
		Path:           info.Path(),
		Backend:        info.Backend(),
		CurrentVersion: info.CurrentVersion(),
		Versions:       []secretVersionJSON{},
	}

	for _, v := range info.Versions() {
		out.Versions = append(out.Versions, secretVersionJSON{
			Version:   v.Version(),
			State:     v.State(),
			Current:   v.Current(),
			CreatedAt: time.UnixMilli(v.CreatedAt()).UTC().Format(time.RFC3339),
		})
	}

	return out
}

func secretsToJSON(secrets []*secret_v1alpha.SecretInfo) []secretJSON {
	out := make([]secretJSON, 0, len(secrets))
	for _, s := range secrets {
		out = append(out, secretToJSON(s))
	}
	return out
}
