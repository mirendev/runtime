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
	DefaultConfigPath = ".config/miren/clientconfig.yaml"

	// EnvConfigPath is the environment variable name for custom config path
	EnvConfigPath = "MIREN_CONFIG"
)

// IdentityConfig holds authentication credentials that can be used across clusters
type IdentityConfig struct {
	Type       string `yaml:"type"`                  // Type of identity: "keypair", "certificate", etc.
	Issuer     string `yaml:"issuer,omitempty"`      // The auth server that issued this identity (e.g., "https://miren.cloud")
	PrivateKey string `yaml:"private_key,omitempty"` // PEM encoded private key (for keypair auth)
	ClientCert string `yaml:"client_cert,omitempty"` // PEM encoded client certificate (for cert auth)
	ClientKey  string `yaml:"client_key,omitempty"`  // PEM encoded client key (for cert auth)
}

// ClusterConfig holds the configuration for a single cluster
type ClusterConfig struct {
	Hostname     string   `yaml:"hostname"`
	AllAddresses []string `yaml:"all_addresses,omitempty"` // All available addresses for this cluster
	Identity     string   `yaml:"identity,omitempty"`      // Reference to an identity in the Identities section
	CACert       string   `yaml:"ca_cert,omitempty"`       // PEM encoded CA certificate
	ClientCert   string   `yaml:"client_cert,omitempty"`   // PEM encoded client certificate (deprecated, use identity)
	ClientKey    string   `yaml:"client_key,omitempty"`    // PEM encoded client key (deprecated, use identity)
	Insecure     bool     `yaml:"insecure,omitempty"`      // Skip TLS verification
	CloudAuth    bool     `yaml:"cloud_auth,omitempty"`    // Use cloud authentication (deprecated, use identity)
}

// Config represents the complete client configuration
type Config struct {
	active     string
	clusters   map[string]*ClusterConfig
	identities map[string]*IdentityConfig

	// The path to the config file that was loaded, if any.
	sourcePath string

	// Whether clientconfig.d should be loaded for this config
	loadConfigD bool

	// Leaf configs loaded from config.d directory
	// These are kept separate and checked when accessing clusters/identities
	leafConfigs []*Config

	// Unsaved leaf configs that need to be written to clientconfig.d/
	// Map of filename -> ConfigData
	unsavedLeafConfigs map[string]*ConfigData
}

// ConfigData is used for YAML unmarshaling to handle private fields
type ConfigData struct {
	Active     string                     `yaml:"active_cluster,omitempty"`
	Clusters   map[string]*ClusterConfig  `yaml:"clusters,omitempty"`
	Identities map[string]*IdentityConfig `yaml:"identities,omitempty"`
}

// UnmarshalYAML implements the yaml.Unmarshaler interface
func (c *Config) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var temp ConfigData
	if err := unmarshal(&temp); err != nil {
		return err
	}

	c.active = temp.Active
	c.clusters = temp.Clusters
	c.identities = temp.Identities

	return nil
}

// MarshalYAML implements the yaml.Marshaler interface
func (c *Config) MarshalYAML() (interface{}, error) {
	return &ConfigData{
		Active:     c.active,
		Clusters:   c.clusters,
		Identities: c.identities,
	}, nil
}

func NewConfig() *Config {
	return &Config{
		clusters:           make(map[string]*ClusterConfig),
		identities:         make(map[string]*IdentityConfig),
		unsavedLeafConfigs: make(map[string]*ConfigData),
	}
}

// IsEmpty checks if the configuration has no clusters defined
func (c *Config) IsEmpty() bool {
	// Check main config
	if len(c.clusters) > 0 {
		return false
	}

	if len(c.identities) > 0 {
		return false
	}

	// Check leaf configs
	for _, leafConfig := range c.leafConfigs {
		if len(leafConfig.clusters) > 0 {
			return false
		}

		if len(leafConfig.identities) > 0 {
			return false
		}
	}

	return true
}

