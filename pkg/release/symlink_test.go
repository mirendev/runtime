package release

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// assertSymlinkTo fails the test unless linkPath is a symlink pointing at target
// whose contents resolve to the target file.
func assertSymlinkTo(t *testing.T, linkPath, target string, wantContent []byte) {
	t.Helper()

	got, err := os.Readlink(linkPath)
	if err != nil {
		t.Fatalf("expected %s to be a symlink: %v", linkPath, err)
	}
	if got != target {
		t.Fatalf("symlink target mismatch: got %s, want %s", got, target)
	}

	// Reading through the link should resolve to the target's contents.
	content, err := os.ReadFile(linkPath)
	if err != nil {
		t.Fatalf("failed to read through symlink: %v", err)
	}
	if string(content) != string(wantContent) {
		t.Fatalf("content through symlink mismatch: got %q, want %q", content, wantContent)
	}
}

func TestEnsurePathSymlink(t *testing.T) {
	targetContent := []byte("managed binary")

	setup := func(t *testing.T) (target, link string) {
		t.Helper()
		dir := t.TempDir()
		target = filepath.Join(dir, "release", "miren")
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			t.Fatalf("failed to create target dir: %v", err)
		}
		if err := os.WriteFile(target, targetContent, 0755); err != nil {
			t.Fatalf("failed to create target binary: %v", err)
		}
		link = filepath.Join(dir, "bin", "miren")
		return target, link
	}

	t.Run("creates when absent", func(t *testing.T) {
		target, link := setup(t)
		if err := EnsurePathSymlink(target, link); err != nil {
			t.Fatalf("EnsurePathSymlink failed: %v", err)
		}
		assertSymlinkTo(t, link, target, targetContent)
	})

	t.Run("replaces stale regular file", func(t *testing.T) {
		target, link := setup(t)
		if err := os.MkdirAll(filepath.Dir(link), 0755); err != nil {
			t.Fatalf("failed to create link dir: %v", err)
		}
		// Simulate the stale binary an installer copied onto $PATH.
		if err := os.WriteFile(link, []byte("stale binary"), 0755); err != nil {
			t.Fatalf("failed to create stale file: %v", err)
		}
		if err := EnsurePathSymlink(target, link); err != nil {
			t.Fatalf("EnsurePathSymlink failed: %v", err)
		}
		assertSymlinkTo(t, link, target, targetContent)
	})

	t.Run("refreshes stale symlink within the managed dir", func(t *testing.T) {
		target, link := setup(t)
		if err := os.MkdirAll(filepath.Dir(link), 0755); err != nil {
			t.Fatalf("failed to create link dir: %v", err)
		}
		// A symlink pointing at another file inside the managed release dir is
		// ours to repoint (e.g. left over from a previous layout).
		other := filepath.Join(filepath.Dir(target), "miren.old")
		if err := os.WriteFile(other, []byte("old binary"), 0755); err != nil {
			t.Fatalf("failed to create other target: %v", err)
		}
		if err := os.Symlink(other, link); err != nil {
			t.Fatalf("failed to create initial symlink: %v", err)
		}
		if err := EnsurePathSymlink(target, link); err != nil {
			t.Fatalf("EnsurePathSymlink failed: %v", err)
		}
		assertSymlinkTo(t, link, target, targetContent)
	})

	t.Run("leaves a foreign symlink untouched", func(t *testing.T) {
		target, link := setup(t)
		if err := os.MkdirAll(filepath.Dir(link), 0755); err != nil {
			t.Fatalf("failed to create link dir: %v", err)
		}
		// Simulate a Homebrew Cask symlink: points outside the managed release
		// dir, into a Caskroom. We must not hijack it.
		caskroom := filepath.Join(filepath.Dir(filepath.Dir(target)), "Caskroom", "miren")
		if err := os.MkdirAll(filepath.Dir(caskroom), 0755); err != nil {
			t.Fatalf("failed to create caskroom dir: %v", err)
		}
		if err := os.WriteFile(caskroom, []byte("brew binary"), 0755); err != nil {
			t.Fatalf("failed to create caskroom binary: %v", err)
		}
		if err := os.Symlink(caskroom, link); err != nil {
			t.Fatalf("failed to create foreign symlink: %v", err)
		}

		err := EnsurePathSymlink(target, link)
		if !errors.Is(err, ErrPathManagedElsewhere) {
			t.Fatalf("expected ErrPathManagedElsewhere, got %v", err)
		}
		// The foreign symlink must be left exactly as it was.
		got, err := os.Readlink(link)
		if err != nil {
			t.Fatalf("foreign symlink should still exist: %v", err)
		}
		if got != caskroom {
			t.Fatalf("foreign symlink was modified: got %s, want %s", got, caskroom)
		}
	})

	t.Run("idempotent when already correct", func(t *testing.T) {
		target, link := setup(t)
		if err := EnsurePathSymlink(target, link); err != nil {
			t.Fatalf("first EnsurePathSymlink failed: %v", err)
		}
		if err := EnsurePathSymlink(target, link); err != nil {
			t.Fatalf("second EnsurePathSymlink failed: %v", err)
		}
		assertSymlinkTo(t, link, target, targetContent)
	})
}

