package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/format"
	"log"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"gopkg.in/yaml.v3"
)

var (
	schemaPath = flag.String("schema", "", "Path to the schema YAML file")
	outputDir  = flag.String("output", "", "Output directory for generated files")
)

// Schema represents the root of the configuration schema
type Schema struct {
	Package string             `yaml:"package"`
	Imports []string           `yaml:"imports"`
	Configs map[string]*Config `yaml:"configs"`
}

// Config represents a configuration struct
type Config struct {
	Description string            `yaml:"description"`
	Fields      map[string]*Field `yaml:"fields"`
}

// Field represents a configuration field
type Field struct {
	Type        string         `yaml:"type"`
	Default     any            `yaml:"default"`
	CLI         *CLIConfig     `yaml:"cli"`
	Env         string         `yaml:"env"`
	TOML        string         `yaml:"toml"`
	Validation  *Validation    `yaml:"validation"`
	Nested      bool           `yaml:"nested"`
	ModeDefault map[string]any `yaml:"mode_default"`
	CLIOnly     bool           `yaml:"cli_only"`
}

// CLIConfig represents CLI flag configuration
type CLIConfig struct {
	Long        string `yaml:"long"`
	Short       string `yaml:"short"`
	Description string `yaml:"description"`
}

// Validation represents field validation rules
type Validation struct {
	Enum   []string `yaml:"enum"`
	Format string   `yaml:"format"`
	Regex  string   `yaml:"regex"`
	Min    *int     `yaml:"min"`
	Max    *int     `yaml:"max"`
	Port   bool     `yaml:"port"`
}

func main() {
	flag.Parse()

	if *schemaPath == "" || *outputDir == "" {
		log.Fatal("Both -schema and -output flags are required")
	}

	// Read schema file
	schemaData, err := os.ReadFile(*schemaPath)
	if err != nil {
		log.Fatalf("Failed to read schema file: %v", err)
	}

	var schema Schema
	if err := yaml.Unmarshal(schemaData, &schema); err != nil {
		log.Fatalf("Failed to parse schema: %v", err)
	}

	// Create output directory if it doesn't exist
	if err := os.MkdirAll(*outputDir, 0755); err != nil {
		log.Fatalf("Failed to create output directory: %v", err)
	}

	// Generate files
	generators := []struct {
		name     string
		template string
		genFunc  func(*Schema) (string, error)
	}{
		{"config.gen.go", configTemplate, generateConfig},
		{"cli.gen.go", cliTemplate, generateCLI},
		{"loader.gen.go", loaderTemplate, generateLoader},
		{"defaults.gen.go", defaultsTemplate, generateDefaults},
		{"validation.gen.go", validationTemplate, generateValidation},
		{"env.gen.go", envTemplate, generateEnv},
	}

	for _, gen := range generators {
		content, err := gen.genFunc(&schema)
		if err != nil {
			log.Fatalf("Failed to generate %s: %v", gen.name, err)
		}

		// Format the Go code
		formatted, err := format.Source([]byte(content))
		if err != nil {
			// Write unformatted for debugging
			debugPath := filepath.Join(*outputDir, gen.name+".debug")
			os.WriteFile(debugPath, []byte(content), 0644)
			log.Fatalf("Failed to format %s (debug output written to %s): %v", gen.name, debugPath, err)
		}

		// Write the file
		outPath := filepath.Join(*outputDir, gen.name)
		if err := os.WriteFile(outPath, formatted, 0644); err != nil {
			log.Fatalf("Failed to write %s: %v", gen.name, err)
		}

		log.Printf("Generated %s", outPath)
	}
}

// generateConfig generates the config structs
func generateConfig(schema *Schema) (string, error) {
	tmpl, err := template.New("config").Funcs(template.FuncMap{
		"title": toGoName,
	}).Parse(configTemplate)
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, schema); err != nil {
		return "", err
	}

	return buf.String(), nil
}

