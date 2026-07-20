package commands

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"miren.dev/mflags"
	appclient "miren.dev/runtime/api/app"
)

func writeAppToml(t *testing.T, dir, content string) {
	t.Helper()
	mirenDir := filepath.Join(dir, ".miren")
	if err := os.MkdirAll(mirenDir, 0755); err != nil {
		t.Fatalf("failed to create .miren dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(mirenDir, "app.toml"), []byte(content), 0644); err != nil {
		t.Fatalf("failed to write app.toml: %v", err)
	}
}

func TestInferAppName(t *testing.T) {
	tests := []struct {
		dir  string
		want string
	}{
		{"/home/user/my-app", "my-app"},
		{"/home/user/My App", "my-app"},
		{"/home/user/my_app", "my-app"},
		{"/home/user/MyApp", "myapp"},
		{"/home/user/HELLO", "hello"},
	}
	for _, tt := range tests {
		t.Run(tt.dir, func(t *testing.T) {
			got := inferAppName(tt.dir)
			if got != tt.want {
				t.Errorf("inferAppName(%q) = %q, want %q", tt.dir, got, tt.want)
			}
		})
	}
}

// chdir changes the working directory for the duration of the test and restores it on cleanup.
func chdir(t *testing.T, dir string) {
	t.Helper()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("failed to chdir to %s: %v", dir, err)
	}
	t.Cleanup(func() { os.Chdir(orig) })
}

