package serverconfig

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/pelletier/go-toml/v2"
)

// CLIFlags represents the command-line flags passed to the server
type CLIFlags struct {
	Mode                      string
	Address                   string
	RunnerAddress             string
	EtcdEndpoints             []string
	EtcdPrefix                string
	RunnerID                  string
	DataPath                  string
	ReleasePath               string
	AdditionalNames           []string
	AdditionalIPs             []string
	StandardTLS               bool
	HTTPRequestTimeout        int
	StartEtcd                 bool
	EtcdClientPort            int
	EtcdPeerPort              int
	EtcdHTTPClientPort        int
	StartClickHouse           bool
	ClickHouseHTTPPort        int
	ClickHouseNativePort      int
	ClickHouseInterServerPort int
	ClickHouseAddress         string
	StartContainerd           bool
	ContainerdBinary          string
	ContainerdSocketPath      string
	SkipClientConfig          bool
	ConfigClusterName         string

	// Flags that were explicitly set (vs using defaults)
	SetFlags map[string]bool
}

// Load loads configuration from all sources with proper precedence:
// CLI flags > Environment variables > Config file > Defaults
func Load(configPath string, flags *CLIFlags, log *slog.Logger) (*SourcedConfig, error) {
	if log == nil {
		log = slog.Default()
	}

	cfg := DefaultConfig()
	sources := make(map[string]ConfigSource)
	setDefaultSources(sources)

	filePath := findConfigFile(configPath, cfg.Server.DataPath)
	if filePath != "" {
		log.Info("loading config file", "path", filePath)
		if err := loadConfigFile(filePath, cfg, sources); err != nil {
			return nil, fmt.Errorf("failed to load config file: %w", err)
		}
	} else if configPath != "" {
		return nil, fmt.Errorf("config file not found: %s", configPath)
	} else {
		log.Debug("no config file found, using defaults")
	}

	if err := applyEnvironmentVariables(cfg, sources, log); err != nil {
		return nil, fmt.Errorf("failed to apply environment variables: %w", err)
	}

	if flags != nil {
		applyCLIFlags(cfg, flags, sources)
	}

	cfg.ApplyModeDefaults()

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("configuration validation failed: %w", err)
	}

	logConfigSources(log, cfg, sources)

	return &SourcedConfig{
		Config:  *cfg,
		Sources: sources,
	}, nil
}

// findConfigFile searches for a config file in the standard locations
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

func loadConfigFile(path string, cfg *Config, sources map[string]ConfigSource) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}

	var fileCfg Config
	if err := toml.Unmarshal(data, &fileCfg); err != nil {
		return fmt.Errorf("failed to parse TOML: %w", err)
	}

	mergeConfig(cfg, &fileCfg, sources, SourceFile)

	return nil
}

// mergeConfig merges src into dst, updating sources for non-zero values
func mergeConfig(dst, src *Config, sources map[string]ConfigSource, source ConfigSource) {
	if src.Mode != "" {
		dst.Mode = src.Mode
		sources["mode"] = source
	}

	mergeServerConfig(&dst.Server, &src.Server, sources, source)
	mergeTLSConfig(&dst.TLS, &src.TLS, sources, source)
	mergeEtcdConfig(&dst.Etcd, &src.Etcd, sources, source)
	mergeClickHouseConfig(&dst.ClickHouse, &src.ClickHouse, sources, source)
	mergeContainerdConfig(&dst.Containerd, &src.Containerd, sources, source)
}

