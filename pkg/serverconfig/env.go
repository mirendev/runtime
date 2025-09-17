package serverconfig

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
)

func applyEnvironmentVariables(cfg *Config, sources map[string]ConfigSource, log *slog.Logger) error {
	appliedVars := []string{}

	if val := os.Getenv("MIREN_MODE"); val != "" {
		cfg.Mode = val
		sources["mode"] = SourceEnv
		appliedVars = append(appliedVars, "MIREN_MODE")
	}

	if err := applyServerEnvVars(&cfg.Server, sources, &appliedVars); err != nil {
		return err
	}

	if err := applyTLSEnvVars(&cfg.TLS, sources, &appliedVars); err != nil {
		return err
	}

	if err := applyEtcdEnvVars(&cfg.Etcd, sources, &appliedVars); err != nil {
		return err
	}

	if err := applyClickHouseEnvVars(&cfg.ClickHouse, sources, &appliedVars); err != nil {
		return err
	}

	if err := applyContainerdEnvVars(&cfg.Containerd, sources, &appliedVars); err != nil {
		return err
	}

	if len(appliedVars) > 0 {
		log.Debug("applied environment variables", "count", len(appliedVars), "vars", appliedVars)
	}

	return nil
}

// applyServerEnvVars applies server environment variables
func applyServerEnvVars(cfg *ServerConfig, sources map[string]ConfigSource, applied *[]string) error {
	if val := os.Getenv("MIREN_SERVER_ADDRESS"); val != "" {
		cfg.Address = val
		sources["server.address"] = SourceEnv
		*applied = append(*applied, "MIREN_SERVER_ADDRESS")
	}

	if val := os.Getenv("MIREN_SERVER_RUNNER_ADDRESS"); val != "" {
		cfg.RunnerAddress = val
		sources["server.runner_address"] = SourceEnv
		*applied = append(*applied, "MIREN_SERVER_RUNNER_ADDRESS")
	}

	if val := os.Getenv("MIREN_SERVER_DATA_PATH"); val != "" {
		cfg.DataPath = val
		sources["server.data_path"] = SourceEnv
		*applied = append(*applied, "MIREN_SERVER_DATA_PATH")
	}

	if val := os.Getenv("MIREN_SERVER_RUNNER_ID"); val != "" {
		cfg.RunnerID = val
		sources["server.runner_id"] = SourceEnv
		*applied = append(*applied, "MIREN_SERVER_RUNNER_ID")
	}

	if val := os.Getenv("MIREN_SERVER_RELEASE_PATH"); val != "" {
		cfg.ReleasePath = val
		sources["server.release_path"] = SourceEnv
		*applied = append(*applied, "MIREN_SERVER_RELEASE_PATH")
	}

	if val := os.Getenv("MIREN_SERVER_CONFIG_CLUSTER_NAME"); val != "" {
		cfg.ConfigClusterName = val
		sources["server.config_cluster_name"] = SourceEnv
		*applied = append(*applied, "MIREN_SERVER_CONFIG_CLUSTER_NAME")
	}

	if val := os.Getenv("MIREN_SERVER_SKIP_CLIENT_CONFIG"); val != "" {
		boolVal, err := strconv.ParseBool(val)
		if err != nil {
			return fmt.Errorf("invalid boolean value for MIREN_SERVER_SKIP_CLIENT_CONFIG: %s", val)
		}
		cfg.SkipClientConfig = boolVal
		sources["server.skip_client_config"] = SourceEnv
		*applied = append(*applied, "MIREN_SERVER_SKIP_CLIENT_CONFIG")
	}

	if val := os.Getenv("MIREN_SERVER_HTTP_REQUEST_TIMEOUT"); val != "" {
		intVal, err := strconv.Atoi(val)
		if err != nil {
			return fmt.Errorf("invalid integer value for MIREN_SERVER_HTTP_REQUEST_TIMEOUT: %s", val)
		}
		cfg.HTTPRequestTimeout = intVal
		sources["server.http_request_timeout"] = SourceEnv
		*applied = append(*applied, "MIREN_SERVER_HTTP_REQUEST_TIMEOUT")
	}

	return nil
}

