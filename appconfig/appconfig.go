package appconfig

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	toml "github.com/pelletier/go-toml/v2"
	tomlast "github.com/pelletier/go-toml/v2/unstable"
)

var aliasWordRegexp = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)

type AppEnvVar struct {
	Key         string `json:"key" toml:"key"`
	Value       string `json:"value,omitempty" toml:"value,omitempty"`
	Required    bool   `json:"required,omitempty" toml:"required,omitempty"`
	Sensitive   bool   `json:"sensitive,omitempty" toml:"sensitive,omitempty"`
	Description string `json:"description,omitempty" toml:"description,omitempty"`
}

type BuildConfig struct {
	Dockerfile string   `toml:"dockerfile"`
	OnBuild    []string `toml:"onbuild"`
	Version    string   `toml:"version"`

	AlpineImage string `toml:"alpine_image"`
}

// ServiceConcurrencyConfig represents per-service concurrency configuration
type ServiceConcurrencyConfig struct {
	Mode                string `toml:"mode"` // "auto" or "fixed"
	RequestsPerInstance int    `toml:"requests_per_instance"`
	ScaleDownDelay      string `toml:"scale_down_delay"` // e.g. "2m", "15m"
	NumInstances        int    `toml:"num_instances"`
	ShutdownTimeout     string `toml:"shutdown_timeout"` // e.g. "10s", "30s" - time to wait for graceful shutdown
}

// DiskConfig represents a disk attachment for a service.
// Provider defaults to "miren" (network disk) when empty.
// Use provider = "local" for node-local persistent storage.
type DiskConfig struct {
	Name         string `toml:"name"`
	Provider     string `toml:"provider"`
	MountPath    string `toml:"mount_path"`
	ReadOnly     bool   `toml:"read_only"`
	SizeGB       int    `toml:"size_gb"`
	Filesystem   string `toml:"filesystem"`
	LeaseTimeout string `toml:"lease_timeout"`
	Owner        string `toml:"owner"`
}

// PortConfig represents a network port for a service
type PortConfig struct {
	Port     int    `toml:"port"`
	Name     string `toml:"name"`
	Type     string `toml:"type"`
	NodePort int    `toml:"node_port"`
}

// ServiceConfig represents configuration for a specific service
type ServiceConfig struct {
	Command     string                    `toml:"command"`
	Port        int                       `toml:"port"`
	PortName    string                    `toml:"port_name"`
	PortType    string                    `toml:"port_type"`
	Ports       []PortConfig              `toml:"ports"`
	Image       string                    `toml:"image"`
	EnvVars     []AppEnvVar               `toml:"env"`
	Concurrency *ServiceConcurrencyConfig `toml:"concurrency"`
	Disks       []DiskConfig              `toml:"disks"`
	// PortTimeout overrides the default 15s wait for the service to bind
	// its port during startup. Accepts a Go duration string (e.g. "60s", "2m").
	// Empty falls back to the default; invalid duration strings are rejected
	// at parse time by Validate.
	PortTimeout string `toml:"port_timeout,omitempty"`
}

// AddonConfig represents configuration for an addon in app.toml.
type AddonConfig struct {
	Variant string `toml:"variant"`
	Version string `toml:"version"`
}

type AppConfig struct {
	Name         string                    `toml:"name"`
	PostImport   string                    `toml:"post_import,omitempty"`
	EnvVars      []AppEnvVar               `toml:"env,omitempty"`
	Concurrency  *int                      `toml:"concurrency,omitempty"`
	Services     map[string]*ServiceConfig `toml:"services,omitempty"`
	Build        *BuildConfig              `toml:"build,omitempty"`
	Include      []string                  `toml:"include,omitempty"`
	Addons       map[string]*AddonConfig   `toml:"addons,omitempty"`
	Aliases      map[string]string         `toml:"aliases,omitempty"`
	WorkloadRole string                    `toml:"workload_role,omitempty"`
}

const AppConfigPath = ".miren/app.toml"