// generateCLI generates the CLI flags struct
func generateCLI(schema *Schema) (string, error) {
	tmpl, err := template.New("cli").Funcs(template.FuncMap{
		"goType":    goTypeForCLI,
		"title":     toGoName,
		"escapeTag": escapeTagValue,
	}).Parse(cliTemplate)
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, schema); err != nil {
		return "", err
	}

	return buf.String(), nil
}

// generateLoader generates the loader code
func generateLoader(schema *Schema) (string, error) {
	tmpl, err := template.New("loader").Funcs(template.FuncMap{
		"title": toGoName,
	}).Parse(loaderTemplate)
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, schema); err != nil {
		return "", err
	}

	return buf.String(), nil
}

// generateDefaults generates the default configuration
func generateDefaults(schema *Schema) (string, error) {
	tmpl, err := template.New("defaults").Funcs(template.FuncMap{
		"formatDefault": formatDefault,
		"title":         toGoName,
	}).Parse(defaultsTemplate)
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, schema); err != nil {
		return "", err
	}

	return buf.String(), nil
}

// generateValidation generates validation functions
func generateValidation(schema *Schema) (string, error) {
	tmpl, err := template.New("validation").Funcs(template.FuncMap{
		"title": toGoName,
	}).Parse(validationTemplate)
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, schema); err != nil {
		return "", err
	}

	return buf.String(), nil
}

// generateEnv generates environment variable handling
func generateEnv(schema *Schema) (string, error) {
	tmpl, err := template.New("env").Funcs(template.FuncMap{
		"title": toGoName,
	}).Parse(envTemplate)
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, schema); err != nil {
		return "", err
	}

	return buf.String(), nil
}

// Helper functions
func toGoName(s string) string {
	// Convert snake_case or kebab-case to PascalCase with proper Go initialisms
	parts := strings.FieldsFunc(s, func(r rune) bool {
		return r == '_' || r == '-'
	})

	for i, part := range parts {
		if len(part) > 0 {
			parts[i] = toGoInitialism(part)
		}
	}

	return strings.Join(parts, "")
}

// toGoInitialism converts a word to proper Go naming convention
// handling common initialisms like HTTP, TLS, IP, ID, etc.
func toGoInitialism(s string) string {
	// Common Go initialisms that should be all caps
	initialisms := map[string]string{
		"http":  "HTTP",
		"https": "HTTPS",
		"tls":   "TLS",
		"ssl":   "SSL",
		"ip":    "IP",
		"ips":   "IPs",
		"id":    "ID",
		"ids":   "IDs",
		"api":   "API",
		"apis":  "APIs",
		"url":   "URL",
		"urls":  "URLs",
		"uri":   "URI",
		"uris":  "URIs",
		"dns":   "DNS",
		"tcp":   "TCP",
		"udp":   "UDP",
		"rpc":   "RPC",
		"sql":   "SQL",
		"db":    "DB",
		"cpu":   "CPU",
		"ram":   "RAM",
		"json":  "JSON",
		"xml":   "XML",
		"yaml":  "YAML",
		"toml":  "TOML",
		"uuid":  "UUID",
		"uuids": "UUIDs",
		"vm":    "VM",
		"vms":   "VMs",
		"os":    "OS",
		"io":    "IO",
		"ui":    "UI",
		"utf":   "UTF",
		"utf8":  "UTF8",
		"ascii": "ASCII",
		"ttl":   "TTL",
		"eof":   "EOF",
		"lhs":   "LHS",
		"rhs":   "RHS",
		"etcd":  "Etcd", // Keep etcd as Etcd since it's a product name
	}

	lower := strings.ToLower(s)
	if replacement, ok := initialisms[lower]; ok {
		return replacement
	}

	// Default: capitalize first letter
	if len(s) > 0 {
		return strings.ToUpper(s[:1]) + strings.ToLower(s[1:])
	}
	return s
}

