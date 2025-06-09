package clientconfig

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	// DefaultConfigPath is the default path for the config file in user's home directory
	DefaultConfigPath = ".config/runtime/clientconfig.yaml"

	// EnvConfigPath is the environment variable name for custom config path
	EnvConfigPath = "RUNTIME_CONFIG"
)

// ClusterConfig holds the configuration for a single cluster
type ClusterConfig struct {
	Hostname   string `yaml:"hostname"`
	CACert     string `yaml:"ca_cert"`            // PEM encoded CA certificate
	ClientCert string `yaml:"client_cert"`        // PEM encoded client certificate
	ClientKey  string `yaml:"client_key"`         // PEM encoded client key
	Insecure   bool   `yaml:"insecure,omitempty"` // Skip TLS verification
}

// Config represents the complete client configuration
type Config struct {
	ActiveCluster string                    `yaml:"active_cluster"`
	Clusters      map[string]*ClusterConfig `yaml:"clusters"`

	// The path to the config file that was loaded, if any.
	sourcePath string
}

func NewConfig() *Config {
	return &Config{
		Clusters: make(map[string]*ClusterConfig),
	}
}

// Validate checks if the configuration is valid
func (c *Config) Validate() error {
	if c.ActiveCluster != "" {
		if _, exists := c.Clusters[c.ActiveCluster]; !exists {
			return fmt.Errorf("active cluster %q not found in configured clusters", c.ActiveCluster)
		}
	}
	return nil
}

// GetActiveCluster returns the active cluster configuration
func (c *Config) GetActiveCluster() (*ClusterConfig, error) {
	if c.ActiveCluster == "" {
		return nil, fmt.Errorf("no active cluster configured")
	}
	return c.GetCluster(c.ActiveCluster)
}

// SetActiveCluster sets the active cluster
func (c *Config) SetActiveCluster(name string) error {
	if _, exists := c.Clusters[name]; !exists {
		return fmt.Errorf("cannot set active cluster: cluster %q not found in configuration", name)
	}
	c.ActiveCluster = name
	return nil
}

// LoadConfig loads the configuration from disk
func LoadConfig() (*Config, error) {
	configPath, err := getConfigPath()
	if err != nil {
		return nil, fmt.Errorf("failed to determine config path: %w", err)
	}

	// Load main config file
	data, err := os.ReadFile(configPath)
	if err != nil {
		// If main config doesn't exist, try loading from config.d directory
		config := NewConfig()
		config.sourcePath = configPath

		if err := loadConfigDir(config); err != nil {
			return nil, ErrNoConfig
		}

		if err := config.Validate(); err != nil {
			return nil, fmt.Errorf("invalid configuration: %w", err)
		}

		return config, nil
	}

	var config Config
	config.sourcePath = configPath

	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	// Load additional configs from config.d directory
	if err := loadConfigDir(&config); err != nil {
		return nil, fmt.Errorf("failed to load config.d: %w", err)
	}

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return &config, nil
}

var ErrNoConfig = fmt.Errorf("no config file found")

// LoadConfig loads the configuration from disk
func LoadConfigFrom(configPath string) (*Config, error) {
	if configPath == "-" {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return nil, fmt.Errorf("failed to read config from stdin: %w", err)
		}

		return DecodeConfig(data)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, ErrNoConfig
	}

	var config Config
	config.sourcePath = configPath
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return &config, nil
}

// LoadConfig loads the configuration from disk
func DecodeConfig(data []byte) (*Config, error) {
	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return &config, nil
}

// SaveConfig saves the configuration to disk
func (c *Config) Save() error {
	configPath := c.sourcePath

	var err error
	if configPath == "" {
		configPath, err = getConfigPath()
		if err != nil {
			return fmt.Errorf("failed to determine config path: %w", err)
		}
	}

	// Ensure directory exists
	configDir := filepath.Dir(configPath)
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	return c.SaveTo(configPath)
}