// Validate checks if the configuration is valid
func (c *Config) Validate() error {
	if c.active != "" {
		if !c.HasCluster(c.active) {
			return fmt.Errorf("active cluster %q not found in configured clusters", c.active)
		}
	}
	return nil
}

// ActiveCluster returns the name of the active cluster
func (c *Config) ActiveCluster() string {
	return c.active
}

// GetActiveCluster returns the active cluster configuration
func (c *Config) GetActiveCluster() (*ClusterConfig, error) {
	if c.active == "" {
		return nil, fmt.Errorf("no active cluster configured")
	}
	return c.GetCluster(c.active)
}

// GetIdentity returns an identity configuration by name
func (c *Config) GetIdentity(name string) (*IdentityConfig, error) {
	// Check leaf configs first (they override main config)
	for _, leafConfig := range c.leafConfigs {
		if leafIdentity, exists := leafConfig.identities[name]; exists {
			return leafIdentity, nil
		}
	}

	// Then check main config
	identity, exists := c.identities[name]
	if exists {
		return identity, nil
	}

	return nil, fmt.Errorf("identity %q not found", name)
}

// SetIdentity adds or updates an identity configuration
func (c *Config) SetIdentity(name string, identity *IdentityConfig) {
	if c.identities == nil {
		c.identities = make(map[string]*IdentityConfig)
	}
	c.identities[name] = identity

	// SetIdentity always modifies the main config, never leaf configs
}

// SetLeafConfig adds or updates a leaf config that will be saved to clientconfig.d/name.yaml
func (c *Config) SetLeafConfig(name string, configData *ConfigData) {
	if c.unsavedLeafConfigs == nil {
		c.unsavedLeafConfigs = make(map[string]*ConfigData)
	}

	// Store the config data to be saved later
	c.unsavedLeafConfigs[name] = configData

	// Create a Config from ConfigData and add to leafConfigs for immediate availability
	leafConfig := &Config{
		active:     configData.Active,
		clusters:   configData.Clusters,
		identities: configData.Identities,
		sourcePath: name, // Store the name so we can identify this leaf config later
	}

	// Replace any existing leaf config with the same source name
	found := false
	for i, existing := range c.leafConfigs {
		if existing.sourcePath == name {
			c.leafConfigs[i] = leafConfig
			found = true
			break
		}
	}

	// If not found, append to the list
	if !found {
		c.leafConfigs = append(c.leafConfigs, leafConfig)
	}

	// If the main config has no clusters and no active cluster set,
	// and this leaf config has clusters, set the first cluster as active
	if len(c.clusters) == 0 && c.active == "" && len(configData.Clusters) > 0 {
		// Find the first cluster name from the leaf config
		for clusterName := range configData.Clusters {
			c.active = clusterName
			break
		}
	}
}

// HasIdentity checks if an identity exists in the configuration
func (c *Config) HasIdentity(name string) bool {
	// First check main config
	_, exists := c.identities[name]
	if exists {
		return true
	}

	// Then check leaf configs
	for _, leafConfig := range c.leafConfigs {
		if _, exists := leafConfig.identities[name]; exists {
			return true
		}
	}

	return false
}