func LoadAppConfig() (*AppConfig, error) {
	ac, _, err := LoadAppConfigWithPath()
	return ac, err
}

// LoadAppConfigWithPath loads the app config and returns the file path it was loaded from.
// Returns (nil, "", nil) if no config file is found.
func LoadAppConfigWithPath() (*AppConfig, string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return nil, "", err
	}

	for dir != "/" {
		path := filepath.Join(dir, AppConfigPath)
		data, err := os.ReadFile(path)
		if err != nil {
			if !os.IsNotExist(err) {
				return nil, "", err
			}
			dir = filepath.Dir(dir)
			continue
		}
		ac, parseErr := decodeAndValidate(data, path)
		if parseErr != nil {
			return nil, "", parseErr
		}
		return ac, path, nil
	}

	return nil, "", nil
}

func LoadAppConfigUnder(dir string) (*AppConfig, error) {
	path := filepath.Join(dir, AppConfigPath)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return decodeAndValidate(data, path)
}

func Parse(data []byte) (*AppConfig, error) {
	return decodeAndValidate(data, "<input>")
}

// decodeAndValidate decodes TOML data into an AppConfig and validates it,
// enriching any errors with file path, source locations, and suggestions.
func decodeAndValidate(data []byte, filePath string) (*AppConfig, error) {
	var ac AppConfig
	dec := toml.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&ac); err != nil {
		return nil, enrichDecodeError(filePath, data, err)
	}
	if err := ac.Validate(); err != nil {
		return nil, enrichValidationError(filePath, data, err)
	}
	return &ac, nil
}