func (c *Config) SaveTo(path string) error {
	if err := c.Validate(); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if path == "-" {
		os.Stdout.Write(data)
		return nil
	}

	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

func (c *Config) SaveToHome() error {
	configPath, err := getConfigPath()
	if err != nil {
		return fmt.Errorf("failed to determine config path: %w", err)
	}

	dir := filepath.Dir(configPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	return c.SaveTo(configPath)
}

// Merge merges another Config with the current one
// If updateActiveCluster is true, the ActiveCluster from the other config will override the current one
// Cluster configurations from the other config will be merged into the current config's clusters
func (c *Config) Merge(other *Config, updateActiveCluster, force bool) error {
	if c.Clusters == nil {
		c.Clusters = make(map[string]*ClusterConfig)
	}

	// Merge clusters from other config
	for name, cluster := range other.Clusters {
		if _, exists := c.Clusters[name]; exists && !force {
			return fmt.Errorf("cluster %q already exists in current config, use --force to overwrite", name)
		}

		c.Clusters[name] = cluster
	}

	// Update active cluster if requested and other config has one
	if c.ActiveCluster == "" || (updateActiveCluster && other.ActiveCluster != "") {
		c.ActiveCluster = other.ActiveCluster
	}

	return nil
}

// GetCluster returns the configuration for a specific cluster
func (c *Config) GetCluster(name string) (*ClusterConfig, error) {
	cluster, exists := c.Clusters[name]
	if !exists {
		return nil, fmt.Errorf("cluster %q not found in configuration", name)
	}
	return cluster, nil
}

// loadConfigDir loads additional config files from the config.d directory
func loadConfigDir(config *Config) error {
	// Determine the config.d directory path
	configDirPath, err := getConfigDirPath()
	if err != nil {
		return fmt.Errorf("failed to determine config.d path: %w", err)
	}

	// Check if the directory exists
	if _, err := os.Stat(configDirPath); os.IsNotExist(err) {
		// Directory doesn't exist, which is fine
		return nil
	}

	// Read all files in the directory
	entries, err := os.ReadDir(configDirPath)
	if err != nil {
		return fmt.Errorf("failed to read config.d directory: %w", err)
	}

	// Filter and sort YAML files
	var yamlFiles []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasSuffix(name, ".yaml") || strings.HasSuffix(name, ".yml") {
			yamlFiles = append(yamlFiles, filepath.Join(configDirPath, name))
		}
	}
	sort.Strings(yamlFiles)

	// Load and merge each config file
	for _, file := range yamlFiles {
		data, err := os.ReadFile(file)
		if err != nil {
			return fmt.Errorf("failed to read config file %s: %w", file, err)
		}

		var additionalConfig Config
		if err := yaml.Unmarshal(data, &additionalConfig); err != nil {
			return fmt.Errorf("failed to parse config file %s: %w", file, err)
		}

		// Merge the additional config into the main config
		// Don't update active cluster from additional configs by default
		if err := config.Merge(&additionalConfig, false, true); err != nil {
			return fmt.Errorf("failed to merge config from %s: %w", file, err)
		}
	}

	return nil
}

// getConfigDirPath determines the configuration directory path
func getConfigDirPath() (string, error) {
	// Check environment variable first
	if envPath := os.Getenv(EnvConfigPath); envPath != "" {
		// If a custom config path is specified, use its directory
		return filepath.Join(filepath.Dir(envPath), "clientconfig.d"), nil
	}

	// Fall back to default path in user's home directory
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get user home directory: %w", err)
	}

	return filepath.Join(homeDir, ".config/runtime/clientconfig.d"), nil
}

// getConfigPath determines the configuration file path
func getConfigPath() (string, error) {
	// Check environment variable first
	if envPath := os.Getenv(EnvConfigPath); envPath != "" {
		return envPath, nil
	}

	// Fall back to default path in user's home directory
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get user home directory: %w", err)
	}

	return filepath.Join(homeDir, DefaultConfigPath), nil
}