// escapeTagValue escapes a string for use in struct tags
func escapeTagValue(s string) string {
	// Replace quotes with escaped quotes
	s = strings.ReplaceAll(s, `"`, `\"`)
	// Replace backticks with single quotes
	s = strings.ReplaceAll(s, "`", "'")
	// Replace newlines with spaces
	s = strings.ReplaceAll(s, "\n", " ")
	return s
}

func goTypeForCLI(fieldType string) string {
	// For CLI, we use pointers to distinguish set vs unset
	switch fieldType {
	case "string":
		return "*string"
	case "int":
		return "*int"
	case "bool":
		return "*bool"
	case "[]string":
		return "[]string" // Slices can be nil
	default:
		return "*" + fieldType
	}
}

func formatDefault(val interface{}, fieldType string) string {
	// Arrays don't need pointers, they have nil as a zero value
	if fieldType == "[]string" {
		if val == nil {
			return "[]string{}"
		}
		switch v := val.(type) {
		case []interface{}:
			if len(v) == 0 {
				return "[]string{}"
			}
			var items []string
			for _, item := range v {
				items = append(items, fmt.Sprintf(`"%s"`, item))
			}
			return fmt.Sprintf("[]string{%s}", strings.Join(items, ", "))
		default:
			return "[]string{}"
		}
	}

	// For non-array types, use pointers
	if val == nil {
		// Return nil for pointer fields with no default
		return "nil"
	}

	switch v := val.(type) {
	case string:
		s := fmt.Sprintf(`"%s"`, v)
		return fmt.Sprintf("strPtr(%s)", s)
	case bool:
		return fmt.Sprintf("boolPtr(%v)", v)
	case int:
		return fmt.Sprintf("intPtr(%d)", v)
	default:
		return fmt.Sprintf("%#v", v)
	}
}

// Template definitions
const configTemplate = `// Code generated by configgen. DO NOT EDIT.

package {{.Package}}

import (
	"time"
)

{{range $name, $config := .Configs}}
// {{$name}} {{if $config.Description}}{{$config.Description}}{{end}}
type {{$name}} struct {
	{{- range $fieldName, $field := $config.Fields}}
	{{- if not $field.CLIOnly}}
	{{- if $field.Nested}}
	{{$fieldName | title}} {{$field.Type}} ` + "`" + `toml:"{{$field.TOML}}"` + "`" + `
	{{- else if eq $field.Type "[]string"}}
	{{$fieldName | title}} {{$field.Type}} ` + "`" + `toml:"{{$field.TOML}}"{{if $field.Env}} env:"{{$field.Env}}"{{end}}` + "`" + `
	{{- else}}
	{{$fieldName | title}} *{{$field.Type}} ` + "`" + `toml:"{{$field.TOML}}"{{if $field.Env}} env:"{{$field.Env}}"{{end}}` + "`" + `
	{{- end}}
	{{- end}}
	{{- end}}
}

{{- range $fieldName, $field := $config.Fields}}
{{- if not $field.CLIOnly}}
{{- if not $field.Nested}}
{{- if ne $field.Type "[]string"}}

// Get{{$fieldName | title}} returns the value of {{$fieldName | title}} or its zero value if nil
func (c *{{$name}}) Get{{$fieldName | title}}() {{$field.Type}} {
	if c.{{$fieldName | title}} != nil {
		return *c.{{$fieldName | title}}
	}
	{{- if eq $field.Type "string"}}
	return ""
	{{- else if eq $field.Type "int"}}
	return 0
	{{- else if eq $field.Type "bool"}}
	return false
	{{- end}}
}

// Set{{$fieldName | title}} sets the value of {{$fieldName | title}}
func (c *{{$name}}) Set{{$fieldName | title}}(v {{$field.Type}}) {
	c.{{$fieldName | title}} = &v
}
{{- end}}
{{- end}}
{{- end}}
{{- end}}
{{end}}

// HTTPRequestTimeoutDuration returns the timeout as a time.Duration
func (c *ServerConfig) HTTPRequestTimeoutDuration() time.Duration {
	if c.HTTPRequestTimeout != nil {
		return time.Duration(*c.HTTPRequestTimeout) * time.Second
	}
	return 60 * time.Second // default
}
`

