package build

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"

	"miren.dev/runtime/api/build/build_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/tarx"
)

// stageMatchingFiles reuses the deployed version's immutable source archive.
// Hash and copy in one pass so delta uploads need neither a second archive nor
// a separately maintained manifest. The caller discards partial files on error.
func (b *Builder) stageMatchingFiles(ctx context.Context, appName, buildDir string, manifest []*build_v1alpha.FileManifestEntry) (int, []string, error) {
	needed := make([]string, 0, len(manifest))
	wanted := make(map[string]*build_v1alpha.FileManifestEntry, len(manifest))
	for _, entry := range manifest {
		needed = append(needed, entry.Path())
		wanted[entry.Path()] = entry
	}
	if len(manifest) == 0 {
		return 0, needed, nil
	}

	var app core_v1alpha.App
	if err := b.ec.Get(ctx, appName, &app); err != nil {
		if errors.Is(err, cond.ErrNotFound{}) {
			return 0, needed, nil
		}
		return 0, nil, err
	}
	if app.ActiveVersion == "" {
		return 0, needed, nil
	}
	var version core_v1alpha.AppVersion
	if err := b.ec.GetById(ctx, app.ActiveVersion, &version); err != nil {
		return 0, nil, err
	}
	if version.SourceArchive == "" {
		return 0, needed, nil
	}
	source, err := b.openSource(version.SourceArchive)
	if err != nil {
		return 0, nil, err
	}
	defer source.Close()
	gz, err := gzip.NewReader(source)
	if err != nil {
		return 0, nil, err
	}
	defer gz.Close()

	matched := make(map[string]bool)
	tr := tar.NewReader(gz)
	for {
		if err := ctx.Err(); err != nil {
			return 0, nil, err
		}
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return 0, nil, err
		}
		entry := wanted[header.Name]
		if header.Typeflag != tar.TypeReg || entry == nil || header.Size != entry.Size() {
			continue
		}
		path, err := tarx.SafeWritePath(buildDir, header.Name)
		if err != nil {
			return 0, nil, err
		}
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return 0, nil, err
		}
		f, err := os.Create(path)
		if err != nil {
			return 0, nil, err
		}
		hash := sha256.New()
		_, copyErr := io.Copy(io.MultiWriter(f, hash), tr)
		closeErr := f.Close()
		if copyErr != nil {
			return 0, nil, copyErr
		}
		if closeErr != nil {
			return 0, nil, closeErr
		}
		if hex.EncodeToString(hash.Sum(nil)) != entry.Hash() {
			if err := os.Remove(path); err != nil {
				return 0, nil, err
			}
			continue
		}
		// A mode-only change should reuse the bytes, not the old permissions.
		if err := os.Chmod(path, os.FileMode(entry.Mode())&os.ModePerm); err != nil {
			return 0, nil, err
		}
		matched[header.Name] = true
	}
	// Read through the gzip trailer to check its checksum even when tar ends
	// before the compressed stream does. Corruption invalidates all staged files.
	if _, err := io.Copy(io.Discard, gz); err != nil {
		return 0, nil, err
	}
	needed = needed[:0]
	for _, entry := range manifest {
		if !matched[entry.Path()] {
			needed = append(needed, entry.Path())
		}
	}
	return len(matched), needed, nil
}