// Validate checks that the AppConfig has valid values.
// Returns *ValidationError with a key path for AST-based line resolution.
func (ac *AppConfig) Validate() error {
	// Validate global environment variables
	// Note: empty values are allowed - secrets may be stored server-side
	for i, ev := range ac.EnvVars {
		if ev.Key == "" {
			return &ValidationError{KeyPath: "env", Message: fmt.Sprintf("env[%d]: key is required", i)}
		}
	}

	// Validate service configurations
	for serviceName, svcConfig := range ac.Services {
		if svcConfig == nil {
			continue
		}

		svcPrefix := "services." + serviceName

		// Validate concurrency if present
		if svcConfig.Concurrency != nil {
			concurrency := svcConfig.Concurrency
			concurrencyPath := svcPrefix + ".concurrency"

			// Validate mode
			if concurrency.Mode != "" && concurrency.Mode != "auto" && concurrency.Mode != "fixed" {
				return &ValidationError{
					KeyPath: concurrencyPath + ".mode",
					Message: fmt.Sprintf("service %s: invalid concurrency mode %q, must be \"auto\" or \"fixed\"", serviceName, concurrency.Mode),
				}
			}

			// Validate auto mode settings
			if concurrency.Mode == "auto" || concurrency.Mode == "" {
				if concurrency.RequestsPerInstance < 0 {
					return &ValidationError{
						KeyPath: concurrencyPath + ".requests_per_instance",
						Message: fmt.Sprintf("service %s: requests_per_instance must be non-negative", serviceName),
					}
				}
				if concurrency.ScaleDownDelay != "" {
					if _, err := time.ParseDuration(concurrency.ScaleDownDelay); err != nil {
						return &ValidationError{
							KeyPath: concurrencyPath + ".scale_down_delay",
							Message: fmt.Sprintf("service %s: invalid scale_down_delay %q: %v", serviceName, concurrency.ScaleDownDelay, err),
						}
					}
				}
				if concurrency.NumInstances > 0 {
					return &ValidationError{
						KeyPath: concurrencyPath + ".num_instances",
						Message: fmt.Sprintf("service %s: num_instances cannot be set in auto mode", serviceName),
					}
				}
			}

			// Validate fixed mode settings
			if concurrency.Mode == "fixed" {
				if concurrency.NumInstances <= 0 {
					return &ValidationError{
						KeyPath: concurrencyPath + ".num_instances",
						Message: fmt.Sprintf("service %s: num_instances must be at least 1 for fixed mode", serviceName),
					}
				}
				if concurrency.RequestsPerInstance > 0 {
					return &ValidationError{
						KeyPath: concurrencyPath + ".requests_per_instance",
						Message: fmt.Sprintf("service %s: requests_per_instance cannot be set in fixed mode", serviceName),
					}
				}
				if concurrency.ScaleDownDelay != "" {
					return &ValidationError{
						KeyPath: concurrencyPath + ".scale_down_delay",
						Message: fmt.Sprintf("service %s: scale_down_delay cannot be set in fixed mode", serviceName),
					}
				}
			}

			// Validate shutdown_timeout (applies to both modes)
			if concurrency.ShutdownTimeout != "" {
				if _, err := time.ParseDuration(concurrency.ShutdownTimeout); err != nil {
					return &ValidationError{
						KeyPath: concurrencyPath + ".shutdown_timeout",
						Message: fmt.Sprintf("service %s: invalid shutdown_timeout %q: %v", serviceName, concurrency.ShutdownTimeout, err),
					}
				}
			}
		}

		// Validate port_timeout (parsed downstream by resolvePortWaitTimeout;
		// catch typos like "120" missing the unit suffix at deploy time rather
		// than silently falling back to the 15s default).
		if svcConfig.PortTimeout != "" {
			if _, err := time.ParseDuration(svcConfig.PortTimeout); err != nil {
				return &ValidationError{
					KeyPath: svcPrefix + ".port_timeout",
					Message: fmt.Sprintf("service %s: invalid port_timeout %q: %v", serviceName, svcConfig.PortTimeout, err),
				}
			}
		}

		// Validate service environment variables
		// Note: empty values are allowed - secrets may be stored server-side
		for i, ev := range svcConfig.EnvVars {
			if ev.Key == "" {
				return &ValidationError{
					KeyPath: svcPrefix + ".env",
					Message: fmt.Sprintf("service %s: env[%d] key is required", serviceName, i),
				}
			}
		}

		// Validate ports configuration
		if len(svcConfig.Ports) > 0 {
			// Mutual exclusion: cannot use both ports[] and scalar port fields
			if svcConfig.Port != 0 || svcConfig.PortName != "" || svcConfig.PortType != "" {
				return &ValidationError{
					KeyPath: svcPrefix + ".ports",
					Message: fmt.Sprintf("service %s: cannot use both 'ports' array and scalar port/port_name/port_type fields", serviceName),
				}
			}

			seenNames := make(map[string]bool)
			type portProto struct {
				port     int
				protocol string
			}
			seenPortProto := make(map[portProto]bool)
			for i, p := range svcConfig.Ports {
				if p.Port <= 0 || p.Port > 65535 {
					return &ValidationError{
						KeyPath: svcPrefix + ".ports",
						Message: fmt.Sprintf("service %s: ports[%d] port must be between 1 and 65535", serviceName, i),
					}
				}
				if p.Name == "" {
					return &ValidationError{
						KeyPath: svcPrefix + ".ports",
						Message: fmt.Sprintf("service %s: ports[%d] name is required", serviceName, i),
					}
				}
				if p.Type != "" && p.Type != "http" && p.Type != "tcp" && p.Type != "udp" {
					return &ValidationError{
						KeyPath: svcPrefix + ".ports",
						Message: fmt.Sprintf("service %s: ports[%d] type must be \"http\", \"tcp\", or \"udp\"", serviceName, i),
					}
				}
				proto := "tcp"
				if p.Type == "udp" {
					proto = "udp"
				}
				if p.NodePort < 0 || p.NodePort > 65535 {
					return &ValidationError{
						KeyPath: svcPrefix + ".ports",
						Message: fmt.Sprintf("service %s: ports[%d] node_port must be between 0 and 65535", serviceName, i),
					}
				}
				if seenNames[p.Name] {
					return &ValidationError{
						KeyPath: svcPrefix + ".ports",
						Message: fmt.Sprintf("service %s: ports[%d] duplicate port name %q", serviceName, i, p.Name),
					}
				}
				seenNames[p.Name] = true
				pp := portProto{p.Port, proto}
				if seenPortProto[pp] {
					return &ValidationError{
						KeyPath: svcPrefix + ".ports",
						Message: fmt.Sprintf("service %s: ports[%d] duplicate port number %d (protocol %q)", serviceName, i, p.Port, proto),
					}
				}
				seenPortProto[pp] = true
			}
		}

		// Validate disk configurations
		hasMirenDisks := false
		for i, disk := range svcConfig.Disks {
			if disk.Provider != "" && disk.Provider != "miren" && disk.Provider != "local" {
				return &ValidationError{
					KeyPath: svcPrefix + ".disks",
					Message: fmt.Sprintf("service %s: disk[%d] (%s) has invalid provider %q, must be \"miren\" or \"local\"", serviceName, i, disk.Name, disk.Provider),
				}
			}
			if disk.Provider == "" || disk.Provider == "miren" {
				hasMirenDisks = true
			}
			if disk.Name == "" {
				return &ValidationError{
					KeyPath: svcPrefix + ".disks",
					Message: fmt.Sprintf("service %s: disk[%d] must have a name", serviceName, i),
				}
			}
			if disk.MountPath == "" {
				return &ValidationError{
					KeyPath: svcPrefix + ".disks",
					Message: fmt.Sprintf("service %s: disk[%d] (%s) must have a mount_path", serviceName, i, disk.Name),
				}
			}
			if !filepath.IsAbs(disk.MountPath) {
				return &ValidationError{
					KeyPath: svcPrefix + ".disks",
					Message: fmt.Sprintf("service %s: disk[%d] (%s) mount_path must be an absolute path", serviceName, i, disk.Name),
				}
			}
			if disk.Provider == "local" {
				if disk.SizeGB != 0 {
					return &ValidationError{
						KeyPath: svcPrefix + ".disks",
						Message: fmt.Sprintf("service %s: disk[%d] (%s) size_gb is not supported for local disks", serviceName, i, disk.Name),
					}
				}
				if disk.Filesystem != "" {
					return &ValidationError{
						KeyPath: svcPrefix + ".disks",
						Message: fmt.Sprintf("service %s: disk[%d] (%s) filesystem is not supported for local disks", serviceName, i, disk.Name),
					}
				}
				if disk.LeaseTimeout != "" {
					return &ValidationError{
						KeyPath: svcPrefix + ".disks",
						Message: fmt.Sprintf("service %s: disk[%d] (%s) lease_timeout is not supported for local disks", serviceName, i, disk.Name),
					}
				}
			} else {
				if disk.Filesystem != "" && disk.Filesystem != "ext4" && disk.Filesystem != "xfs" && disk.Filesystem != "btrfs" {
					return &ValidationError{
						KeyPath: svcPrefix + ".disks",
						Message: fmt.Sprintf("service %s: disk[%d] (%s) has invalid filesystem %q, must be ext4, xfs, or btrfs", serviceName, i, disk.Name, disk.Filesystem),
					}
				}
				if disk.SizeGB < 0 {
					return &ValidationError{
						KeyPath: svcPrefix + ".disks",
						Message: fmt.Sprintf("service %s: disk[%d] (%s) size_gb must be non-negative", serviceName, i, disk.Name),
					}
				}
				if disk.LeaseTimeout != "" {
					if _, err := time.ParseDuration(disk.LeaseTimeout); err != nil {
						return &ValidationError{
							KeyPath: svcPrefix + ".disks",
							Message: fmt.Sprintf("service %s: disk[%d] (%s) invalid lease_timeout %q: %v", serviceName, i, disk.Name, disk.LeaseTimeout, err),
						}
					}
				}
			}
		}

		// Miren disks require fixed concurrency with a single instance
		if hasMirenDisks {
			if svcConfig.Concurrency == nil || svcConfig.Concurrency.Mode != "fixed" {
				return &ValidationError{
					KeyPath: svcPrefix + ".concurrency",
					Message: fmt.Sprintf("service %s: miren disks can only be attached to services with fixed concurrency mode", serviceName),
				}
			}
			if svcConfig.Concurrency.NumInstances != 1 {
				return &ValidationError{
					KeyPath: svcPrefix + ".concurrency.num_instances",
					Message: fmt.Sprintf("service %s: miren disks can only be attached to services with fixed concurrency mode and num_instances=1", serviceName),
				}
			}
		}
	}

	for name, target := range ac.Aliases {
		words := strings.Fields(name)
		if len(words) == 0 {
			return &ValidationError{
				KeyPath: "aliases",
				Message: fmt.Sprintf("alias %q: name must not be empty", name),
			}
		}
		for _, word := range words {
			if !aliasWordRegexp.MatchString(word) {
				return &ValidationError{
					KeyPath: "aliases." + name,
					Message: fmt.Sprintf("alias %q: each word must start with a lowercase letter and contain only lowercase letters, numbers, dashes, and underscores", name),
				}
			}
		}
		if strings.TrimSpace(target) == "" {
			return &ValidationError{
				KeyPath: "aliases." + name,
				Message: fmt.Sprintf("alias %q: command must not be empty", name),
			}
		}
	}

	return nil
}

