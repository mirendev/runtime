package lbdmod

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	lbdsrc "miren.dev/runtime/third_party/lbd"
)

// SourceVersion reports the miren.dev/lbd version this binary carries.
func SourceVersion() string {
	return lbdsrc.Version()
}

// materializeSource writes the embedded module source into dir, which the
// builder container then mounts. The kernel build system writes its object
// files next to the source, so this has to be a real writable directory rather
// than a read-only mount of something we already have.
func materializeSource(dir string) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating the build directory %s: %w", dir, err)
	}

	return fs.WalkDir(lbdsrc.FS, "src", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel("src", path)
		if err != nil {
			return err
		}
		target := filepath.Join(dir, rel)

		if d.IsDir() {
			return os.MkdirAll(target, 0755)
		}

		data, err := lbdsrc.FS.ReadFile(path)
		if err != nil {
			return fmt.Errorf("reading embedded %s: %w", path, err)
		}
		if err := os.WriteFile(target, data, 0644); err != nil {
			return fmt.Errorf("writing %s: %w", target, err)
		}
		return nil
	})
}
