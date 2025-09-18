package serverconfig

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jessevdk/go-flags"
)

func TestGeneratedCode(t *testing.T) {
	// Test that generated code compiles and basic functions work
	cfg := DefaultConfig()

	if cfg.Mode == nil || *cfg.Mode != "standalone" {
		t.Errorf("expected default mode to be 'standalone', got %v", cfg.Mode)
	}

	// Test validation
	if err := cfg.Validate(); err != nil {
		t.Errorf("default config should be valid, got error: %v", err)
	}

	// Mode-default testing is done in TestLoad_ModeDefaultsPrecedence
}

func TestCLIFlagsStructure(t *testing.T) {
	// Test that CLIFlags can be created with nil pointers
	flags := NewCLIFlags()

	// Verify all pointers start as nil (zero values)
	if flags.Mode != nil {
		t.Error("expected Mode to be nil initially")
	}
	if flags.EtcdConfigStartEmbedded != nil {
		t.Error("expected EtcdConfigStartEmbedded to be nil initially")
	}
	if flags.ServerConfigDataPath != nil {
		t.Error("expected ServerConfigDataPath to be nil initially")
	}

	// Test setting a value
	mode := "distributed"
	flags.Mode = &mode
	if flags.Mode == nil || *flags.Mode != "distributed" {
		t.Errorf("expected mode to be 'distributed', got %v", flags.Mode)
	}
}

func TestCLIFlagsParsing(t *testing.T) {
	// Test that go-flags can parse our CLI structure
	var opts CLIFlags
	parser := flags.NewParser(&opts, flags.Default)

	args := []string{
		"--mode=distributed",
		"--start-etcd",
		"--data-path=/var/lib/test",
		"--etcd-client-port=2379",
	}

	_, err := parser.ParseArgs(args)
	if err != nil {
		t.Fatalf("failed to parse flags: %v", err)
	}

	// Verify parsed values
	if opts.Mode == nil || *opts.Mode != "distributed" {
		t.Errorf("expected mode to be 'distributed', got %v", opts.Mode)
	}
	if opts.EtcdConfigStartEmbedded == nil || !*opts.EtcdConfigStartEmbedded {
		t.Error("expected --start-etcd to set EtcdConfigStartEmbedded to true")
	}
	if opts.ServerConfigDataPath == nil || *opts.ServerConfigDataPath != "/var/lib/test" {
		t.Errorf("expected data-path to be '/var/lib/test', got %v", opts.ServerConfigDataPath)
	}
	if opts.EtcdConfigClientPort == nil || *opts.EtcdConfigClientPort != 2379 {
		t.Errorf("expected etcd-client-port to be 2379, got %v", opts.EtcdConfigClientPort)
	}
}

func TestLoad_ModeDefaultsPrecedence(t *testing.T) {
	// Create a temp directory for test configs
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "server.toml")

	tests := []struct {
		name           string
		configContent  string
		envVars        map[string]string
		flags          *CLIFlags
		wantMode       string
		wantEtcdStart  bool
		wantClickStart bool
	}{
		{
			name:           "standalone mode applies defaults",
			configContent:  `mode = "standalone"`,
			wantMode:       "standalone",
			wantEtcdStart:  true,
			wantClickStart: true,
		},
		{
			name:           "distributed mode no defaults",
			configContent:  `mode = "distributed"`,
			wantMode:       "distributed",
			wantEtcdStart:  false,
			wantClickStart: false,
		},
		{
			name:          "CLI overrides mode and defaults apply",
			configContent: `mode = "distributed"`,
			flags: func() *CLIFlags {
				f := NewCLIFlags()
				mode := "standalone"
				f.Mode = &mode
				return f
			}(),
			wantMode:       "standalone",
			wantEtcdStart:  true,
			wantClickStart: true,
		},
		{
			name:          "env overrides mode defaults",
			configContent: `mode = "standalone"`,
			envVars: map[string]string{
				"MIREN_ETCD_START_EMBEDDED": "false",
				"MIREN_ETCD_ENDPOINTS":      "http://etcd:2379",
			},
			wantMode:       "standalone",
			wantEtcdStart:  false,
			wantClickStart: true, // Only etcd was overridden
		},
		{
			name:          "CLI flag overrides mode defaults",
			configContent: `mode = "standalone"`,
			flags: func() *CLIFlags {
				f := NewCLIFlags()
				startEtcd := false
				f.EtcdConfigStartEmbedded = &startEtcd
				f.EtcdConfigEndpoints = []string{"http://etcd:2379"}
				return f
			}(),
			wantMode:       "standalone",
			wantEtcdStart:  false, // CLI override
			wantClickStart: true,  // Not overridden
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Write config file
			if err := os.WriteFile(configPath, []byte(tt.configContent), 0644); err != nil {
				t.Fatalf("failed to write config: %v", err)
			}

			// Set env vars
			for k, v := range tt.envVars {
				t.Setenv(k, v)
			}

			// Load config
			cfg, err := Load(configPath, tt.flags, nil)
			if err != nil {
				t.Fatalf("Load failed: %v", err)
			}

			// Verify results
			if cfg.Mode == nil || *cfg.Mode != tt.wantMode {
				var got string
				if cfg.Mode != nil {
					got = *cfg.Mode
				}
				t.Errorf("Mode = %q, want %q", got, tt.wantMode)
			}

			// Check Etcd.StartEmbedded
			var gotEtcd bool
			if cfg.Etcd.StartEmbedded != nil {
				gotEtcd = *cfg.Etcd.StartEmbedded
			}
			if gotEtcd != tt.wantEtcdStart {
				t.Errorf("Etcd.StartEmbedded = %v, want %v", gotEtcd, tt.wantEtcdStart)
			}

			// Check Clickhouse.StartEmbedded
			var gotClick bool
			if cfg.Clickhouse.StartEmbedded != nil {
				gotClick = *cfg.Clickhouse.StartEmbedded
			}
			if gotClick != tt.wantClickStart {
				t.Errorf("Clickhouse.StartEmbedded = %v, want %v", gotClick, tt.wantClickStart)
			}
		})
	}
}