// mergeServerConfig merges server configuration
func mergeServerConfig(dst, src *ServerConfig, sources map[string]ConfigSource, source ConfigSource) {
	if src.Address != "" {
		dst.Address = src.Address
		sources["server.address"] = source
	}
	if src.RunnerAddress != "" {
		dst.RunnerAddress = src.RunnerAddress
		sources["server.runner_address"] = source
	}
	if src.DataPath != "" {
		dst.DataPath = src.DataPath
		sources["server.data_path"] = source
	}
	if src.RunnerID != "" {
		dst.RunnerID = src.RunnerID
		sources["server.runner_id"] = source
	}
	if src.ReleasePath != "" {
		dst.ReleasePath = src.ReleasePath
		sources["server.release_path"] = source
	}
	if src.ConfigClusterName != "" {
		dst.ConfigClusterName = src.ConfigClusterName
		sources["server.config_cluster_name"] = source
	}
	if src.HTTPRequestTimeout != 0 {
		dst.HTTPRequestTimeout = src.HTTPRequestTimeout
		sources["server.http_request_timeout"] = source
	}
	// Bool fields need special handling since zero value is meaningful
	if source == SourceFile || source == SourceEnv || source == SourceCLI {
		dst.SkipClientConfig = src.SkipClientConfig
		sources["server.skip_client_config"] = source
	}
}

// mergeTLSConfig merges TLS configuration
func mergeTLSConfig(dst, src *TLSConfig, sources map[string]ConfigSource, source ConfigSource) {
	if len(src.AdditionalNames) > 0 {
		dst.AdditionalNames = src.AdditionalNames
		sources["tls.additional_names"] = source
	}
	if len(src.AdditionalIPs) > 0 {
		dst.AdditionalIPs = src.AdditionalIPs
		sources["tls.additional_ips"] = source
	}
	// Bool fields need special handling since zero value is meaningful
	if source == SourceFile || source == SourceEnv || source == SourceCLI {
		dst.StandardTLS = src.StandardTLS
		sources["tls.standard_tls"] = source
	}
}

// mergeEtcdConfig merges Etcd configuration
func mergeEtcdConfig(dst, src *EtcdConfig, sources map[string]ConfigSource, source ConfigSource) {
	if len(src.Endpoints) > 0 {
		dst.Endpoints = src.Endpoints
		sources["etcd.endpoints"] = source
	}
	if src.Prefix != "" {
		dst.Prefix = src.Prefix
		sources["etcd.prefix"] = source
	}
	if src.ClientPort != 0 {
		dst.ClientPort = src.ClientPort
		sources["etcd.client_port"] = source
	}
	if src.PeerPort != 0 {
		dst.PeerPort = src.PeerPort
		sources["etcd.peer_port"] = source
	}
	if src.HTTPClientPort != 0 {
		dst.HTTPClientPort = src.HTTPClientPort
		sources["etcd.http_client_port"] = source
	}
	// Bool fields need special handling since zero value is meaningful
	if source == SourceFile || source == SourceEnv || source == SourceCLI {
		dst.StartEmbedded = src.StartEmbedded
		sources["etcd.start_embedded"] = source
	}
}

// mergeClickHouseConfig merges ClickHouse configuration
func mergeClickHouseConfig(dst, src *ClickHouseConfig, sources map[string]ConfigSource, source ConfigSource) {
	if src.HTTPPort != 0 {
		dst.HTTPPort = src.HTTPPort
		sources["clickhouse.http_port"] = source
	}
	if src.NativePort != 0 {
		dst.NativePort = src.NativePort
		sources["clickhouse.native_port"] = source
	}
	if src.InterServerPort != 0 {
		dst.InterServerPort = src.InterServerPort
		sources["clickhouse.interserver_port"] = source
	}
	if src.Address != "" {
		dst.Address = src.Address
		sources["clickhouse.address"] = source
	}
	// Bool fields need special handling since zero value is meaningful
	if source == SourceFile || source == SourceEnv || source == SourceCLI {
		dst.StartEmbedded = src.StartEmbedded
		sources["clickhouse.start_embedded"] = source
	}
}

