package runnerconfig

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const DefaultConfigPath = "/var/lib/miren/runner/config.yaml"

type Config struct {
	RunnerID           string            `yaml:"runner_id" json:"runner_id"`
	Name               string            `yaml:"name,omitempty" json:"name,omitempty"`
	CoordinatorAddress string            `yaml:"coordinator_address" json:"coordinator_address"`
	CACert             string            `yaml:"ca_cert" json:"ca_cert"`
	ClientCert         string            `yaml:"client_cert" json:"client_cert"`
	ClientKey          string            `yaml:"client_key" json:"client_key"`
	Labels             map[string]string `yaml:"labels,omitempty" json:"labels,omitempty"`
	// Network configuration for distributed runners
	EtcdEndpoints []string `yaml:"etcd_endpoints,omitempty" json:"etcd_endpoints,omitempty"`
	EtcdPrefix    string   `yaml:"etcd_prefix,omitempty" json:"etcd_prefix,omitempty"`
	DiskMode      string   `yaml:"disk_mode,omitempty" json:"disk_mode,omitempty"`
	// Observability endpoints for metrics and logs
	VictoriametricsAddress string `yaml:"victoriametrics_address,omitempty" json:"victoriametrics_address,omitempty"`
	VictorialogsAddress    string `yaml:"victorialogs_address,omitempty" json:"victorialogs_address,omitempty"`
}

func Load(path string) (*Config, error) {
	if path == "" {
		path = DefaultConfigPath
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	return &cfg, nil
}

func (c *Config) Save(path string) error {
	if path == "" {
		path = DefaultConfigPath
	}

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}

	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("encoding config: %w", err)
	}

	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("writing config file: %w", err)
	}

	return nil
}

func Exists(path string) bool {
	if path == "" {
		path = DefaultConfigPath
	}
	_, err := os.Stat(path)
	return err == nil
}
