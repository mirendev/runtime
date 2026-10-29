package serverconfig

import (
	"os"
	"path/filepath"
	"testing"

	"miren.dev/mflags"
)

// TestLoad_EnvAppliesViaMflags exercises every type of env var that the
// serverconfig codegen wires up — string, bool, int, []string — to ensure
// the mflags env:"..." tag path produces the same Config as the deleted
// applyEnvironmentVariables() did.
func TestLoad_EnvAppliesViaMflags(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "server.toml")
	if err := os.WriteFile(configPath, []byte(`mode = "standalone"`), 0644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("MIREN_SERVER_ADDRESS", "0.0.0.0:9999")
	t.Setenv("MIREN_SERVER_SKIP_CLIENT_CONFIG", "true")
	t.Setenv("MIREN_TLS_ADDITIONAL_NAMES", "a.example.com,b.example.com")
	t.Setenv("MIREN_ETCD_CLIENT_PORT", "23790")
	t.Setenv("MIREN_LABS", "alpha,beta")
	t.Setenv("MIREN_TELEMETRY_METRICS_REMOTE_WRITE_URL", "https://metrics.example.com/write")
	t.Setenv("MIREN_TELEMETRY_METRICS_AUDIENCE", "metrics.example.com")
	t.Setenv("MIREN_TELEMETRY_TRACES_ENDPOINT", "https://traces.example.com")
	t.Setenv("MIREN_TELEMETRY_TRACES_AUDIENCE", "traces.example.com")

	cfg, err := Load(configPath, nil, nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Server.Address == nil || *cfg.Server.Address != "0.0.0.0:9999" {
		t.Errorf("Server.Address = %v, want 0.0.0.0:9999", cfg.Server.Address)
	}
	if !cfg.Server.GetSkipClientConfig() {
		t.Errorf("Server.SkipClientConfig = %v, want true", cfg.Server.SkipClientConfig)
	}
	if cfg.Etcd.ClientPort == nil || *cfg.Etcd.ClientPort != 23790 {
		t.Errorf("Etcd.ClientPort = %v, want 23790", cfg.Etcd.ClientPort)
	}
	wantNames := []string{"a.example.com", "b.example.com"}
	if len(cfg.TLS.AdditionalNames) != len(wantNames) {
		t.Errorf("TLS.AdditionalNames = %v, want %v", cfg.TLS.AdditionalNames, wantNames)
	} else {
		for i, name := range cfg.TLS.AdditionalNames {
			if name != wantNames[i] {
				t.Errorf("TLS.AdditionalNames[%d] = %s, want %s", i, name, wantNames[i])
			}
		}
	}
	wantLabs := []string{"alpha", "beta"}
	if len(cfg.Labs) != len(wantLabs) {
		t.Errorf("Labs = %v, want %v", cfg.Labs, wantLabs)
	} else {
		for i, l := range cfg.Labs {
			if l != wantLabs[i] {
				t.Errorf("Labs[%d] = %s, want %s", i, l, wantLabs[i])
			}
		}
	}
	if got := cfg.Telemetry.Metrics.GetRemoteWriteURL(); got != "https://metrics.example.com/write" {
		t.Errorf("Telemetry.Metrics.RemoteWriteURL = %q, want remote-write URL from env", got)
	}
	if got := cfg.Telemetry.Metrics.GetWorkloadIdentityAudience(); got != "metrics.example.com" {
		t.Errorf("Telemetry.Metrics.WorkloadIdentityAudience = %q, want audience from env", got)
	}
	if got := cfg.Telemetry.Traces.GetEndpoint(); got != "https://traces.example.com" {
		t.Errorf("Telemetry.Traces.Endpoint = %q, want endpoint from env", got)
	}
	if got := cfg.Telemetry.Traces.GetWorkloadIdentityAudience(); got != "traces.example.com" {
		t.Errorf("Telemetry.Traces.WorkloadIdentityAudience = %q, want audience from env", got)
	}
}

// TestLoad_EnvBeatsConfigFile confirms env still wins over a value already
// set in the TOML file (the headline precedence guarantee).
func TestLoad_EnvBeatsConfigFile(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "server.toml")
	tomlBody := `mode = "standalone"
[server]
address = "127.0.0.1:1111"
`
	if err := os.WriteFile(configPath, []byte(tomlBody), 0644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("MIREN_SERVER_ADDRESS", "0.0.0.0:2222")

	cfg, err := Load(configPath, nil, nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Server.Address == nil || *cfg.Server.Address != "0.0.0.0:2222" {
		t.Errorf("Server.Address = %v, want 0.0.0.0:2222 (env should beat TOML)", cfg.Server.Address)
	}
}

// TestLoad_CLIBeatsEnv confirms an explicit CLI flag overrides an env-derived
// value. Simulates the production path: parse args through mflags with env
// tags present, then pass the resulting flags struct into Load.
func TestLoad_CLIBeatsEnv(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "server.toml")
	if err := os.WriteFile(configPath, []byte(`mode = "standalone"`), 0644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("MIREN_SERVER_ADDRESS", "0.0.0.0:7777")

	flags := NewCLIFlags()
	fs := mflags.NewFlagSet("serverconfig")
	if err := fs.FromStruct(flags); err != nil {
		t.Fatalf("FromStruct: %v", err)
	}
	// FromStruct applied env. Now simulate a user passing --address on the CLI.
	if err := fs.Parse([]string{"--address=10.0.0.1:8888"}); err != nil {
		t.Fatalf("Parse: %v", err)
	}

	cfg, err := Load(configPath, flags, nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Server.Address == nil || *cfg.Server.Address != "10.0.0.1:8888" {
		t.Errorf("Server.Address = %v, want 10.0.0.1:8888 (CLI should beat env)", cfg.Server.Address)
	}
}

// TestLoad_DeprecatedMetricsEnv keeps the v0.15 remote-write env vars working:
// they load into the retired section and ResolveDeprecatedConfig carries them
// to [telemetry.metrics], where everything downstream reads.
func TestLoad_DeprecatedMetricsEnv(t *testing.T) {
	t.Setenv("MIREN_METRICS_REMOTE_WRITE_URL", "https://metrics.example.com/write")
	t.Setenv("MIREN_METRICS_REMOTE_WRITE_AUDIENCE", "metrics.example.com")

	configPath := filepath.Join(t.TempDir(), "server.toml")
	if err := os.WriteFile(configPath, []byte(`mode = "standalone"`), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(configPath, nil, nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := cfg.ResolveDeprecatedConfig(); err != nil {
		t.Fatalf("ResolveDeprecatedConfig: %v", err)
	}
	if got := cfg.Telemetry.Metrics.GetRemoteWriteURL(); got != "https://metrics.example.com/write" {
		t.Errorf("Telemetry.Metrics.RemoteWriteURL = %q, want deprecated env carried over", got)
	}
	if got := cfg.Telemetry.Metrics.GetWorkloadIdentityAudience(); got != "metrics.example.com" {
		t.Errorf("Telemetry.Metrics.WorkloadIdentityAudience = %q, want deprecated env carried over", got)
	}
}