// mergeContainerdConfig merges Containerd configuration
func mergeContainerdConfig(dst, src *ContainerdConfig, sources map[string]ConfigSource, source ConfigSource) {
	if src.BinaryPath != "" {
		dst.BinaryPath = src.BinaryPath
		sources["containerd.binary_path"] = source
	}
	if src.SocketPath != "" {
		dst.SocketPath = src.SocketPath
		sources["containerd.socket_path"] = source
	}
	// Bool fields need special handling since zero value is meaningful
	if source == SourceFile || source == SourceEnv || source == SourceCLI {
		dst.StartEmbedded = src.StartEmbedded
		sources["containerd.start_embedded"] = source
	}
}

// setDefaultSources marks all fields as coming from defaults
func setDefaultSources(sources map[string]ConfigSource) {
	defaultFields := []string{
		"mode",
		"server.address",
		"server.runner_address",
		"server.data_path",
		"server.runner_id",
		"server.release_path",
		"server.config_cluster_name",
		"server.http_request_timeout",
		"server.skip_client_config",
		"tls.additional_names",
		"tls.additional_ips",
		"tls.standard_tls",
		"etcd.endpoints",
		"etcd.prefix",
		"etcd.client_port",
		"etcd.peer_port",
		"etcd.http_client_port",
		"etcd.start_embedded",
		"clickhouse.http_port",
		"clickhouse.native_port",
		"clickhouse.interserver_port",
		"clickhouse.address",
		"clickhouse.start_embedded",
		"containerd.binary_path",
		"containerd.socket_path",
		"containerd.start_embedded",
	}

	for _, field := range defaultFields {
		sources[field] = SourceDefault
	}
}

