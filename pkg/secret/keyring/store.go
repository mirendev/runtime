package keyring

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
)

// Filename is the keyring's name under the server's data directory.
const Filename = "secrets.keyring"

// fileMode keeps the keyring readable only by the server's own user. It sits
// beside the CA private key, which is custodied the same way.
const fileMode = 0600

// storedKeyring is the on-disk form. Key material is base64 in JSON; the file's
// mode, not its encoding, is what protects it.
type storedKeyring struct {
	Version int         `json:"version"`
	Current string      `json:"current"`
	Keys    []storedKey `json:"keys"`
}

type storedKey struct {
	ID       string `json:"id"`
	Material string `json:"material"`
}

// storeVersion lets the format change later without silently misreading an old
// file.
const storeVersion = 1

// Path returns the keyring's location under a server data directory.
func Path(dataPath string) string {
	return filepath.Join(dataPath, "server", Filename)
}

// Ensure loads the cluster's keyring from the data directory, generating one on
// first use.
//
// Losing this file makes every stored secret permanently unrecoverable, so a
// keyring that exists but cannot be read is a hard error: regenerating over it
// would silently orphan every value the cluster holds. That mirrors how the CA
// refuses to regenerate over an unreadable cert.
func Ensure(log *slog.Logger, dataPath string) (*Keyring, error) {
	path := Path(dataPath)

	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		log.Info("loading existing secret keyring", "path", path)
		ring, err := decode(data)
		if err != nil {
			return nil, fmt.Errorf("failed to load secret keyring at %s: %w", path, err)
		}
		return ring, nil
	case !errors.Is(err, os.ErrNotExist):
		return nil, fmt.Errorf("failed to read secret keyring at %s: %w", path, err)
	}

	log.Info("generating new secret keyring", "path", path)

	ring, err := Generate()
	if err != nil {
		return nil, fmt.Errorf("failed to generate secret keyring: %w", err)
	}
	if err := Save(path, ring); err != nil {
		return nil, err
	}
	return ring, nil
}

// Save writes the keyring to path, replacing any existing file atomically so a
// crash mid-write cannot leave a truncated ring behind — which would be
// indistinguishable from key loss.
func Save(path string, ring *Keyring) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("failed to create keyring directory: %w", err)
	}

	data, err := encode(ring)
	if err != nil {
		return err
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), "."+Filename+".*")
	if err != nil {
		return fmt.Errorf("failed to create temporary keyring: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if err := tmp.Chmod(fileMode); err != nil {
		tmp.Close()
		return fmt.Errorf("failed to set keyring permissions: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("failed to write keyring: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("failed to sync keyring: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("failed to close keyring: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("failed to install keyring: %w", err)
	}

	// Syncing the file made its contents durable, but the rename that publishes
	// it lives in the parent directory and is not durable until that directory
	// is synced too. This matters more here than for most atomic writes: a
	// crash in the window could drop a newly added key after secrets have
	// already been sealed under it, leaving those versions permanently
	// unreadable. Unlike a lost config file, there is nothing to regenerate.
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return fmt.Errorf("failed to open keyring directory for sync: %w", err)
	}
	defer dir.Close()

	if err := dir.Sync(); err != nil {
		return fmt.Errorf("failed to sync keyring directory: %w", err)
	}
	return nil
}

func encode(ring *Keyring) ([]byte, error) {
	sk := storedKeyring{
		Version: storeVersion,
		Current: ring.CurrentID(),
	}
	for _, k := range ring.Keys() {
		sk.Keys = append(sk.Keys, storedKey{
			ID:       k.ID,
			Material: base64.StdEncoding.EncodeToString(k.Material),
		})
	}

	data, err := json.MarshalIndent(sk, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to encode keyring: %w", err)
	}
	return append(data, '\n'), nil
}

func decode(data []byte) (*Keyring, error) {
	var sk storedKeyring
	if err := json.Unmarshal(data, &sk); err != nil {
		return nil, fmt.Errorf("malformed keyring: %w", err)
	}
	if sk.Version != storeVersion {
		return nil, fmt.Errorf("keyring version %d is not supported (want %d)", sk.Version, storeVersion)
	}

	keys := make([]Key, 0, len(sk.Keys))
	for _, k := range sk.Keys {
		material, err := base64.StdEncoding.DecodeString(k.Material)
		if err != nil {
			return nil, fmt.Errorf("key %q has malformed material", k.ID)
		}
		keys = append(keys, Key{ID: k.ID, Material: material})
	}

	return New(keys, sk.Current)
}
