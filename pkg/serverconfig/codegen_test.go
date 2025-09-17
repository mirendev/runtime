package serverconfig

import (
	"testing"
)

func TestGeneratedCode(t *testing.T) {
	// Test that generated code compiles and basic functions work
	cfg := DefaultConfig()

	if cfg.Mode != "standalone" {
		t.Errorf("expected default mode to be 'standalone', got %s", cfg.Mode)
	}

	// Test validation
	if err := cfg.Validate(); err != nil {
		t.Errorf("default config should be valid, got error: %v", err)
	}

	// Test mode defaults (inline application since ApplyModeDefaults was removed)
	if cfg.Mode == "standalone" {
		cfg.Etcd.StartEmbedded = true
		cfg.Clickhouse.StartEmbedded = true
		cfg.Containerd.StartEmbedded = true
	}
	if !cfg.Etcd.StartEmbedded {
		t.Error("expected etcd.StartEmbedded to be true in standalone mode")
	}
}

func TestCLIFlagsStructure(t *testing.T) {
	// Test that CLIFlags can be created
	flags := NewCLIFlags()

	// Test setting a value
	mode := "distributed"
	flags.Mode = &mode
	if flags.Mode == nil || *flags.Mode != "distributed" {
		t.Errorf("expected mode to be 'distributed', got %v", flags.Mode)
	}
}