func applyCLIFlags(cfg *Config, flags *CLIFlags, sources map[string]ConfigSource) {
	if flags.SetFlags == nil {
		flags.SetFlags = make(map[string]bool)
	}

	wasSet := func(name string) bool {
		return flags.SetFlags[name]
	}

	if wasSet("mode") && flags.Mode != "" {
		cfg.Mode = flags.Mode
		sources["mode"] = SourceCLI
	}

	if wasSet("address") && flags.Address != "" {
		cfg.Server.Address = flags.Address
		sources["server.address"] = SourceCLI
	}
	if wasSet("runner-address") && flags.RunnerAddress != "" {
		cfg.Server.RunnerAddress = flags.RunnerAddress
		sources["server.runner_address"] = SourceCLI
	}
	if wasSet("data-path") && flags.DataPath != "" {
		cfg.Server.DataPath = flags.DataPath
		sources["server.data_path"] = SourceCLI
	}
	if wasSet("runner-id") && flags.RunnerID != "" {
		cfg.Server.RunnerID = flags.RunnerID
		sources["server.runner_id"] = SourceCLI
	}
	if wasSet("release-path") && flags.ReleasePath != "" {
		cfg.Server.ReleasePath = flags.ReleasePath
		sources["server.release_path"] = SourceCLI
	}
	if wasSet("config-cluster-name") && flags.ConfigClusterName != "" {
		cfg.Server.ConfigClusterName = flags.ConfigClusterName
		sources["server.config_cluster_name"] = SourceCLI
	}
	if wasSet("skip-client-config") {
		cfg.Server.SkipClientConfig = flags.SkipClientConfig
		sources["server.skip_client_config"] = SourceCLI
	}
	if wasSet("http-request-timeout") && flags.HTTPRequestTimeout != 0 {
		cfg.Server.HTTPRequestTimeout = flags.HTTPRequestTimeout
		sources["server.http_request_timeout"] = SourceCLI
	}

	if wasSet("dns-names") && len(flags.AdditionalNames) > 0 {
		cfg.TLS.AdditionalNames = flags.AdditionalNames
		sources["tls.additional_names"] = SourceCLI
	}
	if wasSet("ips") && len(flags.AdditionalIPs) > 0 {
		cfg.TLS.AdditionalIPs = flags.AdditionalIPs
		sources["tls.additional_ips"] = SourceCLI
	}
	if wasSet("serve-tls") {
		cfg.TLS.StandardTLS = flags.StandardTLS
		sources["tls.standard_tls"] = SourceCLI
	}

	if wasSet("etcd") && len(flags.EtcdEndpoints) > 0 {
		cfg.Etcd.Endpoints = flags.EtcdEndpoints
		sources["etcd.endpoints"] = SourceCLI
	}
	if wasSet("etcd-prefix") && flags.EtcdPrefix != "" {
		cfg.Etcd.Prefix = flags.EtcdPrefix
		sources["etcd.prefix"] = SourceCLI
	}
	if wasSet("start-etcd") {
		cfg.Etcd.StartEmbedded = flags.StartEtcd
		sources["etcd.start_embedded"] = SourceCLI
	}
	if wasSet("etcd-client-port") && flags.EtcdClientPort != 0 {
		cfg.Etcd.ClientPort = flags.EtcdClientPort
		sources["etcd.client_port"] = SourceCLI
	}
	if wasSet("etcd-peer-port") && flags.EtcdPeerPort != 0 {
		cfg.Etcd.PeerPort = flags.EtcdPeerPort
		sources["etcd.peer_port"] = SourceCLI
	}
	if wasSet("etcd-http-client-port") && flags.EtcdHTTPClientPort != 0 {
		cfg.Etcd.HTTPClientPort = flags.EtcdHTTPClientPort
		sources["etcd.http_client_port"] = SourceCLI
	}

	if wasSet("start-clickhouse") {
		cfg.ClickHouse.StartEmbedded = flags.StartClickHouse
		sources["clickhouse.start_embedded"] = SourceCLI
	}
	if wasSet("clickhouse-http-port") && flags.ClickHouseHTTPPort != 0 {
		cfg.ClickHouse.HTTPPort = flags.ClickHouseHTTPPort
		sources["clickhouse.http_port"] = SourceCLI
	}
	if wasSet("clickhouse-native-port") && flags.ClickHouseNativePort != 0 {
		cfg.ClickHouse.NativePort = flags.ClickHouseNativePort
		sources["clickhouse.native_port"] = SourceCLI
	}
	if wasSet("clickhouse-interserver-port") && flags.ClickHouseInterServerPort != 0 {
		cfg.ClickHouse.InterServerPort = flags.ClickHouseInterServerPort
		sources["clickhouse.interserver_port"] = SourceCLI
	}
	if wasSet("clickhouse-addr") && flags.ClickHouseAddress != "" {
		cfg.ClickHouse.Address = flags.ClickHouseAddress
		sources["clickhouse.address"] = SourceCLI
	}

	if wasSet("start-containerd") {
		cfg.Containerd.StartEmbedded = flags.StartContainerd
		sources["containerd.start_embedded"] = SourceCLI
	}
	if wasSet("containerd-binary") && flags.ContainerdBinary != "" {
		cfg.Containerd.BinaryPath = flags.ContainerdBinary
		sources["containerd.binary_path"] = SourceCLI
	}
	if wasSet("containerd-socket") && flags.ContainerdSocketPath != "" {
		cfg.Containerd.SocketPath = flags.ContainerdSocketPath
		sources["containerd.socket_path"] = SourceCLI
	}
}

// logConfigSources logs where each configuration value came from
func logConfigSources(log *slog.Logger, cfg *Config, sources map[string]ConfigSource) {
	// Log at info level for values not from defaults
	importantSources := []struct {
		path   string
		value  interface{}
		source ConfigSource
	}{
		{"mode", cfg.Mode, sources["mode"]},
		{"server.address", cfg.Server.Address, sources["server.address"]},
		{"etcd.endpoints", cfg.Etcd.Endpoints, sources["etcd.endpoints"]},
	}

	log.Info("Configuration sources:")
	for _, item := range importantSources {
		if item.source != SourceDefault {
			log.Info("  "+item.path, "value", item.value, "source", item.source)
		}
	}

	// Log all sources at debug level
	if log.Enabled(context.TODO(), slog.LevelDebug) {
		log.Debug("All configuration sources:")
		for path, source := range sources {
			log.Debug("  "+path, "source", source)
		}
	}
}