const cliTemplate = `// Code generated by configgen. DO NOT EDIT.

package {{.Package}}

// CLIFlags represents command-line flags for server configuration
// All fields are pointers to distinguish between set and unset values
type CLIFlags struct {
	{{- range $cname, $config := .Configs}}
	{{- range $fname, $field := $config.Fields}}
	{{- if $field.CLI}}
	{{if eq $cname "Config"}}{{$fname | title}}{{else}}{{$cname}}{{$fname | title}}{{end}} {{goType $field.Type}} ` + "`" + `{{if $field.CLI.Long}}long:"{{$field.CLI.Long}}"{{end}}{{if $field.CLI.Short}} short:"{{$field.CLI.Short}}"{{end}}{{if $field.CLI.Description}} description:"{{$field.CLI.Description | escapeTag}}"{{end}}` + "`" + `
	{{- end}}
	{{- end}}
	{{- end}}
}

// NewCLIFlags creates a new CLIFlags struct for parsing
func NewCLIFlags() *CLIFlags {
	return &CLIFlags{}
}
`

const loaderTemplate = `// Code generated by configgen. DO NOT EDIT.

package {{.Package}}

import (
	"fmt"
	"os"
	"path/filepath"
	"log/slog"

	"github.com/pelletier/go-toml/v2"
)

// Load loads configuration from all sources with proper precedence:
// CLI flags > Environment variables > Config file > Defaults
func Load(configPath string, flags *CLIFlags, log *slog.Logger) (*Config, error) {
	if log == nil {
		log = slog.Default()
	}

	cfg := DefaultConfig()

	// Determine data path for config discovery with proper precedence
	// CLI > Env > Defaults
	var dataPathForSearch string
	if cfg.Server.DataPath != nil {
		dataPathForSearch = *cfg.Server.DataPath
	}
	if flags != nil && flags.ServerConfigDataPath != nil && *flags.ServerConfigDataPath != "" {
		dataPathForSearch = *flags.ServerConfigDataPath
	} else if envDataPath := os.Getenv("MIREN_SERVER_DATA_PATH"); envDataPath != "" {
		dataPathForSearch = envDataPath
	}

	// Load config file
	filePath := findConfigFile(configPath, dataPathForSearch)
	if filePath != "" {
		log.Info("loading config file", "path", filePath)
		if err := loadConfigFile(filePath, cfg); err != nil {
			return nil, fmt.Errorf("failed to load config file: %w", err)
		}
	} else if configPath != "" {
		return nil, fmt.Errorf("config file not found: %s", configPath)
	}

	// Resolve the effective mode first (CLI > Env > Config > Default)
	// We need this to apply mode-specific defaults correctly
	var effectiveMode string
	if cfg.Mode != nil {
		effectiveMode = *cfg.Mode
	}
	if envMode := os.Getenv("MIREN_MODE"); envMode != "" {
		effectiveMode = envMode
	}
	if flags != nil && flags.Mode != nil && *flags.Mode != "" {
		effectiveMode = *flags.Mode
	}

	// Apply mode defaults based on the resolved mode
	// Only set if not already set (nil check)
	{{range $cname, $config := .Configs}}
	{{- $structField := ""}}{{range $k, $v := (index $.Configs "Config").Fields}}{{if eq $v.Type $cname}}{{$structField = ($k | title)}}{{end}}{{end}}
	{{- range $fname, $field := $config.Fields}}
	{{- if $field.ModeDefault}}
	{{- range $mode, $val := $field.ModeDefault}}
	if effectiveMode == "{{$mode}}" {
		if cfg.{{if $structField}}{{$structField}}.{{end}}{{$fname | title}} == nil {
			cfg.{{if $structField}}{{$structField}}.{{end}}{{$fname | title}} = boolPtr({{$val}})
		}
	}
	{{- end}}
	{{- end}}
	{{- end}}
	{{- end}}

	// Apply environment variables (can override mode defaults)
	if err := applyEnvironmentVariables(cfg, log); err != nil {
		return nil, fmt.Errorf("failed to apply environment variables: %w", err)
	}

	// Apply CLI flags (can override everything)
	if flags != nil {
		applyCLIFlags(cfg, flags)
	}

	// Post-process etcd configuration
	// If embedded etcd is enabled and no endpoints are specified, set default endpoint
	if cfg.Etcd.StartEmbedded != nil && *cfg.Etcd.StartEmbedded && len(cfg.Etcd.Endpoints) == 0 {
		port := 12379
		if cfg.Etcd.ClientPort != nil {
			port = *cfg.Etcd.ClientPort
		}
		cfg.Etcd.Endpoints = []string{fmt.Sprintf("http://127.0.0.1:%d", port)}
	}

	// Validate
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("configuration validation failed: %w", err)
	}

	return cfg, nil
}

func findConfigFile(explicitPath, dataPath string) string {
	if explicitPath != "" {
		if _, err := os.Stat(explicitPath); err == nil {
			return explicitPath
		}
		return ""
	}

	searchPaths := []string{
		"/etc/miren/server.toml",
		filepath.Join(dataPath, "config", "server.toml"),
	}

	for _, path := range searchPaths {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}

	return ""
}

func loadConfigFile(path string, cfg *Config) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}

	if err := toml.Unmarshal(data, cfg); err != nil {
		return fmt.Errorf("failed to parse TOML: %w", err)
	}

	return nil
}

func applyCLIFlags(cfg *Config, flags *CLIFlags) {
	{{range $cname, $config := .Configs}}
	{{$structField := $cname}}{{range $k, $v := (index $.Configs "Config").Fields}}{{if eq $v.Type $cname}}{{$structField = ($k | title)}}{{end}}{{end}}
	{{range $fname, $field := $config.Fields}}
	{{if and $field.CLI (not $field.CLIOnly)}}
	{{$flagName := $fname | title}}{{if ne $cname "Config"}}{{$flagName = print $cname ($fname | title)}}{{end}}
	{{if eq $field.Type "string"}}
	if flags.{{$flagName}} != nil && *flags.{{$flagName}} != "" {
		cfg.{{if ne $cname "Config"}}{{$structField}}.{{end}}{{$fname | title}} = flags.{{$flagName}}
	}
	{{else if eq $field.Type "int"}}
	if flags.{{$flagName}} != nil {
		cfg.{{if ne $cname "Config"}}{{$structField}}.{{end}}{{$fname | title}} = flags.{{$flagName}}
	}
	{{else if eq $field.Type "bool"}}
	if flags.{{$flagName}} != nil {
		cfg.{{if ne $cname "Config"}}{{$structField}}.{{end}}{{$fname | title}} = flags.{{$flagName}}
	}
	{{else if eq $field.Type "[]string"}}
	if len(flags.{{$flagName}}) > 0 {
		cfg.{{if ne $cname "Config"}}{{$structField}}.{{end}}{{$fname | title}} = flags.{{$flagName}}
	}
	{{end}}
	{{end}}
	{{end}}
	{{end}}
}
`

