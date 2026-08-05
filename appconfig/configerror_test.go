package appconfig

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLevenshteinDistance(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"", "", 0},
		{"", "abc", 3},
		{"abc", "", 3},
		{"abc", "abc", 0},
		{"size", "size_gb", 3},
		{"comand", "command", 1},
		{"siz_gb", "size_gb", 1},
		{"port", "ports", 1},
	}
	for _, tt := range tests {
		t.Run(tt.a+"→"+tt.b, func(t *testing.T) {
			assert.Equal(t, tt.want, levenshteinDistance(tt.a, tt.b))
		})
	}
}

func TestSuggestField(t *testing.T) {
	diskFields := []string{"name", "provider", "mount_path", "read_only", "size_gb", "filesystem", "lease_timeout"}

	tests := []struct {
		unknown    string
		candidates []string
		want       string
	}{
		// Substring match (high confidence)
		{"size", diskFields, "size_gb"},
		{"mount", diskFields, "mount_path"},
		// Levenshtein match
		{"comand", []string{"command", "port", "image"}, "command"},
		{"naem", []string{"name", "provider"}, "name"},
		// No match
		{"zzzzz", []string{"name", "port"}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.unknown, func(t *testing.T) {
			assert.Equal(t, tt.want, suggestField(tt.unknown, tt.candidates))
		})
	}
}

func TestConfigErrorFormat(t *testing.T) {
	ce := &ConfigError{
		FilePath: ".miren/app.toml",
		Diagnostics: []Diagnostic{
			{Line: 5, Message: `unknown field "comand"`, Hint: `did you mean "command"?`},
		},
	}
	errStr := ce.Error()
	assert.Contains(t, errStr, ".miren/app.toml:5:")
	assert.Contains(t, errStr, `unknown field "comand"`)
	assert.Contains(t, errStr, `did you mean "command"?`)
}

func TestConfigErrorMultipleDiagnostics(t *testing.T) {
	ce := &ConfigError{
		FilePath: "app.toml",
		Diagnostics: []Diagnostic{
			{Line: 3, Message: `unknown field "a"`},
			{Line: 7, Message: `unknown field "b"`},
		},
	}
	errStr := ce.Error()
	assert.Contains(t, errStr, "app.toml:3:")
	assert.Contains(t, errStr, "app.toml:7:")
}

func TestEnrichDecodeError_UnknownField(t *testing.T) {
	config := `
name = "test-app"

[services.web]
comand = "server"
`
	_, err := Parse([]byte(config))
	require.Error(t, err)

	ce, ok := errors.AsType[*ConfigError](err)
	require.True(t, ok, "expected *ConfigError, got %T", err)
	assert.Equal(t, "<input>", ce.FilePath)
	require.Len(t, ce.Diagnostics, 1)
	assert.Contains(t, ce.Diagnostics[0].Message, "comand")
	assert.Contains(t, ce.Diagnostics[0].Hint, `"command"`)
	assert.Greater(t, ce.Diagnostics[0].Line, 0)
}

func TestEnrichDecodeError_SyntaxError(t *testing.T) {
	config := `name = "unterminated`
	_, err := Parse([]byte(config))
	require.Error(t, err)

	ce, ok := errors.AsType[*ConfigError](err)
	require.True(t, ok, "expected *ConfigError, got %T", err)
	assert.Greater(t, ce.Diagnostics[0].Line, 0)
	assert.Contains(t, ce.Diagnostics[0].Context, "unterminated")
}

func TestEnrichDecodeError_TypeMismatch(t *testing.T) {
	config := `name = 42`
	_, err := Parse([]byte(config))
	require.Error(t, err)

	ce, ok := errors.AsType[*ConfigError](err)
	require.True(t, ok, "expected *ConfigError, got %T", err)
	assert.Contains(t, ce.Error(), "cannot decode")
}

func TestEnrichValidationError_WithLineNumber(t *testing.T) {
	config := `
name = "test-app"

[services.web.concurrency]
mode = "invalid"
`
	_, err := Parse([]byte(config))
	require.Error(t, err)

	ce, ok := errors.AsType[*ConfigError](err)
	require.True(t, ok, "expected *ConfigError, got %T", err)
	assert.Contains(t, ce.Error(), `invalid concurrency mode "invalid"`)
	// Should resolve line number for the mode key
	require.Len(t, ce.Diagnostics, 1)
	assert.Equal(t, 5, ce.Diagnostics[0].Line)
}

func TestEnrichValidationError_AliasError(t *testing.T) {
	config := `
name = "test-app"

[aliases]
Console = "app exec"
`
	_, err := Parse([]byte(config))
	require.Error(t, err)

	ce, ok := errors.AsType[*ConfigError](err)
	require.True(t, ok, "expected *ConfigError, got %T", err)
	assert.Contains(t, ce.Error(), "each word must start with a lowercase letter")
	// Should have a line number for the alias
	assert.Greater(t, ce.Diagnostics[0].Line, 0)
}

func TestResolveKeyLine(t *testing.T) {
	data := []byte(`
name = "test"

[services.web.concurrency]
mode = "auto"
requests_per_instance = 10
`)
	assert.Equal(t, 5, resolveKeyLine(data, "services.web.concurrency.mode"))
	assert.Equal(t, 6, resolveKeyLine(data, "services.web.concurrency.requests_per_instance"))
	assert.Equal(t, 0, resolveKeyLine(data, "nonexistent.key"))
}

func TestResolveKeyLine_ParentChildTables(t *testing.T) {
	// Regression: when a parent table [services.web] appears before a child
	// table [services.web.concurrency], the AST walker must skip the
	// already-matched prefix components when comparing table header keys.
	data := []byte(`
name = "test"

[services.web]
port = 8080

[services.web.concurrency]
mode = "auto"
requests_per_instance = 10
`)
	assert.Equal(t, 8, resolveKeyLine(data, "services.web.concurrency.mode"))
	assert.Equal(t, 9, resolveKeyLine(data, "services.web.concurrency.requests_per_instance"))
	assert.Equal(t, 5, resolveKeyLine(data, "services.web.port"))
}

func TestResolveKeyLine_SiblingTables(t *testing.T) {
	// Regression: sibling tables like [services.api.concurrency] must not
	// match when looking for services.web.concurrency.mode — the skipped
	// prefix components must be verified against the expected path.
	data := []byte(`
name = "test"

[services.api.concurrency]
mode = "fixed"
num_instances = 1

[services.web.concurrency]
mode = "auto"
requests_per_instance = 10
`)
	// Should find the web service's mode, not the api service's
	assert.Equal(t, 9, resolveKeyLine(data, "services.web.concurrency.mode"))
	// Should find the api service's mode
	assert.Equal(t, 5, resolveKeyLine(data, "services.api.concurrency.mode"))
}

func TestKeyParentPath(t *testing.T) {
	tests := []struct {
		name string
		key  []string
		want string
	}{
		{"top-level", []string{"unknown"}, ""},
		{"service field", []string{"services", "web", "comand"}, "services.*"},
		{"service nested", []string{"services", "web", "concurrency", "mode"}, "services.*.concurrency"},
		{"disk field", []string{"services", "db", "disks", "size"}, "services.*.disks"},
		{"addon field", []string{"addons", "pg", "unknown"}, "addons.*"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// keyParentPath expects a toml.Key which is []string
			assert.Equal(t, tt.want, keyParentPath(tt.key))
		})
	}
}
