package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"miren.dev/mflags"
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

// TestAppCentric_NoEnvTagOnApp pins that AppCentric.App carries no env:"MIREN_APP"
// tag. mflags applies env tags in FromStruct, before Validate runs, so a tag here
// would make MIREN_APP indistinguishable from an explicit -a and silently outrank
// .miren/app.toml again (MIR-1402). MIREN_APP is read in Validate instead; see
// TestAppCentric_AppNamePrecedence.
func TestAppCentric_NoEnvTagOnApp(t *testing.T) {
	t.Setenv("MIREN_APP", "from-env")

	var a AppCentric
	fs := mflags.NewFlagSet("app")
	if err := fs.FromStruct(&a); err != nil {
		t.Fatalf("FromStruct: %v", err)
	}
	if err := fs.Parse(nil); err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if a.App != "" {
		t.Errorf("App = %q after parsing, want empty — MIREN_APP must not be wired up as an env tag; resolve it in Validate below the config file", a.App)
	}
}

// TestAppCentric_AppNamePrecedence covers the resolution order Validate implements:
// -a flag > .miren/app.toml > MIREN_APP.
func TestAppCentric_AppNamePrecedence(t *testing.T) {
	t.Run("env populates App when no app.toml", func(t *testing.T) {
		t.Setenv("MIREN_APP", "from-env")
		dir := t.TempDir()

		a := AppCentric{Dir: dir}
		if err := a.Validate(&GlobalFlags{}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if a.App != "from-env" {
			t.Errorf("App = %q, want from-env (env is the fallback when no config exists)", a.App)
		}
	})

	t.Run("app.toml beats env", func(t *testing.T) {
		t.Setenv("MIREN_APP", "from-env")
		dir := t.TempDir()
		writeAppToml(t, dir, `name = "myapp"`)

		a := AppCentric{Dir: dir}
		if err := a.Validate(&GlobalFlags{}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if a.App != "myapp" {
			t.Errorf("App = %q, want myapp (config file must outrank MIREN_APP)", a.App)
		}
	})

	t.Run("app flag beats both app.toml and env", func(t *testing.T) {
		t.Setenv("MIREN_APP", "from-env")
		dir := t.TempDir()
		writeAppToml(t, dir, `name = "myapp"`)

		a := AppCentric{Dir: dir, App: "from-cli"}
		if err := a.Validate(&GlobalFlags{}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if a.App != "from-cli" {
			t.Errorf("App = %q, want from-cli (explicit flag wins)", a.App)
		}
	})

	t.Run("app.toml found in parent beats env", func(t *testing.T) {
		t.Setenv("MIREN_APP", "from-env")
		parent := t.TempDir()
		writeAppToml(t, parent, `name = "myapp"`)

		sub := filepath.Join(parent, "scripts")
		if err := os.MkdirAll(sub, 0755); err != nil {
			t.Fatalf("failed to create subdirectory: %v", err)
		}
		chdir(t, sub)

		a := AppCentric{Dir: "."}
		if err := a.Validate(&GlobalFlags{}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if a.App != "myapp" {
			t.Errorf("App = %q, want myapp (parent-walk config must outrank MIREN_APP)", a.App)
		}
	})

	// The MIR-1402 repro, through the real command lifecycle: mflags parsing
	// followed by Validate, with MIREN_APP set the way an app sandbox sets it.
	// The direct-construction subtests above cannot catch a re-added env tag,
	// since they never run FromStruct.
	t.Run("full mflags lifecycle: app.toml beats sandbox MIREN_APP", func(t *testing.T) {
		t.Setenv("MIREN_APP", "sandbox-app")
		dir := t.TempDir()
		writeAppToml(t, dir, `name = "checkout-app"`)
		chdir(t, dir)

		var a AppCentric
		fs := mflags.NewFlagSet("app")
		if err := fs.FromStruct(&a); err != nil {
			t.Fatalf("FromStruct: %v", err)
		}
		if err := fs.Parse(nil); err != nil {
			t.Fatalf("Parse: %v", err)
		}
		if err := a.Validate(&GlobalFlags{}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if a.App != "checkout-app" {
			t.Errorf("App = %q, want checkout-app — deploying from inside a sandbox must honor the app.toml in the working directory", a.App)
		}
	})

	t.Run("empty env is ignored", func(t *testing.T) {
		t.Setenv("MIREN_APP", "")
		dir := t.TempDir()

		a := AppCentric{Dir: dir}
		err := a.Validate(&GlobalFlags{})
		if err == nil {
			t.Fatal("expected error when no app name is resolvable")
		}
		if !strings.Contains(err.Error(), "miren init") {
			t.Errorf("expected error to mention 'miren init', got: %v", err)
		}
	})
}
