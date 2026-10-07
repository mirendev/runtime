package ocireg

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// BlobStore provides direct, content-addressed access to the registry's blob
// storage for server-owned artifacts that do not need the OCI upload protocol.
type BlobStore struct {
	root string
}

func NewBlobStore(dataPath string) *BlobStore {
	return &BlobStore{root: filepath.Join(dataPath, "registry", "blobs")}
}

func (s *BlobStore) Put(r io.Reader) (string, int64, error) {
	if err := os.MkdirAll(s.root, 0755); err != nil {
		return "", 0, fmt.Errorf("creating blob directory: %w", err)
	}

	tmp, err := os.CreateTemp(s.root, ".static-*")
	if err != nil {
		return "", 0, fmt.Errorf("creating temporary blob: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	hash := sha256.New()
	size, copyErr := io.Copy(io.MultiWriter(tmp, hash), r)
	closeErr := tmp.Close()
	if copyErr != nil {
		return "", 0, fmt.Errorf("writing blob: %w", copyErr)
	}
	if closeErr != nil {
		return "", 0, fmt.Errorf("closing blob: %w", closeErr)
	}

	digest := "sha256:" + hex.EncodeToString(hash.Sum(nil))
	finalPath := filepath.Join(s.root, digest)
	if err := os.Rename(tmpName, finalPath); err != nil {
		if _, statErr := os.Stat(finalPath); statErr == nil {
			return digest, size, nil
		}
		return "", 0, fmt.Errorf("committing blob: %w", err)
	}
	return digest, size, nil
}

func (s *BlobStore) Open(digest string) (*os.File, error) {
	if err := validateDigest(digest); err != nil {
		return nil, err
	}
	return os.Open(filepath.Join(s.root, digest))
}
