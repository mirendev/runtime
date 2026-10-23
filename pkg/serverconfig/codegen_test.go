package serverconfig

import (
	"testing"

	"miren.dev/mflags"
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

}

func TestCLIFlagsStructure(t *testing.T) {
	// Test that CLIFlags can be created with nil pointers
	flags := NewCLIFlags()

	// Verify all pointers start as nil (zero values)
	if flags.Mode != nil {
		t.Error("expected Mode to be nil initially")
	}
	if flags.ServerConfigSkipClientConfig != nil {
		t.Error("expected ServerConfigSkipClientConfig to be nil initially")
	}
	if flags.ServerConfigDataPath != nil {
		t.Error("expected ServerConfigDataPath to be nil initially")
	}

	// Test setting a value
	mode := "standalone"
	flags.Mode = &mode
	if flags.Mode == nil || *flags.Mode != "standalone" {
		t.Errorf("expected mode to be 'standalone', got %v", flags.Mode)
	}
}

func TestCLIFlagsParsing(t *testing.T) {
	// Test that mflags can parse our CLI structure
	var opts CLIFlags
	fs := mflags.NewFlagSet("serverconfig")
	err := fs.FromStruct(&opts)
	if err != nil {
		t.Fatalf("failed to create flagset from struct: %v", err)
	}

	args := []string{
		"--mode=standalone",
		"--skip-client-config",
		"--data-path=/var/lib/test",
		"--etcd-client-port=2379",
		"--metrics-remote-write-url=https://metrics.example.com/write",
		"--metrics-remote-write-audience=metrics.example.com",
		"--telemetry-traces-endpoint=https://traces.example.com",
		"--telemetry-traces-audience=traces.example.com",
	}

	err = fs.Parse(args)
	if err != nil {
		t.Fatalf("failed to parse flags: %v", err)
	}

	// Verify parsed values
	if opts.Mode == nil || *opts.Mode != "standalone" {
		t.Errorf("expected mode to be 'standalone', got %v", opts.Mode)
	}
	if opts.ServerConfigSkipClientConfig == nil || !*opts.ServerConfigSkipClientConfig {
		t.Error("expected --skip-client-config to set ServerConfigSkipClientConfig to true")
	}
	if opts.ServerConfigDataPath == nil || *opts.ServerConfigDataPath != "/var/lib/test" {
		t.Errorf("expected data-path to be '/var/lib/test', got %v", opts.ServerConfigDataPath)
	}
	if opts.EtcdConfigClientPort == nil || *opts.EtcdConfigClientPort != 2379 {
		t.Errorf("expected etcd-client-port to be 2379, got %v", opts.EtcdConfigClientPort)
	}
	if opts.RemoteWriteConfigURL == nil || *opts.RemoteWriteConfigURL != "https://metrics.example.com/write" {
		t.Errorf("expected metrics remote-write URL to be parsed, got %v", opts.RemoteWriteConfigURL)
	}
	if opts.RemoteWriteConfigWorkloadIdentityAudience == nil || *opts.RemoteWriteConfigWorkloadIdentityAudience != "metrics.example.com" {
		t.Errorf("expected metrics remote-write audience to be parsed, got %v", opts.RemoteWriteConfigWorkloadIdentityAudience)
	}
	if opts.TelemetryTracesConfigEndpoint == nil || *opts.TelemetryTracesConfigEndpoint != "https://traces.example.com" {
		t.Errorf("expected traces endpoint to be parsed, got %v", opts.TelemetryTracesConfigEndpoint)
	}
	if opts.TelemetryTracesConfigWorkloadIdentityAudience == nil || *opts.TelemetryTracesConfigWorkloadIdentityAudience != "traces.example.com" {
		t.Errorf("expected traces audience to be parsed, got %v", opts.TelemetryTracesConfigWorkloadIdentityAudience)
	}
}