const defaultsTemplate = `// Code generated by configgen. DO NOT EDIT.

package {{.Package}}

// Helper functions for creating pointers to literals
func boolPtr(b bool) *bool { return &b }
func intPtr(i int) *int { return &i }
func strPtr(s string) *string { return &s }

// DefaultConfig returns the default configuration
func DefaultConfig() *Config {
	return &Config{
		{{- range $fname, $field := (index .Configs "Config").Fields}}
		{{- if not $field.CLIOnly}}
		{{- if $field.Nested}}
		{{$fname | title}}: Default{{$field.Type}}(),
		{{- else}}
		{{$fname | title}}: {{formatDefault $field.Default $field.Type}},
		{{- end}}
		{{- end}}
		{{- end}}
	}
}

{{range $name, $config := .Configs}}
{{- if ne $name "Config"}}
// Default{{$name}} returns default {{$name}}
func Default{{$name}}() {{$name}} {
	return {{$name}}{
		{{- range $fname, $field := $config.Fields}}
		{{$fname | title}}: {{formatDefault $field.Default $field.Type}},
		{{- end}}
	}
}
{{end}}
{{end}}
`

const validationTemplate = `// Code generated by configgen. DO NOT EDIT.

package {{.Package}}

import (
	"fmt"
	"net"
	"regexp"
)

// Validate validates the configuration
func (c *Config) Validate() error {
	{{- range $fname, $field := (index .Configs "Config").Fields}}
	{{- if $field.Validation}}
	{{- if $field.Validation.Enum}}
	// Validate {{$fname}}
	if c.{{$fname | title}} != nil {
		validModes := map[string]bool{
			{{- range $val := $field.Validation.Enum}}
			"{{$val}}": true,
			{{- end}}
		}
		if !validModes[*c.{{$fname | title}}] {
			return fmt.Errorf("invalid {{$fname}} %q: must be one of {{$field.Validation.Enum}}", *c.{{$fname | title}})
		}
	}
	{{- end}}
	{{- end}}
	{{- if $field.Nested}}

	if err := c.{{$fname | title}}.Validate(); err != nil {
		return fmt.Errorf("{{$fname}}: %w", err)
	}
	{{- end}}
	{{- end}}
	return nil
}

{{range $name, $config := .Configs}}
{{- if ne $name "Config"}}
// Validate validates {{$name}}
func (c *{{$name}}) Validate() error {
	{{- range $fname, $field := $config.Fields}}
	{{- if $field.Validation}}
	{{if eq $field.Validation.Format "host:port"}}
	// Validate {{$fname}}
	if c.{{$fname | title}} != nil && *c.{{$fname | title}} != "" {
		if _, _, err := net.SplitHostPort(*c.{{$fname | title}}); err != nil {
			return fmt.Errorf("invalid {{$fname}} %q: %w", *c.{{$fname | title}}, err)
		}
	}
	{{else if eq $field.Validation.Format "ip_list"}}
	// Validate {{$fname}}
	for _, ip := range c.{{$fname | title}} {
		if net.ParseIP(ip) == nil {
			return fmt.Errorf("invalid IP address %q in {{$fname}}", ip)
		}
	}
	{{else if $field.Validation.Port}}
	// Validate {{$fname}}
	if c.{{$fname | title}} != nil && (*c.{{$fname | title}} < 1 || *c.{{$fname | title}} > 65535) {
		return fmt.Errorf("{{$fname}} must be between 1 and 65535, got %d", *c.{{$fname | title}})
	}
	{{end}}
	{{if $field.Validation.Regex}}
	// Validate {{$fname}} regex
	if c.{{$fname | title}} != nil && *c.{{$fname | title}} != "" {
		matched, err := regexp.MatchString(` + "`{{$field.Validation.Regex}}`" + `, *c.{{$fname | title}})
		if err != nil {
			return fmt.Errorf("invalid regex pattern for {{$fname}}: %w", err)
		}
		if !matched {
			return fmt.Errorf("invalid {{$fname}} %q: must match pattern %q", *c.{{$fname | title}}, ` + "`{{$field.Validation.Regex}}`" + `)
		}
	}
	{{end}}
	{{if $field.Validation.Min}}
	// Validate {{$fname}} minimum
	if c.{{$fname | title}} != nil && *c.{{$fname | title}} < {{$field.Validation.Min}} {
		return fmt.Errorf("{{$fname}} must be at least {{$field.Validation.Min}}, got %d", *c.{{$fname | title}})
	}
	{{end}}
	{{if $field.Validation.Max}}
	// Validate {{$fname}} maximum
	if c.{{$fname | title}} != nil && *c.{{$fname | title}} > {{$field.Validation.Max}} {
		return fmt.Errorf("{{$fname}} must be at most {{$field.Validation.Max}}, got %d", *c.{{$fname | title}})
	}
	{{end}}
	{{if $field.Validation.Enum}}
	// Validate {{$fname}} enum
	if c.{{$fname | title}} != nil {
		valid{{$fname | title}} := map[string]bool{
			{{- range $val := $field.Validation.Enum}}
			"{{$val}}": true,
			{{- end}}
		}
		if !valid{{$fname | title}}[*c.{{$fname | title}}] {
			return fmt.Errorf("invalid {{$fname}} %q: must be one of {{$field.Validation.Enum}}", *c.{{$fname | title}})
		}
	}
	{{end}}
	{{end}}
	{{end}}
	
	// Check for port conflicts in {{$name}}
	{{- if or (eq $name "EtcdConfig") }}
	seen := make(map[int]bool)
	{{- range $fname, $field := $config.Fields}}
	{{- if $field.Validation}}{{if $field.Validation.Port}}
	if c.{{$fname | title}} != nil {
		if seen[*c.{{$fname | title}}] {
			return fmt.Errorf("port conflict: port %d is used multiple times", *c.{{$fname | title}})
		}
		seen[*c.{{$fname | title}}] = true
	}
	{{- end}}{{end}}
	{{- end}}
	{{end}}
	
	{{if eq $name "EtcdConfig"}}
	// Validate etcd endpoints requirement
	if c.StartEmbedded != nil && !*c.StartEmbedded && len(c.Endpoints) == 0 {
		return fmt.Errorf("etcd endpoints must be set when start_embedded=false")
	}
	{{end}}
	
	return nil
}
{{end}}
{{end}}
`

