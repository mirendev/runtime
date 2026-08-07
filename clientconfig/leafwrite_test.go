package clientconfig

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestGetIdentitySource(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, "clientconfig.yaml")

	cfg := NewConfig()
	cfg.sourcePath = mainPath

	// Identity that lives directly in the main config.
	cfg.SetIdentity("main-id", &IdentityConfig{Type: "keypair"})

	// Identity that lives in a leaf config file.
	cfg.SetLeafConfig("identity-leaf-id", &ConfigData{
		Identities: map[string]*IdentityConfig{
			"leaf-id": {Type: "token"},
		},
	})

	require.Equal(t, mainPath, cfg.GetIdentitySource("main-id"))
	require.Equal(t,
		filepath.Join(dir, "clientconfig.d", "identity-leaf-id.yaml"),
		cfg.GetIdentitySource("leaf-id"))
	require.Equal(t, "", cfg.GetIdentitySource("missing"))
}

// TestAtomicWriteFileNoTornRead hammers atomicWriteFile with a concurrent
// reader that unmarshals the file; a non-atomic write would occasionally yield
// a truncated document and a yaml error.
func TestAtomicWriteFileNoTornRead(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "leaf.yaml")

	// Seed a valid file so the reader always has something to read.
	small := []byte("type: token\n")
	require.NoError(t, atomicWriteFile(path, small, 0600))

	// A large payload makes a torn read far more likely under a plain WriteFile.
	big, err := yaml.Marshal(&ConfigData{
		Identities: func() map[string]*IdentityConfig {
			m := make(map[string]*IdentityConfig)
			for i := range 200 {
				m[string(rune('a'+i%26))+string(rune('0'+i%10))+"-"+itoa(i)] = &IdentityConfig{
					Type:         "token",
					Token:        "eyJhbGciOiJFZERTQSJ9." + itoa(i),
					RefreshToken: "eyJhbGciOiJFZERTQSJ9.refresh." + itoa(i),
				}
			}
			return m
		}(),
	})
	require.NoError(t, err)

	var wg sync.WaitGroup
	stop := make(chan struct{})

	// Reader goroutine.
	wg.Go(func() {
		for {
			select {
			case <-stop:
				return
			default:
			}
			data, err := os.ReadFile(path)
			if err != nil {
				continue // File momentarily absent is fine; rename is what matters.
			}
			var cd ConfigData
			require.NoError(t, yaml.Unmarshal(data, &cd), "reader observed a torn/partial write")
		}
	})

	// Writer: alternate payloads so length changes each time.
	for i := range 300 {
		payload := big
		if i%2 == 0 {
			payload = small
		}
		require.NoError(t, atomicWriteFile(path, payload, 0600))
	}
	close(stop)
	wg.Wait()

	// Final file must be valid and mode 0600.
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0600), info.Mode().Perm())
}

// itoa is a tiny helper to avoid pulling strconv into a hot loop's closure.
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}
