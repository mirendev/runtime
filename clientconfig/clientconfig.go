package clientconfig

import (
	"fmt"
	"os"
	"path/filepath"

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
	CACert     string `yaml:"ca_cert"`     // PEM encoded CA certificate
	ClientCert string `yaml:"client_cert"` // PEM encoded client certificate
	ClientKey  string `yaml:"client_key"`  // PEM encoded client key
	Insecure   bool   `yaml:"insecure"`    // Skip TLS verification
}

// Config represents the complete client configuration
type Config struct {
	ActiveCluster string                    `yaml:"active_cluster"`
	Clusters      map[string]*ClusterConfig `yaml:"clusters"`
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

	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return &config, nil
}

// LoadConfig loads the configuration from disk
func LoadConfigFrom(configPath string) (*Config, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config Config
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
func SaveConfig(config *Config) error {
	configPath, err := getConfigPath()
	if err != nil {
		return fmt.Errorf("failed to determine config path: %w", err)
	}

	// Ensure directory exists
	configDir := filepath.Dir(configPath)
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	return config.SaveTo(configPath)
}

func (c *Config) SaveTo(path string) error {
	if err := c.Validate(); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
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

// GetCluster returns the configuration for a specific cluster
func (c *Config) GetCluster(name string) (*ClusterConfig, error) {
	cluster, exists := c.Clusters[name]
	if !exists {
		return nil, fmt.Errorf("cluster %q not found in configuration", name)
	}
	return cluster, nil
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