const envTemplate = `// Code generated by configgen. DO NOT EDIT.

package {{.Package}}

import (
	"log/slog"
	"os"
	"strconv"
	"strings"
)

// applyEnvironmentVariables applies environment variables to the configuration
func applyEnvironmentVariables(cfg *Config, log *slog.Logger) error {
	{{range $cname, $config := .Configs}}
	{{$structField := $cname}}{{range $k, $v := (index $.Configs "Config").Fields}}{{if eq $v.Type $cname}}{{$structField = ($k | title)}}{{end}}{{end}}
	{{range $fname, $field := $config.Fields}}
	{{if $field.Env}}
	// Apply {{$field.Env}}
	if val := os.Getenv("{{$field.Env}}"); val != "" {
		{{if eq $field.Type "string"}}
		cfg.{{if ne $cname "Config"}}{{$structField}}.{{end}}{{$fname | title}} = &val
		log.Debug("applied env var", "key", "{{$field.Env}}")
		{{else if eq $field.Type "int"}}
		if i, err := strconv.Atoi(val); err == nil {
			cfg.{{if ne $cname "Config"}}{{$structField}}.{{end}}{{$fname | title}} = &i
			log.Debug("applied env var", "key", "{{$field.Env}}")
		} else {
			log.Warn("invalid {{$field.Env}} value", "value", val, "error", err)
		}
		{{else if eq $field.Type "bool"}}
		if b, err := strconv.ParseBool(val); err == nil {
			cfg.{{if ne $cname "Config"}}{{$structField}}.{{end}}{{$fname | title}} = &b
			log.Debug("applied env var", "key", "{{$field.Env}}")
		} else {
			log.Warn("invalid {{$field.Env}} value", "value", val, "error", err)
		}
		{{else if eq $field.Type "[]string"}}
		// Split and clean CSV list
		parts := strings.Split(val, ",")
		cleaned := make([]string, 0, len(parts))
		seen := make(map[string]struct{})
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			if _, exists := seen[p]; exists {
				continue
			}
			seen[p] = struct{}{}
			cleaned = append(cleaned, p)
		}
		cfg.{{if ne $cname "Config"}}{{$structField}}.{{end}}{{$fname | title}} = cleaned
		log.Debug("applied env var", "key", "{{$field.Env}}", "count", len(cleaned))
		{{end}}
	}
	{{end}}
	{{end}}
	{{end}}
	return nil
}
`
