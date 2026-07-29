package runnerconfig

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadIgnoresLegacyNetworkBackend(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	err := os.WriteFile(configPath, []byte(`
runner_id: runner-1
coordinator_address: coordinator.example.com:8443
network_backend: vxlan
`), 0600)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := Load(configPath); err != nil {
		t.Fatalf("loading a config with the legacy network_backend field: %v", err)
	}
}