// ResolveDefaults populates Services map for all service names with fully-resolved defaults.
// If a service already has explicit config in app.toml, it is preserved.
// Otherwise, defaults are applied based on service name:
//   - "web": auto mode, requests_per_instance=10, scale_down_delay=15m
//   - others: fixed mode, num_instances=1
func (ac *AppConfig) ResolveDefaults(services []string) {
	if ac.Services == nil {
		ac.Services = make(map[string]*ServiceConfig)
	}

	for _, serviceName := range services {
		// Skip if service already has config
		if _, exists := ac.Services[serviceName]; exists {
			continue
		}

		// Apply defaults based on service name
		if serviceName == "web" {
			ac.Services[serviceName] = &ServiceConfig{
				Concurrency: &ServiceConcurrencyConfig{
					Mode:                "auto",
					RequestsPerInstance: 10,
					ScaleDownDelay:      "15m",
					ShutdownTimeout:     "10s",
				},
			}
		} else {
			ac.Services[serviceName] = &ServiceConfig{
				Concurrency: &ServiceConcurrencyConfig{
					Mode:            "fixed",
					NumInstances:    1,
					ShutdownTimeout: "10s",
				},
			}
		}
	}
}

// GetDefaultsForServices returns an AppConfig with defaults resolved for given service names.
// This is useful for migration - it provides the same defaults used at build time.
func GetDefaultsForServices(serviceNames []string) *AppConfig {
	ac := &AppConfig{}
	ac.ResolveDefaults(serviceNames)
	return ac
}

// AliasLineNumbers parses the TOML file at configPath and returns a map from
// alias name to the line number where it is defined. Uses the go-toml/v2 AST
// parser for accurate source locations.
func AliasLineNumbers(configPath string) map[string]int {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil
	}

	var p tomlast.Parser
	p.Reset(data)

	result := make(map[string]int)

	for p.NextExpression() {
		node := p.Expression()
		if node.Kind != tomlast.Table {
			continue
		}

		// Check if this is the [aliases] table
		keyIter := node.Key()
		if !keyIter.Next() {
			continue
		}
		if string(keyIter.Node().Data) != "aliases" {
			continue
		}

		// Iterate subsequent KeyValue expressions under [aliases]
		for p.NextExpression() {
			kv := p.Expression()
			if kv.Kind != tomlast.KeyValue {
				break
			}
			keyIter := kv.Key()
			if !keyIter.Next() {
				continue
			}
			keyNode := keyIter.Node()
			name := string(keyNode.Data)
			shape := p.Shape(keyNode.Raw)
			result[name] = shape.Start.Line
		}
		break
	}

	return result
}
