package serverconfig

import (
	"fmt"
	"net"
	"time"
)

// Config represents the complete server configuration from all sources
type Config struct {
	// Mode determines server operation mode
	Mode string `toml:"mode" env:"MIREN_MODE"`

	// Server contains core server settings
	Server ServerConfig `toml:"server"`

	// TLS contains TLS/certificate settings
	TLS TLSConfig `toml:"tls"`

	// Etcd contains etcd configuration
	Etcd EtcdConfig `toml:"etcd"`

	// ClickHouse contains ClickHouse configuration
	ClickHouse ClickHouseConfig `toml:"clickhouse"`

	// Containerd contains containerd configuration
	Containerd ContainerdConfig `toml:"containerd"`
}

// ServerConfig contains core server settings
type ServerConfig struct {
	Address            string `toml:"address" env:"MIREN_SERVER_ADDRESS"`
	RunnerAddress      string `toml:"runner_address" env:"MIREN_SERVER_RUNNER_ADDRESS"`
	DataPath           string `toml:"data_path" env:"MIREN_SERVER_DATA_PATH"`
	RunnerID           string `toml:"runner_id" env:"MIREN_SERVER_RUNNER_ID"`
	ReleasePath        string `toml:"release_path" env:"MIREN_SERVER_RELEASE_PATH"`
	ConfigClusterName  string `toml:"config_cluster_name" env:"MIREN_SERVER_CONFIG_CLUSTER_NAME"`
	SkipClientConfig   bool   `toml:"skip_client_config" env:"MIREN_SERVER_SKIP_CLIENT_CONFIG"`
	HTTPRequestTimeout int    `toml:"http_request_timeout" env:"MIREN_SERVER_HTTP_REQUEST_TIMEOUT"`
}

// TLSConfig contains TLS/certificate settings
type TLSConfig struct {
	AdditionalNames []string `toml:"additional_names" env:"MIREN_TLS_ADDITIONAL_NAMES"`
	AdditionalIPs   []string `toml:"additional_ips" env:"MIREN_TLS_ADDITIONAL_IPS"`
	StandardTLS     bool     `toml:"standard_tls" env:"MIREN_TLS_STANDARD_TLS"`
}

// EtcdConfig contains etcd configuration
type EtcdConfig struct {
	Endpoints      []string `toml:"endpoints" env:"MIREN_ETCD_ENDPOINTS"`
	Prefix         string   `toml:"prefix" env:"MIREN_ETCD_PREFIX"`
	StartEmbedded  bool     `toml:"start_embedded" env:"MIREN_ETCD_START_EMBEDDED"`
	ClientPort     int      `toml:"client_port" env:"MIREN_ETCD_CLIENT_PORT"`
	PeerPort       int      `toml:"peer_port" env:"MIREN_ETCD_PEER_PORT"`
	HTTPClientPort int      `toml:"http_client_port" env:"MIREN_ETCD_HTTP_CLIENT_PORT"`
}

// ClickHouseConfig contains ClickHouse configuration
type ClickHouseConfig struct {
	StartEmbedded   bool   `toml:"start_embedded" env:"MIREN_CLICKHOUSE_START_EMBEDDED"`
	HTTPPort        int    `toml:"http_port" env:"MIREN_CLICKHOUSE_HTTP_PORT"`
	NativePort      int    `toml:"native_port" env:"MIREN_CLICKHOUSE_NATIVE_PORT"`
	InterServerPort int    `toml:"interserver_port" env:"MIREN_CLICKHOUSE_INTERSERVER_PORT"`
	Address         string `toml:"address" env:"MIREN_CLICKHOUSE_ADDRESS"`
}

// ContainerdConfig contains containerd configuration
type ContainerdConfig struct {
	StartEmbedded bool   `toml:"start_embedded" env:"MIREN_CONTAINERD_START_EMBEDDED"`
	BinaryPath    string `toml:"binary_path" env:"MIREN_CONTAINERD_BINARY_PATH"`
	SocketPath    string `toml:"socket_path" env:"MIREN_CONTAINERD_SOCKET_PATH"`
}

// ConfigSource tracks where a configuration value came from
type ConfigSource string