func TestEtcdEndpointsDefault(t *testing.T) {
	// Test that etcd endpoints are properly defaulted when embedded etcd is enabled
	tmpDir := t.TempDir()

	tests := []struct {
		name          string
		configContent string
		flags         *CLIFlags
		wantEndpoints []string
		wantEtcdStart *bool
	}{
		{
			name:          "standalone mode with default port",
			configContent: `mode = "standalone"`,
			wantEndpoints: []string{"http://127.0.0.1:12379"},
			wantEtcdStart: boolPtr(true),
		},
		{
			name: "standalone mode with custom port",
			configContent: `mode = "standalone"
[etcd]
client_port = 9999`,
			wantEndpoints: []string{"http://127.0.0.1:9999"},
			wantEtcdStart: boolPtr(true),
		},
		{
			name: "explicit endpoints override default",
			configContent: `mode = "standalone"
[etcd]
endpoints = ["http://custom:2379"]`,
			wantEndpoints: []string{"http://custom:2379"},
			wantEtcdStart: boolPtr(true),
		},
		{
			name: "distributed mode with no embedded etcd",
			configContent: `mode = "distributed"
[etcd]
endpoints = ["http://etcd1:2379", "http://etcd2:2379"]`,
			wantEndpoints: []string{"http://etcd1:2379", "http://etcd2:2379"},
			wantEtcdStart: nil,
		},
		{
			name:          "CLI flag overrides config for endpoints",
			configContent: `mode = "standalone"`,
			flags: &CLIFlags{
				EtcdConfigEndpoints: []string{"http://cli-etcd:2379"},
			},
			wantEndpoints: []string{"http://cli-etcd:2379"},
			wantEtcdStart: boolPtr(true),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Write config file
			configPath := filepath.Join(tmpDir, tt.name+".toml")
			if err := os.WriteFile(configPath, []byte(tt.configContent), 0644); err != nil {
				t.Fatal(err)
			}

			// Load config
			cfg, err := Load(configPath, tt.flags, nil)
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}

			// Check endpoints
			if len(cfg.Etcd.Endpoints) != len(tt.wantEndpoints) {
				t.Errorf("Etcd.Endpoints length = %d, want %d", len(cfg.Etcd.Endpoints), len(tt.wantEndpoints))
			} else {
				for i, ep := range cfg.Etcd.Endpoints {
					if ep != tt.wantEndpoints[i] {
						t.Errorf("Etcd.Endpoints[%d] = %s, want %s", i, ep, tt.wantEndpoints[i])
					}
				}
			}

			// Check StartEmbedded
			if tt.wantEtcdStart != nil {
				if cfg.Etcd.StartEmbedded == nil {
					t.Errorf("Etcd.StartEmbedded is nil, want %v", *tt.wantEtcdStart)
				} else if *cfg.Etcd.StartEmbedded != *tt.wantEtcdStart {
					t.Errorf("Etcd.StartEmbedded = %v, want %v", *cfg.Etcd.StartEmbedded, *tt.wantEtcdStart)
				}
			} else if cfg.Etcd.StartEmbedded != nil {
				t.Errorf("Etcd.StartEmbedded = %v, want nil", *cfg.Etcd.StartEmbedded)
			}
		})
	}
}