func TestAppCentricValidate(t *testing.T) {
	t.Run("invalid TOML syntax returns parse error", func(t *testing.T) {
		dir := t.TempDir()
		writeAppToml(t, dir, `[[[`)

		a := AppCentric{Dir: dir}
		err := a.Validate(&GlobalFlags{})
		if err == nil {
			t.Fatal("expected error for invalid TOML syntax")
		}
		if strings.Contains(err.Error(), "app is required") {
			t.Errorf("expected parse error, got generic 'app is required': %v", err)
		}
		if !strings.Contains(err.Error(), "error loading") {
			t.Errorf("expected error to mention 'error loading', got: %v", err)
		}
	})

	t.Run("type mismatch returns decode error", func(t *testing.T) {
		dir := t.TempDir()
		// command is a string field but we give it an array
		writeAppToml(t, dir, `
name = "myapp"

[services.web]
command = ["foo", "bar"]
`)

		a := AppCentric{Dir: dir}
		err := a.Validate(&GlobalFlags{})
		if err == nil {
			t.Fatal("expected error for type mismatch")
		}
		if strings.Contains(err.Error(), "app is required") {
			t.Errorf("expected decode error, got generic 'app is required': %v", err)
		}
		if !strings.Contains(err.Error(), "error loading") {
			t.Errorf("expected error to mention 'error loading', got: %v", err)
		}
	})

	t.Run("valid TOML with name populates App", func(t *testing.T) {
		dir := t.TempDir()
		writeAppToml(t, dir, `name = "myapp"`)

		a := AppCentric{Dir: dir}
		err := a.Validate(&GlobalFlags{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if a.App != "myapp" {
			t.Errorf("expected App to be 'myapp', got %q", a.App)
		}
	})

	t.Run("no app.toml returns helpful error mentioning miren init", func(t *testing.T) {
		dir := t.TempDir()

		a := AppCentric{Dir: dir}
		err := a.Validate(&GlobalFlags{})
		if err == nil {
			t.Fatal("expected error when no app.toml exists")
		}
		if !strings.Contains(err.Error(), "miren init") {
			t.Errorf("expected error to mention 'miren init', got: %v", err)
		}
		if !strings.Contains(err.Error(), "-a") {
			t.Errorf("expected error to mention '-a' flag, got: %v", err)
		}
	})

	t.Run("app flag with invalid TOML still returns parse error", func(t *testing.T) {
		dir := t.TempDir()
		writeAppToml(t, dir, `[[[`)

		a := AppCentric{Dir: dir, App: "myapp"}
		err := a.Validate(&GlobalFlags{})
		if err == nil {
			t.Fatal("expected error for invalid TOML even with -a flag")
		}
		if strings.Contains(err.Error(), "app is required") {
			t.Errorf("expected parse error, got: %v", err)
		}
	})

	t.Run("app flag with no app.toml succeeds", func(t *testing.T) {
		dir := t.TempDir()

		a := AppCentric{Dir: dir, App: "myapp"}
		err := a.Validate(&GlobalFlags{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if a.App != "myapp" {
			t.Errorf("expected App to be 'myapp', got %q", a.App)
		}
	})

	// MIR-1435 guard: inside a sandbox, MIREN_APP is injected as a deprecated alias
	// of the sandbox's own app (MIREN_RUNTIME_APP). mflags populates a.App from the
	// MIREN_APP env tag, but Validate must ignore that injected value and resolve
	// the app from .miren/app.toml, preserving the MIR-1406 fix during the alias
	// window.
	t.Run("injected MIREN_APP alias is ignored inside a sandbox", func(t *testing.T) {
		dir := t.TempDir()
		writeAppToml(t, dir, `name = "realapp"`)
		t.Setenv(appclient.EnvRuntimeApp, "sandboxapp")
		t.Setenv("MIREN_APP", "sandboxapp")

		a := AppCentric{Dir: dir, App: "sandboxapp"}
		if err := a.Validate(&GlobalFlags{}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if a.App != "realapp" {
			t.Errorf("App = %q, want realapp — injected MIREN_APP alias should be ignored", a.App)
		}
	})

	t.Run("explicit -a differing from the sandbox app is preserved", func(t *testing.T) {
		dir := t.TempDir()
		writeAppToml(t, dir, `name = "realapp"`)
		t.Setenv(appclient.EnvRuntimeApp, "sandboxapp")
		t.Setenv("MIREN_APP", "sandboxapp")

		// mflags: an explicit --app beats the env, so a.App differs from MIREN_APP.
		a := AppCentric{Dir: dir, App: "otherapp"}
		if err := a.Validate(&GlobalFlags{}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if a.App != "otherapp" {
			t.Errorf("App = %q, want otherapp — an explicit target must not be cleared", a.App)
		}
	})

	t.Run("MIREN_APP as CLI input is preserved outside a sandbox", func(t *testing.T) {
		dir := t.TempDir()
		// No MIREN_RUNTIME_APP: this is the CI case, MIREN_APP is a real target.
		t.Setenv("MIREN_APP", "ciapp")

		a := AppCentric{Dir: dir, App: "ciapp"}
		if err := a.Validate(&GlobalFlags{}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if a.App != "ciapp" {
			t.Errorf("App = %q, want ciapp — MIREN_APP CLI input must survive outside a sandbox", a.App)
		}
	})

	t.Run("config in parent directory sets foundInParent", func(t *testing.T) {
		parent := t.TempDir()
		writeAppToml(t, parent, `name = "myapp"`)

		sub := filepath.Join(parent, "scripts")
		if err := os.MkdirAll(sub, 0755); err != nil {
			t.Fatalf("failed to create subdirectory: %v", err)
		}

		chdir(t, sub)

		a := AppCentric{Dir: "."}
		err := a.Validate(&GlobalFlags{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if a.App != "myapp" {
			t.Errorf("expected App to be 'myapp', got %q", a.App)
		}
		if !a.foundInParent {
			t.Error("expected foundInParent to be true")
		}
		if a.ResolvedDir() != parent {
			t.Errorf("expected ResolvedDir() = %q, got %q", parent, a.ResolvedDir())
		}
	})

	t.Run("config in current directory does not set foundInParent", func(t *testing.T) {
		dir := t.TempDir()
		writeAppToml(t, dir, `name = "myapp"`)

		chdir(t, dir)

		a := AppCentric{Dir: "."}
		err := a.Validate(&GlobalFlags{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if a.App != "myapp" {
			t.Errorf("expected App to be 'myapp', got %q", a.App)
		}
		if a.foundInParent {
			t.Error("expected foundInParent to be false")
		}
	})

	t.Run("explicit dir flag does not walk parents", func(t *testing.T) {
		parent := t.TempDir()
		writeAppToml(t, parent, `name = "myapp"`)

		sub := filepath.Join(parent, "scripts")
		if err := os.MkdirAll(sub, 0755); err != nil {
			t.Fatalf("failed to create subdirectory: %v", err)
		}

		// With explicit -d pointing at the subdirectory, we should NOT
		// find the parent config.
		a := AppCentric{Dir: sub}
		err := a.Validate(&GlobalFlags{})
		if err == nil {
			t.Fatal("expected error when no app.toml in explicit dir")
		}
		if !strings.Contains(err.Error(), "miren init") {
			t.Errorf("expected error to mention 'miren init', got: %v", err)
		}
	})
}

// TestAppCentric_MIREN_APP_EnvTag confirms that env:"MIREN_APP" on AppCentric.App
// actually flows through mflags' FromStruct path (which silently ignored the tag
// before the env-tag support landed in mflags MIR-986). It also verifies the
// CLI-beats-env precedence the rest of the system relies on.
func TestAppCentric_MIREN_APP_EnvTag(t *testing.T) {
	t.Run("env populates App when CLI flag absent", func(t *testing.T) {
		t.Setenv("MIREN_APP", "from-env")

		var a AppCentric
		fs := mflags.NewFlagSet("app")
		if err := fs.FromStruct(&a); err != nil {
			t.Fatalf("FromStruct: %v", err)
		}
		if err := fs.Parse(nil); err != nil {
			t.Fatalf("Parse: %v", err)
		}
		if a.App != "from-env" {
			t.Errorf("App = %q, want %q (env tag should have fired)", a.App, "from-env")
		}
	})

	t.Run("CLI flag beats env", func(t *testing.T) {
		t.Setenv("MIREN_APP", "from-env")

		var a AppCentric
		fs := mflags.NewFlagSet("app")
		if err := fs.FromStruct(&a); err != nil {
			t.Fatalf("FromStruct: %v", err)
		}
		if err := fs.Parse([]string{"--app=from-cli"}); err != nil {
			t.Fatalf("Parse: %v", err)
		}
		if a.App != "from-cli" {
			t.Errorf("App = %q, want from-cli (CLI should beat env)", a.App)
		}
	})
}

// TestCLIEnvTagsDoNotReadInjectedVars closes the MIR-1406 collision from the CLI
// side. It derives the MIREN_* vars the CLI reads from the actual mflags `env:`
// struct tags on AppCentric (which embeds ConfigCentric) — no hand-maintained
// list to drift out of sync — and asserts none of them names a var Miren injects
// into sandboxes. The deployment side guarantees the other half: every injected
// var stays under the reserved MIREN_RUNTIME_ prefix (see
// TestRuntimeEnvNamesDoNotCollideWithClientEnv).
//
// These are the app/cluster targeting flags whose env tags caused MIR-1406. Vars
// the CLI reads via os.Getenv elsewhere aren't reflected here, but they're safe
// as long as none of them lives under MIREN_RUNTIME_, which is reserved.
func TestCLIEnvTagsDoNotReadInjectedVars(t *testing.T) {
	injected := make(map[string]bool, len(appclient.RuntimeEnvNames))
	for _, n := range appclient.RuntimeEnvNames {
		injected[n] = true
	}

	tags := collectEnvTags(reflect.TypeOf(AppCentric{}))
	if len(tags) == 0 {
		t.Fatal("no env tags found — the reflection walk is not seeing the CLI flags")
	}

	for _, env := range tags {
		if injected[env] {
			t.Errorf("CLI flag reads env %q, which Miren injects into sandboxes — "+
				"running the CLI inside a sandbox would target the wrong app (MIR-1406)", env)
		}
	}
}

// collectEnvTags returns the `env:` struct tags on t and, recursively, on its
// embedded (anonymous) structs — mirroring how mflags composes flags.
func collectEnvTags(t reflect.Type) []string {
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil
	}

	var out []string
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if env := f.Tag.Get("env"); env != "" {
			out = append(out, env)
		}
		if f.Anonymous {
			out = append(out, collectEnvTags(f.Type)...)
		}
	}
	return out
}
