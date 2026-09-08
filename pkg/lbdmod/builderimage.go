package lbdmod

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"miren.dev/runtime/api/core/core_v1alpha"
)

// builderFS holds the toolchain image definition. It is embedded rather than
// published so the cluster builds its own copy: the image is a base plus a
// handful of packages and a script, and owning a released artifact to
// distribute that costs more than rebuilding it.
//
//go:embed all:builder
var builderFS embed.FS

// BuilderRepository is the repository half of the builder's image reference.
// The registry resolves manifests by tag alone and ignores this, so it is here
// to make the reference readable in logs and `ctr images ls`.
const BuilderRepository = "miren-system/lbd-builder"

// BuilderDockerfile is the Dockerfile's name inside the build context, which
// BuildKit's dockerfile frontend takes as its "filename" attribute.
const BuilderDockerfile = "Dockerfile"

// BuilderTag is the tag the image is pushed under.
//
// The tag carries SystemArtifactPrefix because an artifact's entity name is
// the tag it was pushed under, and that name is the only thing artifact GC has
// to tell a system image apart from a genuinely orphaned one. Without the
// prefix the toolchain image is archived within the hour and its blobs
// deleted. The rest is a content hash, so the tag moves exactly when the
// toolchain does.
func BuilderTag() string {
	return core_v1alpha.SystemArtifactPrefix + "lbd-builder-" + BuilderVersion()
}

// BuilderVersion is a content hash of the toolchain definition, used as the
// image tag. Hashing the content rather than tagging by hand means the image
// is rebuilt exactly when the Dockerfile or its build script changes, and
// never otherwise.
func BuilderVersion() string {
	sum, err := hashFS(builderFS, "builder")
	if err != nil {
		// The tree is embedded at compile time, so a walk over it cannot fail
		// for any reason a caller could act on.
		panic(fmt.Sprintf("hashing the embedded lbd builder: %v", err))
	}
	return sum
}

// BuilderImage is the full reference the coordinator pushes to and nodes pull
// from. registryHost is normally ocireg.Host.
func BuilderImage(registryHost string) string {
	return fmt.Sprintf("%s/%s:%s", registryHost, BuilderRepository, BuilderTag())
}

// MaterializeBuilder writes the toolchain definition into dir, which then
// becomes the BuildKit context. It has to reach a real directory: fsutil.NewFS
// only takes a path, and the repo has no in-memory build context.
func MaterializeBuilder(dir string) error {
	return materializeEmbed(builderFS, "builder", dir)
}

// hashFS produces a stable digest over every file under root: each path and
// its bytes, in sorted order, so the result does not depend on walk order.
func hashFS(fsys fs.FS, root string) (string, error) {
	var paths []string
	err := fs.WalkDir(fsys, root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(paths)

	h := sha256.New()
	for _, path := range paths {
		data, err := fs.ReadFile(fsys, path)
		if err != nil {
			return "", err
		}
		// Length-prefix the name so a path and its content cannot be confused
		// with a different split of the same bytes.
		fmt.Fprintf(h, "%d:%s\n", len(path), path)
		fmt.Fprintf(h, "%d:", len(data))
		h.Write(data)
	}

	// Short enough to read in an image tag, long enough not to collide.
	return hex.EncodeToString(h.Sum(nil))[:16], nil
}

// materializeEmbed writes an embedded tree rooted at root into dir, stripping
// the root prefix. Executable bits are not carried by embed.FS, so anything
// that has to run is given one.
func materializeEmbed(fsys fs.FS, root, dir string) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating %s: %w", dir, err)
	}

	return fs.WalkDir(fsys, root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dir, rel)

		if d.IsDir() {
			return os.MkdirAll(target, 0755)
		}

		data, err := fs.ReadFile(fsys, path)
		if err != nil {
			return fmt.Errorf("reading embedded %s: %w", path, err)
		}

		mode := os.FileMode(0644)
		if filepath.Ext(rel) == ".sh" {
			mode = 0755
		}
		if err := os.WriteFile(target, data, mode); err != nil {
			return fmt.Errorf("writing %s: %w", target, err)
		}
		return nil
	})
}
