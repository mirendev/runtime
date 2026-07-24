package release

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SystemCLIPath is the on-$PATH location where miren maintains a symlink to the
// managed server binary. It matches the documented install location, so a CLI
// obtained by following the install docs stays in lockstep with the server that
// miren manages after every upgrade instead of silently drifting.
const SystemCLIPath = "/usr/local/bin/miren"

// ErrPathManagedElsewhere is returned by EnsurePathSymlink when linkPath is a
// symlink that some other tool clearly owns (e.g. a Homebrew Cask links its
// binaries into the Caskroom). Rather than hijack it, we leave it untouched;
// callers can surface a note that the on-$PATH CLI may not track the server.
var ErrPathManagedElsewhere = errors.New("path is a symlink managed by another tool")

// EnsurePathSymlink makes linkPath an up-to-date symlink pointing at target.
//
// It is idempotent and self-correcting: if linkPath is already the correct
// symlink it does nothing; if it is a stale regular file (e.g. a binary an
// installer copied there) or a symlink that already points into the managed
// release directory, it is atomically replaced. The swap is done by creating the
// new symlink under a temporary name in the same directory and renaming it over
// linkPath, so a miren process currently executing via linkPath keeps its
// already-mapped inode on Linux.
//
// If linkPath is a symlink pointing somewhere outside the managed release
// directory, it is assumed to belong to another package manager and is left
// alone, returning ErrPathManagedElsewhere.
func EnsurePathSymlink(target, linkPath string) error {
	// Fast path: linkPath is already the symlink we want.
	if current, err := os.Readlink(linkPath); err == nil && current == target {
		return nil
	}

	// Don't hijack a symlink another tool owns. We take over a plain file (the
	// tarball/terraform bootstrap copy), a missing path, or a symlink that
	// already points into the managed release directory. A symlink pointing
	// elsewhere is left as-is so we don't corrupt another package manager's
	// bookkeeping.
	if info, err := os.Lstat(linkPath); err == nil && info.Mode()&os.ModeSymlink != 0 {
		if !symlinkPointsInto(linkPath, filepath.Dir(target)) {
			return ErrPathManagedElsewhere
		}
	}

	linkDir := filepath.Dir(linkPath)
	if err := os.MkdirAll(linkDir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s for CLI symlink: %w", linkDir, err)
	}

	// Stage the new symlink under a unique temp name in the same directory so the
	// final swap onto linkPath is an atomic rename.
	tmp, err := os.CreateTemp(linkDir, ".miren-symlink-*")
	if err != nil {
		return fmt.Errorf("failed to create temp file for CLI symlink: %w", err)
	}
	tmpPath := tmp.Name()
	tmp.Close()
	// CreateTemp leaves a regular file behind; remove it so os.Symlink can claim
	// the path.
	if err := os.Remove(tmpPath); err != nil {
		return fmt.Errorf("failed to prepare temp path for CLI symlink: %w", err)
	}

	if err := os.Symlink(target, tmpPath); err != nil {
		return fmt.Errorf("failed to create CLI symlink: %w", err)
	}

	if err := os.Rename(tmpPath, linkPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to install CLI symlink at %s: %w", linkPath, err)
	}

	return nil
}

// symlinkPointsInto reports whether linkPath is a symlink whose target resolves
// to dir or something beneath it. Relative targets are resolved against the
// link's own directory.
func symlinkPointsInto(linkPath, dir string) bool {
	tgt, err := os.Readlink(linkPath)
	if err != nil {
		return false
	}
	if !filepath.IsAbs(tgt) {
		tgt = filepath.Join(filepath.Dir(linkPath), tgt)
	}
	tgt = filepath.Clean(tgt)
	dir = filepath.Clean(dir)
	return tgt == dir || strings.HasPrefix(tgt, dir+string(os.PathSeparator))
}