// GetIdentityNames returns a sorted list of all identity names
func (c *Config) GetIdentityNames() []string {
	nameSet := make(map[string]bool)

	// Add names from main config
	for name := range c.identities {
		nameSet[name] = true
	}

	// Add names from leaf configs
	for _, leafConfig := range c.leafConfigs {
		for name := range leafConfig.identities {
			nameSet[name] = true
		}
	}

	names := make([]string, 0, len(nameSet))
	for name := range nameSet {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// GetIdentityCount returns the number of configured identities
func (c *Config) GetIdentityCount() int {
	return len(c.GetIdentityNames())
}

// HasIdentities returns true if there are any identities configured
func (c *Config) HasIdentities() bool {
	return c.GetIdentityCount() > 0
}

// SetActiveCluster sets the active cluster
func (c *Config) SetActiveCluster(name string) error {
	if !c.HasCluster(name) {
		return fmt.Errorf("cannot set active cluster: cluster %q not found in configuration", name)
	}
	c.active = name
	return nil
}

// LoadConfig loads the configuration from disk
func LoadConfig() (*Config, error) {
	configPath, loadConfigD, err := getConfigPath()
	if err != nil {
		return nil, fmt.Errorf("failed to determine config path: %w", err)
	}

	// Load main config file
	data, err := os.ReadFile(configPath)
	if err != nil {
		// If main config doesn't exist, try loading from config.d directory
		config := NewConfig()
		config.sourcePath = configPath
		config.loadConfigD = loadConfigD

		if loadConfigD {
			if err := loadConfigDir(config); err != nil {
				return nil, ErrNoConfig
			}
		}

		// Check if config is still empty after loading config.d
		if config.IsEmpty() {
			return nil, ErrNoConfig
		}

		if err := config.Validate(); err != nil {
			return nil, fmt.Errorf("invalid configuration: %w", err)
		}

		return config, nil
	}

	var (
		config Config
		cdata  ConfigData
	)

	config.sourcePath = configPath
	config.loadConfigD = loadConfigD

	if err := yaml.Unmarshal(data, &cdata); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	config.active = cdata.Active
	config.clusters = cdata.Clusters
	config.identities = cdata.Identities

	// Load additional configs from config.d directory if enabled
	if loadConfigD {
		if err := loadConfigDir(&config); err != nil {
			return nil, fmt.Errorf("failed to load config.d: %w", err)
		}
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
		configPath, _, err = getConfigPath()
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

	// Save only the main config data (not leaf configs)
	var cdata ConfigData

	cdata.Active = c.active
	cdata.Clusters = c.clusters
	cdata.Identities = c.identities
	//}

	data, err := yaml.Marshal(cdata)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if path == "-" {
		_, err := os.Stdout.Write(data)
		if err != nil {
			return fmt.Errorf("failed to write config to stdout: %w", err)
		}
		return nil
	}

	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	// Save any unsaved leaf configs to clientconfig.d/
	if len(c.unsavedLeafConfigs) > 0 {
		if err := c.saveLeafConfigs(path); err != nil {
			return fmt.Errorf("failed to save leaf configs: %w", err)
		}
	}

	return nil
}

// sanitizeLeafName normalizes a leaf config name to prevent path traversal and weird filenames
func sanitizeLeafName(name string) string {
	// Remove any directory separators and parent directory references
	name = strings.ReplaceAll(name, "/", "_")
	name = strings.ReplaceAll(name, "\\", "_")
	name = strings.ReplaceAll(name, "..", "_")

	// Remove any file extensions to prevent confusion
	if idx := strings.LastIndex(name, "."); idx > 0 {
		name = name[:idx]
	}

	// Replace other potentially problematic characters
	name = strings.ReplaceAll(name, ":", "_")
	name = strings.ReplaceAll(name, "*", "_")
	name = strings.ReplaceAll(name, "?", "_")
	name = strings.ReplaceAll(name, "\"", "_")
	name = strings.ReplaceAll(name, "<", "_")
	name = strings.ReplaceAll(name, ">", "_")
	name = strings.ReplaceAll(name, "|", "_")

	// Trim any leading/trailing whitespace or underscores
	name = strings.Trim(name, " _")

	// If the name is empty after sanitization, use a default
	if name == "" {
		name = "unnamed"
	}

	return name
}

// saveLeafConfigs saves all unsaved leaf configs to clientconfig.d/ directory
func (c *Config) saveLeafConfigs(mainConfigPath string) error {
	// Determine the config.d directory path based on the main config path
	configDirPath := filepath.Join(filepath.Dir(mainConfigPath), "clientconfig.d")

	// Create the clientconfig.d directory if it doesn't exist
	if err := os.MkdirAll(configDirPath, 0755); err != nil {
		return fmt.Errorf("failed to create config.d directory: %w", err)
	}

	// Save each unsaved leaf config
	for name, configData := range c.unsavedLeafConfigs {
		leafPath := filepath.Join(configDirPath, sanitizeLeafName(name)+".yaml")

		data, err := yaml.Marshal(configData)
		if err != nil {
			return fmt.Errorf("failed to marshal leaf config %s: %w", name, err)
		}

		if err := os.WriteFile(leafPath, data, 0600); err != nil {
			return fmt.Errorf("failed to write leaf config %s: %w", name, err)
		}
	}

	// Clear the unsaved leaf configs since they've been saved
	c.unsavedLeafConfigs = make(map[string]*ConfigData)

	return nil
}

func (c *Config) SaveToHome() error {
	configPath, _, err := getConfigPath()
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
	// Check leaf configs first (they override main config)
	for _, leafConfig := range c.leafConfigs {
		if leafCluster, exists := leafConfig.clusters[name]; exists {
			return leafCluster, nil
		}
	}

	// Then check main config
	cluster, exists := c.clusters[name]
	if exists {
		return cluster, nil
	}

	return nil, fmt.Errorf("cluster %q not found in configuration", name)
}

// HasCluster checks if a cluster exists in the configuration
func (c *Config) HasCluster(name string) bool {
	// First check main config
	_, exists := c.clusters[name]
	if exists {
		return true
	}

	// Then check leaf configs
	for _, leafConfig := range c.leafConfigs {
		if _, exists := leafConfig.clusters[name]; exists {
			return true
		}
	}

	return false
}

// HasAnyClusters checks if any clusters are configured
func (c *Config) HasAnyClusters() bool {
	// Check main config
	if len(c.clusters) > 0 {
		return true
	}

	// Check leaf configs
	for _, leafConfig := range c.leafConfigs {
		if len(leafConfig.clusters) > 0 {
			return true
		}
	}

	return false
}

// SetCluster adds or updates a cluster in the configuration
func (c *Config) SetCluster(name string, cluster *ClusterConfig) {
	if c.clusters == nil {
		c.clusters = make(map[string]*ClusterConfig)
	}
	c.clusters[name] = cluster

	// SetCluster always modifies the main config, never leaf configs
}

// RemoveCluster removes a cluster from the configuration
func (c *Config) RemoveCluster(name string) error {
	if !c.HasCluster(name) {
		return fmt.Errorf("cluster %q not found in configuration", name)
	}

	// Don't allow removing the active cluster
	if c.active == name {
		return fmt.Errorf("cannot remove active cluster %q", name)
	}

	delete(c.clusters, name)

	return nil
}

// GetClusterNames returns a sorted list of all cluster names
func (c *Config) GetClusterNames() []string {
	nameSet := make(map[string]bool)

	// Add names from main config
	for name := range c.clusters {
		nameSet[name] = true
	}

	// Add names from leaf configs
	for _, leafConfig := range c.leafConfigs {
		for name := range leafConfig.clusters {
			nameSet[name] = true
		}
	}

	names := make([]string, 0, len(nameSet))
	for name := range nameSet {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// GetClusterCount returns the number of clusters in the configuration
func (c *Config) GetClusterCount() int {
	return len(c.GetClusterNames())
}

// IterateClusters calls the provided function for each cluster in sorted order
// If the function returns an error, iteration stops and the error is returned
func (c *Config) IterateClusters(fn func(name string, cluster *ClusterConfig) error) error {
	names := c.GetClusterNames()
	for _, name := range names {
		cluster, err := c.GetCluster(name)
		if err != nil {
			return err
		}
		if err := fn(name, cluster); err != nil {
			return err
		}
	}
	return nil
}

// loadConfigDir loads additional config files from the config.d directory
func loadConfigDir(config *Config) error {
	// If clientconfig.d loading is disabled, return early
	if !config.loadConfigD {
		return nil
	}

	var (
		configDirPath string
		err           error
	)

	if config.sourcePath != "" {
		configDirPath = filepath.Join(filepath.Dir(config.sourcePath), "clientconfig.d")
	} else {
		// Determine the config.d directory path
		configDirPath, err = getConfigDirPath()
		if err != nil {
			return fmt.Errorf("failed to determine config.d path: %w", err)
		}
		// If configDirPath is empty, it means clientconfig.d is disabled
		if configDirPath == "" {
			return nil
		}
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

	// Load each config file as a leaf config
	for _, file := range yamlFiles {
		data, err := os.ReadFile(file)
		if err != nil {
			return fmt.Errorf("failed to read config file %s: %w", file, err)
		}

		var (
			leafConfig Config
			cdata      ConfigData
		)

		if err := yaml.Unmarshal(data, &cdata); err != nil {
			return fmt.Errorf("failed to parse config file %s: %w", file, err)
		}

		leafConfig.active = cdata.Active
		leafConfig.clusters = cdata.Clusters
		leafConfig.identities = cdata.Identities

		// Store the leaf config separately
		config.leafConfigs = append(config.leafConfigs, &leafConfig)

		// If main config has no active cluster, use one from config.d
		if config.active == "" && leafConfig.active != "" {
			config.active = leafConfig.active
		}
	}

	return nil
}

// getConfigDirPath determines the configuration directory path
// Returns empty string if MIREN_CONFIG points to a file (disabling clientconfig.d)
func getConfigDirPath() (string, error) {
	// Check environment variable first
	if envPath := os.Getenv(EnvConfigPath); envPath != "" {
		// Check if it's a file or directory
		info, err := os.Stat(envPath)
		if err == nil {
			if !info.IsDir() {
				// It's a file, don't use clientconfig.d
				return "", nil
			}
			// It's a directory, use clientconfig.d within it
			return filepath.Join(envPath, "clientconfig.d"), nil
		}
		// Path doesn't exist yet, check if it has a .yaml or .yml extension
		if strings.HasSuffix(envPath, ".yaml") || strings.HasSuffix(envPath, ".yml") {
			// Looks like a file path, don't use clientconfig.d
			return "", nil
		}
		// Assume it's a directory
		return filepath.Join(envPath, "clientconfig.d"), nil
	}

	// Fall back to default path in user's home directory
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get user home directory: %w", err)
	}

	return filepath.Join(homeDir, ".config/miren/clientconfig.d"), nil
}

// getConfigPath determines the configuration file path and whether clientconfig.d should be loaded
// Returns: (configPath, loadConfigD, error)
func getConfigPath() (string, bool, error) {
	// Check environment variable first
	if envPath := os.Getenv(EnvConfigPath); envPath != "" {
		// Check if it's a file or directory
		info, err := os.Stat(envPath)
		if err == nil {
			if !info.IsDir() {
				// It's a file, use it directly, don't load clientconfig.d
				return envPath, false, nil
			}
			// It's a directory, append clientconfig.yaml and load clientconfig.d
			return filepath.Join(envPath, "clientconfig.yaml"), true, nil
		}
		// Path doesn't exist yet, check if it has a .yaml or .yml extension
		if strings.HasSuffix(envPath, ".yaml") || strings.HasSuffix(envPath, ".yml") {
			// Looks like a file path, use it directly, don't load clientconfig.d
			return envPath, false, nil
		}
		// Assume it's a directory, load clientconfig.d
		return filepath.Join(envPath, "clientconfig.yaml"), true, nil
	}

	// Fall back to default path in user's home directory, load clientconfig.d
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", false, fmt.Errorf("failed to get user home directory: %w", err)
	}

	return filepath.Join(homeDir, DefaultConfigPath), true, nil
}