const (
	SourceDefault ConfigSource = "default"
	SourceFile    ConfigSource = "file"
	SourceEnv     ConfigSource = "environment"
	SourceCLI     ConfigSource = "cli"
)

// SourcedConfig wraps Config with source tracking for debugging
type SourcedConfig struct {
	Config  Config
	Sources map[string]ConfigSource // Track source of each config value
}

func (c *Config) Validate() error {
	if c.Mode != "standalone" && c.Mode != "distributed" {
		return fmt.Errorf("invalid mode %q: must be 'standalone' or 'distributed'", c.Mode)
	}

	if err := c.Server.Validate(); err != nil {
		return fmt.Errorf("server config: %w", err)
	}

	if err := c.TLS.Validate(); err != nil {
		return fmt.Errorf("tls config: %w", err)
	}

	if err := c.Etcd.Validate(); err != nil {
		return fmt.Errorf("etcd config: %w", err)
	}

	if err := c.ClickHouse.Validate(); err != nil {
		return fmt.Errorf("clickhouse config: %w", err)
	}

	if err := c.Containerd.Validate(); err != nil {
		return fmt.Errorf("containerd config: %w", err)
	}

	return nil
}

func (c *ServerConfig) Validate() error {
	if c.HTTPRequestTimeout <= 0 {
		return fmt.Errorf("http_request_timeout must be positive, got %d", c.HTTPRequestTimeout)
	}

	if c.Address != "" {
		if _, _, err := net.SplitHostPort(c.Address); err != nil {
			return fmt.Errorf("invalid address %q: %w", c.Address, err)
		}
	}

	if c.RunnerAddress != "" {
		if _, _, err := net.SplitHostPort(c.RunnerAddress); err != nil {
			return fmt.Errorf("invalid runner_address %q: %w", c.RunnerAddress, err)
		}
	}

	return nil
}

func (c *TLSConfig) Validate() error {
	for _, ip := range c.AdditionalIPs {
		if net.ParseIP(ip) == nil {
			return fmt.Errorf("invalid IP address %q", ip)
		}
	}
	return nil
}

func (c *EtcdConfig) Validate() error {
	if !c.StartEmbedded && len(c.Endpoints) == 0 {
		return fmt.Errorf("etcd endpoints must be set when start_embedded=false")
	}
	if err := validatePort(c.ClientPort, "client_port"); err != nil {
		return err
	}
	if err := validatePort(c.PeerPort, "peer_port"); err != nil {
		return err
	}
	if err := validatePort(c.HTTPClientPort, "http_client_port"); err != nil {
		return err
	}

	ports := []int{c.ClientPort, c.PeerPort, c.HTTPClientPort}
	seen := make(map[int]bool)
	for _, port := range ports {
		if seen[port] {
			return fmt.Errorf("port conflict: port %d is used multiple times", port)
		}
		seen[port] = true
	}

	return nil
}

func (c *ClickHouseConfig) Validate() error {
	if err := validatePort(c.HTTPPort, "http_port"); err != nil {
		return err
	}
	if err := validatePort(c.NativePort, "native_port"); err != nil {
		return err
	}
	if err := validatePort(c.InterServerPort, "interserver_port"); err != nil {
		return err
	}

	ports := []int{c.HTTPPort, c.NativePort, c.InterServerPort}
	seen := make(map[int]bool)
	for _, port := range ports {
		if seen[port] {
			return fmt.Errorf("port conflict: port %d is used multiple times", port)
		}
		seen[port] = true
	}

	return nil
}

func (c *ContainerdConfig) Validate() error {
	return nil
}

func validatePort(port int, name string) error {
	if port < 1 || port > 65535 {
		return fmt.Errorf("%s must be between 1 and 65535, got %d", name, port)
	}
	return nil
}

// ApplyModeDefaults applies mode-specific defaults.
// In standalone mode, embedded services are automatically started.
func (c *Config) ApplyModeDefaults() {
	if c.Mode == "standalone" {
		c.Etcd.StartEmbedded = true
		c.ClickHouse.StartEmbedded = true
		c.Containerd.StartEmbedded = true
	}
}

func (c *ServerConfig) HTTPRequestTimeoutDuration() time.Duration {
	return time.Duration(c.HTTPRequestTimeout) * time.Second
}
