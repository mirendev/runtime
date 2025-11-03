package appconfig

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	toml "github.com/pelletier/go-toml/v2"
)

type AppEnvVar struct {
	Name      string `json:"name" toml:"key"`
	Value     string `json:"value" toml:"value"`
	Generator string `json:"generator" toml:"generator"`
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
}

// ServiceConfig represents configuration for a specific service
type ServiceConfig struct {
	Command     string                    `toml:"command"`
	Image       string                    `toml:"image"`
	Concurrency *ServiceConcurrencyConfig `toml:"concurrency"`
}

type AppConfig struct {
	Name        string                    `toml:"name"`
	PostImport  string                    `toml:"post_import"`
	EnvVars     []AppEnvVar               `toml:"env"`
	Concurrency *int                      `toml:"concurrency"`
	Services    map[string]*ServiceConfig `toml:"services"`
	Build       *BuildConfig              `toml:"build"`
	Include     []string                  `toml:"include"`
}

const AppConfigPath = ".miren/app.toml"

func LoadAppConfig() (*AppConfig, error) {
	dir, err := os.Getwd()
	if err != nil {
		return nil, err
	}

	for dir != "/" {
		path := filepath.Join(dir, AppConfigPath)
		fi, err := os.Open(path)
		if err == nil {
			defer fi.Close()

			var ac AppConfig
			dec := toml.NewDecoder(fi)
			err = dec.Decode(&ac)
			if err != nil {
				return nil, err
			}

			// Validate the configuration
			if err := ac.Validate(); err != nil {
				return nil, err
			}

			return &ac, nil
		}

		dir = filepath.Dir(dir)
	}

	return nil, nil
}

func LoadAppConfigUnder(dir string) (*AppConfig, error) {
	path := filepath.Join(dir, AppConfigPath)
	fi, err := os.Open(path)
	if err == nil {
		defer fi.Close()

		var ac AppConfig
		dec := toml.NewDecoder(fi)
		err = dec.Decode(&ac)
		if err != nil {
			return nil, err
		}

		// Validate the configuration
		if err := ac.Validate(); err != nil {
			return nil, err
		}

		return &ac, nil
	}

	return nil, nil
}

func Parse(data []byte) (*AppConfig, error) {
	var ac AppConfig
	err := toml.Unmarshal(data, &ac)
	if err != nil {
		return nil, err
	}

	// Validate the configuration
	if err := ac.Validate(); err != nil {
		return nil, err
	}

	return &ac, nil
}

// Validate checks that the AppConfig has valid values
func (ac *AppConfig) Validate() error {
	// Validate service configurations
	for serviceName, svcConfig := range ac.Services {
		if svcConfig == nil || svcConfig.Concurrency == nil {
			continue
		}

		concurrency := svcConfig.Concurrency

		// Validate mode
		if concurrency.Mode != "" && concurrency.Mode != "auto" && concurrency.Mode != "fixed" {
			return fmt.Errorf("service %s: invalid concurrency mode %q, must be \"auto\" or \"fixed\"", serviceName, concurrency.Mode)
		}

		// Validate auto mode settings
		if concurrency.Mode == "auto" || concurrency.Mode == "" {
			if concurrency.RequestsPerInstance < 0 {
				return fmt.Errorf("service %s: requests_per_instance must be non-negative", serviceName)
			}
			if concurrency.ScaleDownDelay != "" {
				if _, err := time.ParseDuration(concurrency.ScaleDownDelay); err != nil {
					return fmt.Errorf("service %s: invalid scale_down_delay %q: %v", serviceName, concurrency.ScaleDownDelay, err)
				}
			}
			if concurrency.NumInstances > 0 {
				return fmt.Errorf("service %s: num_instances cannot be set in auto mode", serviceName)
			}
		}

		// Validate fixed mode settings
		if concurrency.Mode == "fixed" {
			if concurrency.NumInstances <= 0 {
				return fmt.Errorf("service %s: num_instances must be at least 1 for fixed mode", serviceName)
			}
			if concurrency.RequestsPerInstance > 0 {
				return fmt.Errorf("service %s: requests_per_instance cannot be set in fixed mode", serviceName)
			}
			if concurrency.ScaleDownDelay != "" {
				return fmt.Errorf("service %s: scale_down_delay cannot be set in fixed mode", serviceName)
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
				},
			}
		} else {
			ac.Services[serviceName] = &ServiceConfig{
				Concurrency: &ServiceConcurrencyConfig{
					Mode:         "fixed",
					NumInstances: 1,
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