// applyTLSEnvVars applies TLS environment variables
func applyTLSEnvVars(cfg *TLSConfig, sources map[string]ConfigSource, applied *[]string) error {
	if val := os.Getenv("MIREN_TLS_ADDITIONAL_NAMES"); val != "" {
		cfg.AdditionalNames = splitCommaSeparated(val)
		sources["tls.additional_names"] = SourceEnv
		*applied = append(*applied, "MIREN_TLS_ADDITIONAL_NAMES")
	}

	if val := os.Getenv("MIREN_TLS_ADDITIONAL_IPS"); val != "" {
		cfg.AdditionalIPs = splitCommaSeparated(val)
		sources["tls.additional_ips"] = SourceEnv
		*applied = append(*applied, "MIREN_TLS_ADDITIONAL_IPS")
	}

	if val := os.Getenv("MIREN_TLS_STANDARD_TLS"); val != "" {
		boolVal, err := strconv.ParseBool(val)
		if err != nil {
			return fmt.Errorf("invalid boolean value for MIREN_TLS_STANDARD_TLS: %s", val)
		}
		cfg.StandardTLS = boolVal
		sources["tls.standard_tls"] = SourceEnv
		*applied = append(*applied, "MIREN_TLS_STANDARD_TLS")
	}

	return nil
}

// applyEtcdEnvVars applies Etcd environment variables
func applyEtcdEnvVars(cfg *EtcdConfig, sources map[string]ConfigSource, applied *[]string) error {
	if val := os.Getenv("MIREN_ETCD_ENDPOINTS"); val != "" {
		cfg.Endpoints = splitCommaSeparated(val)
		sources["etcd.endpoints"] = SourceEnv
		*applied = append(*applied, "MIREN_ETCD_ENDPOINTS")
	}

	if val := os.Getenv("MIREN_ETCD_PREFIX"); val != "" {
		cfg.Prefix = val
		sources["etcd.prefix"] = SourceEnv
		*applied = append(*applied, "MIREN_ETCD_PREFIX")
	}

	if val := os.Getenv("MIREN_ETCD_START_EMBEDDED"); val != "" {
		boolVal, err := strconv.ParseBool(val)
		if err != nil {
			return fmt.Errorf("invalid boolean value for MIREN_ETCD_START_EMBEDDED: %s", val)
		}
		cfg.StartEmbedded = boolVal
		sources["etcd.start_embedded"] = SourceEnv
		*applied = append(*applied, "MIREN_ETCD_START_EMBEDDED")
	}

	if val := os.Getenv("MIREN_ETCD_CLIENT_PORT"); val != "" {
		intVal, err := strconv.Atoi(val)
		if err != nil {
			return fmt.Errorf("invalid integer value for MIREN_ETCD_CLIENT_PORT: %s", val)
		}
		cfg.ClientPort = intVal
		sources["etcd.client_port"] = SourceEnv
		*applied = append(*applied, "MIREN_ETCD_CLIENT_PORT")
	}

	if val := os.Getenv("MIREN_ETCD_PEER_PORT"); val != "" {
		intVal, err := strconv.Atoi(val)
		if err != nil {
			return fmt.Errorf("invalid integer value for MIREN_ETCD_PEER_PORT: %s", val)
		}
		cfg.PeerPort = intVal
		sources["etcd.peer_port"] = SourceEnv
		*applied = append(*applied, "MIREN_ETCD_PEER_PORT")
	}

	if val := os.Getenv("MIREN_ETCD_HTTP_CLIENT_PORT"); val != "" {
		intVal, err := strconv.Atoi(val)
		if err != nil {
			return fmt.Errorf("invalid integer value for MIREN_ETCD_HTTP_CLIENT_PORT: %s", val)
		}
		cfg.HTTPClientPort = intVal
		sources["etcd.http_client_port"] = SourceEnv
		*applied = append(*applied, "MIREN_ETCD_HTTP_CLIENT_PORT")
	}

	return nil
}