func TestHealPathSymlink(t *testing.T) {
	targetContent := []byte("managed binary")

	// A managed release binary and a stale plain-file launcher on $PATH.
	setup := func(t *testing.T) (install, link string) {
		t.Helper()
		dir := t.TempDir()
		install = filepath.Join(dir, "release", "miren")
		if err := os.MkdirAll(filepath.Dir(install), 0755); err != nil {
			t.Fatalf("failed to create release dir: %v", err)
		}
		if err := os.WriteFile(install, targetContent, 0755); err != nil {
			t.Fatalf("failed to create managed binary: %v", err)
		}
		link = filepath.Join(dir, "bin", "miren")
		if err := os.MkdirAll(filepath.Dir(link), 0755); err != nil {
			t.Fatalf("failed to create bin dir: %v", err)
		}
		if err := os.WriteFile(link, []byte("stale binary"), 0755); err != nil {
			t.Fatalf("failed to create stale launcher: %v", err)
		}
		return install, link
	}

	t.Run("heals a stale launcher when running as the managed binary", func(t *testing.T) {
		install, link := setup(t)
		healed, err := HealPathSymlink(install, install, link)
		if err != nil {
			t.Fatalf("HealPathSymlink failed: %v", err)
		}
		if !healed {
			t.Fatal("expected healed=true for a stale plain-file launcher")
		}
		assertSymlinkTo(t, link, install, targetContent)
	})

	t.Run("counts a binary launched through a symlink as the managed one", func(t *testing.T) {
		install, link := setup(t)
		// os.Executable reported a symlink to the release binary.
		alias := filepath.Join(filepath.Dir(install), "..", "sbin", "miren")
		if err := os.MkdirAll(filepath.Dir(alias), 0755); err != nil {
			t.Fatalf("failed to create alias dir: %v", err)
		}
		if err := os.Symlink(install, alias); err != nil {
			t.Fatalf("failed to create alias: %v", err)
		}
		healed, err := HealPathSymlink(alias, install, link)
		if err != nil {
			t.Fatalf("HealPathSymlink failed: %v", err)
		}
		if !healed {
			t.Fatal("expected healed=true when exe resolves to the managed binary")
		}
		assertSymlinkTo(t, link, install, targetContent)
	})

	t.Run("does nothing when not running as the managed binary", func(t *testing.T) {
		install, link := setup(t)
		// A dev build or a copy in $HOME must not claim the launcher.
		other := filepath.Join(filepath.Dir(filepath.Dir(install)), "dev", "miren")
		if err := os.MkdirAll(filepath.Dir(other), 0755); err != nil {
			t.Fatalf("failed to create dev dir: %v", err)
		}
		if err := os.WriteFile(other, []byte("dev binary"), 0755); err != nil {
			t.Fatalf("failed to create dev binary: %v", err)
		}
		healed, err := HealPathSymlink(other, install, link)
		if err != nil {
			t.Fatalf("HealPathSymlink failed: %v", err)
		}
		if healed {
			t.Fatal("expected healed=false for a non-managed executable")
		}
		if _, err := os.Readlink(link); err == nil {
			t.Fatal("stale launcher was replaced by a symlink; it should have been left alone")
		}
		got, err := os.ReadFile(link)
		if err != nil || string(got) != "stale binary" {
			t.Fatalf("stale launcher was modified: %q, %v", got, err)
		}
	})

	t.Run("does nothing when the managed binary is missing", func(t *testing.T) {
		install, link := setup(t)
		if err := os.Remove(install); err != nil {
			t.Fatalf("failed to remove managed binary: %v", err)
		}
		healed, err := HealPathSymlink(install, install, link)
		if err != nil {
			t.Fatalf("HealPathSymlink failed: %v", err)
		}
		if healed {
			t.Fatal("expected healed=false when the managed binary does not exist")
		}
	})

	t.Run("reports nothing to do once correct", func(t *testing.T) {
		install, link := setup(t)
		if _, err := HealPathSymlink(install, install, link); err != nil {
			t.Fatalf("first HealPathSymlink failed: %v", err)
		}
		healed, err := HealPathSymlink(install, install, link)
		if err != nil {
			t.Fatalf("second HealPathSymlink failed: %v", err)
		}
		if healed {
			t.Fatal("expected healed=false on an already-correct symlink")
		}
		assertSymlinkTo(t, link, install, targetContent)
	})

	t.Run("leaves an equivalent relative symlink alone", func(t *testing.T) {
		install, link := setup(t)
		if err := os.Remove(link); err != nil {
			t.Fatalf("failed to remove stale launcher: %v", err)
		}
		rel, err := filepath.Rel(filepath.Dir(link), install)
		if err != nil {
			t.Fatalf("failed to compute relative target: %v", err)
		}
		if err := os.Symlink(rel, link); err != nil {
			t.Fatalf("failed to create relative symlink: %v", err)
		}
		healed, err := HealPathSymlink(install, install, link)
		if err != nil {
			t.Fatalf("HealPathSymlink failed: %v", err)
		}
		if healed {
			t.Fatal("expected healed=false for a relative symlink that already resolves to the managed binary")
		}
		if got, _ := os.Readlink(link); got != rel {
			t.Fatalf("relative symlink was rewritten: got %s, want %s", got, rel)
		}
	})

	t.Run("passes through a foreign symlink refusal", func(t *testing.T) {
		install, link := setup(t)
		if err := os.Remove(link); err != nil {
			t.Fatalf("failed to remove stale launcher: %v", err)
		}
		caskroom := filepath.Join(filepath.Dir(filepath.Dir(install)), "Caskroom", "miren")
		if err := os.MkdirAll(filepath.Dir(caskroom), 0755); err != nil {
			t.Fatalf("failed to create caskroom dir: %v", err)
		}
		if err := os.WriteFile(caskroom, []byte("brew binary"), 0755); err != nil {
			t.Fatalf("failed to create caskroom binary: %v", err)
		}
		if err := os.Symlink(caskroom, link); err != nil {
			t.Fatalf("failed to create foreign symlink: %v", err)
		}
		healed, err := HealPathSymlink(install, install, link)
		if !errors.Is(err, ErrPathManagedElsewhere) {
			t.Fatalf("expected ErrPathManagedElsewhere, got %v", err)
		}
		if healed {
			t.Fatal("expected healed=false when refusing a foreign symlink")
		}
		if got, _ := os.Readlink(link); got != caskroom {
			t.Fatalf("foreign symlink was modified: got %s, want %s", got, caskroom)
		}
	})
}
