package serverconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Mode != "standalone" {
		t.Errorf("expected default mode to be 'standalone', got %s", cfg.Mode)
	}

	if cfg.Server.Address != "localhost:8443" {
		t.Errorf("expected default address to be 'localhost:8443', got %s", cfg.Server.Address)
	}

	if cfg.Server.HTTPRequestTimeout != 60 {
		t.Errorf("expected default HTTP timeout to be 60, got %d", cfg.Server.HTTPRequestTimeout)
	}
}

func TestConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		modify  func(*Config)
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid config",
			modify:  func(c *Config) {},
			wantErr: false,
		},
		{
			name: "invalid mode",
			modify: func(c *Config) {
				c.Mode = "invalid"
			},
			wantErr: true,
			errMsg:  "invalid mode",
		},
		{
			name: "invalid HTTP timeout",
			modify: func(c *Config) {
				c.Server.HTTPRequestTimeout = 0
			},
			wantErr: true,
			errMsg:  "http_request_timeout must be positive",
		},
		{
			name: "invalid port",
			modify: func(c *Config) {
				c.Etcd.ClientPort = 70000
			},
			wantErr: true,
			errMsg:  "must be between 1 and 65535",
		},
		{
			name: "invalid IP",
			modify: func(c *Config) {
				c.TLS.AdditionalIPs = []string{"not-an-ip"}
			},
			wantErr: true,
			errMsg:  "invalid IP address",
		},
		{
			name: "port conflict in etcd",
			modify: func(c *Config) {
				c.Etcd.ClientPort = 12379
				c.Etcd.PeerPort = 12379
			},
			wantErr: true,
			errMsg:  "port conflict",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			tt.modify(cfg)
			err := cfg.Validate()

			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.errMsg)
				} else if tt.errMsg != "" && !contains(err.Error(), tt.errMsg) {
					t.Errorf("expected error containing %q, got %q", tt.errMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestApplyModeDefaults(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Mode = "standalone"
	cfg.ApplyModeDefaults()

	if !cfg.Etcd.StartEmbedded {
		t.Error("expected etcd.start_embedded to be true in standalone mode")
	}

	if !cfg.ClickHouse.StartEmbedded {
		t.Error("expected clickhouse.start_embedded to be true in standalone mode")
	}

	if !cfg.Containerd.StartEmbedded {
		t.Error("expected containerd.start_embedded to be true in standalone mode")
	}
}

func TestLoadConfigFile(t *testing.T) {
	// Create a temporary config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "server.toml")

	configContent := `
mode = "distributed"

[server]
address = "0.0.0.0:9443"
runner_id = "test-runner"

[etcd]
endpoints = ["http://etcd-1:2379", "http://etcd-2:2379"]
prefix = "/test"
`

	err := os.WriteFile(configPath, []byte(configContent), 0644)
	if err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	// Load the config
	sourcedCfg, err := Load(configPath, nil, nil)
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	cfg := &sourcedCfg.Config

	// Check values from file
	if cfg.Mode != "distributed" {
		t.Errorf("expected mode 'distributed', got %s", cfg.Mode)
	}

	if cfg.Server.Address != "0.0.0.0:9443" {
		t.Errorf("expected address '0.0.0.0:9443', got %s", cfg.Server.Address)
	}

	if cfg.Server.RunnerID != "test-runner" {
		t.Errorf("expected runner ID 'test-runner', got %s", cfg.Server.RunnerID)
	}

	if len(cfg.Etcd.Endpoints) != 2 {
		t.Errorf("expected 2 etcd endpoints, got %d", len(cfg.Etcd.Endpoints))
	}

	if cfg.Etcd.Prefix != "/test" {
		t.Errorf("expected prefix '/test', got %s", cfg.Etcd.Prefix)
	}

	// Check source tracking
	if sourcedCfg.Sources["mode"] != SourceFile {
		t.Errorf("expected mode source to be 'file', got %s", sourcedCfg.Sources["mode"])
	}

	if sourcedCfg.Sources["server.address"] != SourceFile {
		t.Errorf("expected server.address source to be 'file', got %s", sourcedCfg.Sources["server.address"])
	}
}

func TestEnvironmentVariableOverrides(t *testing.T) {
	// Set environment variables
	t.Setenv("MIREN_MODE", "distributed")
	t.Setenv("MIREN_SERVER_ADDRESS", "env-server:1234")
	t.Setenv("MIREN_ETCD_ENDPOINTS", "http://env-etcd:2379,http://env-etcd2:2379")

	// Load config with env vars
	sourcedCfg, err := Load("", nil, nil)
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	cfg := &sourcedCfg.Config

	// Check environment overrides
	if cfg.Mode != "distributed" {
		t.Errorf("expected mode from env 'distributed', got %s", cfg.Mode)
	}

	if cfg.Server.Address != "env-server:1234" {
		t.Errorf("expected address from env 'env-server:1234', got %s", cfg.Server.Address)
	}

	if len(cfg.Etcd.Endpoints) != 2 || cfg.Etcd.Endpoints[0] != "http://env-etcd:2379" {
		t.Errorf("expected etcd endpoints from env, got %v", cfg.Etcd.Endpoints)
	}

	// Check source tracking
	if sourcedCfg.Sources["mode"] != SourceEnv {
		t.Errorf("expected mode source to be 'environment', got %s", sourcedCfg.Sources["mode"])
	}
}

func TestPrecedence(t *testing.T) {
	// Create a config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "server.toml")

	configContent := `
mode = "distributed"

[server]
address = "file:8443"
runner_id = "file-runner"
`

	err := os.WriteFile(configPath, []byte(configContent), 0644)
	if err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	// Set environment variable (should override file)
	t.Setenv("MIREN_SERVER_ADDRESS", "env:8443")

	// CLI flags (should override everything)
	cliFlags := &CLIFlags{
		RunnerID: "cli-runner",
		SetFlags: map[string]bool{
			"runner-id": true,
		},
	}

	// Load config with all sources
	sourcedCfg, err := Load(configPath, cliFlags, nil)
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	cfg := &sourcedCfg.Config

	// Check precedence
	if cfg.Mode != "distributed" {
		t.Errorf("expected mode from file 'distributed', got %s", cfg.Mode)
	}

	if cfg.Server.Address != "env:8443" {
		t.Errorf("expected address from env 'env:8443', got %s", cfg.Server.Address)
	}

	if cfg.Server.RunnerID != "cli-runner" {
		t.Errorf("expected runner ID from CLI 'cli-runner', got %s", cfg.Server.RunnerID)
	}
}

func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}