// applyClickHouseEnvVars applies ClickHouse environment variables
func applyClickHouseEnvVars(cfg *ClickHouseConfig, sources map[string]ConfigSource, applied *[]string) error {
	if val := os.Getenv("MIREN_CLICKHOUSE_START_EMBEDDED"); val != "" {
		boolVal, err := strconv.ParseBool(val)
		if err != nil {
			return fmt.Errorf("invalid boolean value for MIREN_CLICKHOUSE_START_EMBEDDED: %s", val)
		}
		cfg.StartEmbedded = boolVal
		sources["clickhouse.start_embedded"] = SourceEnv
		*applied = append(*applied, "MIREN_CLICKHOUSE_START_EMBEDDED")
	}

	if val := os.Getenv("MIREN_CLICKHOUSE_HTTP_PORT"); val != "" {
		intVal, err := strconv.Atoi(val)
		if err != nil {
			return fmt.Errorf("invalid integer value for MIREN_CLICKHOUSE_HTTP_PORT: %s", val)
		}
		cfg.HTTPPort = intVal
		sources["clickhouse.http_port"] = SourceEnv
		*applied = append(*applied, "MIREN_CLICKHOUSE_HTTP_PORT")
	}

	if val := os.Getenv("MIREN_CLICKHOUSE_NATIVE_PORT"); val != "" {
		intVal, err := strconv.Atoi(val)
		if err != nil {
			return fmt.Errorf("invalid integer value for MIREN_CLICKHOUSE_NATIVE_PORT: %s", val)
		}
		cfg.NativePort = intVal
		sources["clickhouse.native_port"] = SourceEnv
		*applied = append(*applied, "MIREN_CLICKHOUSE_NATIVE_PORT")
	}

	if val := os.Getenv("MIREN_CLICKHOUSE_INTERSERVER_PORT"); val != "" {
		intVal, err := strconv.Atoi(val)
		if err != nil {
			return fmt.Errorf("invalid integer value for MIREN_CLICKHOUSE_INTERSERVER_PORT: %s", val)
		}
		cfg.InterServerPort = intVal
		sources["clickhouse.interserver_port"] = SourceEnv
		*applied = append(*applied, "MIREN_CLICKHOUSE_INTERSERVER_PORT")
	}

	if val := os.Getenv("MIREN_CLICKHOUSE_ADDRESS"); val != "" {
		cfg.Address = val
		sources["clickhouse.address"] = SourceEnv
		*applied = append(*applied, "MIREN_CLICKHOUSE_ADDRESS")
	}

	return nil
}

// applyContainerdEnvVars applies Containerd environment variables
func applyContainerdEnvVars(cfg *ContainerdConfig, sources map[string]ConfigSource, applied *[]string) error {
	if val := os.Getenv("MIREN_CONTAINERD_START_EMBEDDED"); val != "" {
		boolVal, err := strconv.ParseBool(val)
		if err != nil {
			return fmt.Errorf("invalid boolean value for MIREN_CONTAINERD_START_EMBEDDED: %s", val)
		}
		cfg.StartEmbedded = boolVal
		sources["containerd.start_embedded"] = SourceEnv
		*applied = append(*applied, "MIREN_CONTAINERD_START_EMBEDDED")
	}

	if val := os.Getenv("MIREN_CONTAINERD_BINARY_PATH"); val != "" {
		cfg.BinaryPath = val
		sources["containerd.binary_path"] = SourceEnv
		*applied = append(*applied, "MIREN_CONTAINERD_BINARY_PATH")
	}

	if val := os.Getenv("MIREN_CONTAINERD_SOCKET_PATH"); val != "" {
		cfg.SocketPath = val
		sources["containerd.socket_path"] = SourceEnv
		*applied = append(*applied, "MIREN_CONTAINERD_SOCKET_PATH")
	}

	return nil
}

func splitCommaSeparated(s string) []string {
	if s == "" {
		return []string{}
	}

	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}
