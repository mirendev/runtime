package build

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
)

// makeTar returns an io.Reader producing a gzip'd tar archive (tarx.TarFS
// always decompresses) containing the given files. Parent directories are
// emitted automatically for nested paths so the receiver doesn't need to
// pre-create them. Same shape of data BuildFromTar receives over the wire.
func makeTar(t *testing.T, files map[string]string) io.Reader {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	seenDirs := map[string]bool{}
	emitDir := func(dir string) {
		if dir == "" || dir == "." || seenDirs[dir] {
			return
		}
		seenDirs[dir] = true
		hdr := &tar.Header{
			Name:     dir + "/",
			Mode:     0o755,
			Typeflag: tar.TypeDir,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("tar header for dir %q: %v", dir, err)
		}
	}

	for name, body := range files {
		// Emit each parent directory once so nested file entries can be
		// created without ENOENT on os.Create.
		if i := strings.LastIndex(name, "/"); i > 0 {
			emitDir(name[:i])
		}
		hdr := &tar.Header{
			Name: name,
			Mode: 0o644,
			Size: int64(len(body)),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("tar header for %q: %v", name, err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatalf("tar body for %q: %v", name, err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("closing tar: %v", err)
	}
	if err := gw.Close(); err != nil {
		t.Fatalf("closing gzip: %v", err)
	}
	return &buf
}

func TestStreamRegistry_StageExtractsAndReturnsPath(t *testing.T) {
	reg := NewStreamRegistry(t.TempDir(), nil)
	reg.Register("s1", makeTar(t, map[string]string{
		"app.toml": "name = 'demo'",
		"main.go":  "package main",
	}))

	path, err := reg.Stage("s1")
	if err != nil {
		t.Fatalf("Stage: %v", err)
	}
	if path == "" {
		t.Fatal("Stage returned empty path")
	}

	if !stagedFileExists(path, "app.toml") {
		t.Errorf("expected app.toml at %s", path)
	}
	if !stagedFileExists(path, "main.go") {
		t.Errorf("expected main.go at %s", path)
	}
}

func TestStreamRegistry_StageIsIdempotent(t *testing.T) {
	reg := NewStreamRegistry(t.TempDir(), nil)
	reg.Register("s1", makeTar(t, map[string]string{"a": "x"}))

	first, err := reg.Stage("s1")
	if err != nil {
		t.Fatalf("first Stage: %v", err)
	}

	// Second call must return the same path without consuming a reader.
	// The reader is gone after the first Stage, so a non-idempotent impl
	// would either re-stage (changing path) or fail with ErrStreamUnavailable.
	second, err := reg.Stage("s1")
	if err != nil {
		t.Fatalf("second Stage: %v", err)
	}
	if first != second {
		t.Errorf("Stage paths differ: first=%s second=%s", first, second)
	}
}

func TestStreamRegistry_StageRecoveryWithoutStream(t *testing.T) {
	// Simulate: caller staged once, then "crashed" — re-staging without
	// ever calling Register again must still succeed because the path is
	// remembered in-process. (Note: cross-process recovery would require
	// persisting the path; that's a future iteration.)
	reg := NewStreamRegistry(t.TempDir(), nil)
	reg.Register("s1", makeTar(t, map[string]string{"a": "x"}))
	staged, err := reg.Stage("s1")
	if err != nil {
		t.Fatalf("initial Stage: %v", err)
	}

	recovered, err := reg.Stage("s1")
	if err != nil {
		t.Fatalf("recovery Stage: %v", err)
	}
	if recovered != staged {
		t.Errorf("recovery path mismatch: got %s want %s", recovered, staged)
	}
}

func TestStreamRegistry_StageFailsWhenStreamGoneAndUnstaged(t *testing.T) {
	reg := NewStreamRegistry(t.TempDir(), nil)
	// Never registered. This is the "crash before any stage attempt" case.
	_, err := reg.Stage("never-seen")
	if err == nil {
		t.Fatal("Stage should fail when nothing registered")
	}
	if !errors.Is(err, ErrStreamUnavailable) {
		t.Errorf("expected ErrStreamUnavailable, got %v", err)
	}
}

func TestStreamRegistry_StageFailsOnBadTar(t *testing.T) {
	reg := NewStreamRegistry(t.TempDir(), nil)
	reg.Register("s1", strings.NewReader("not a tar"))

	_, err := reg.Stage("s1")
	if err == nil {
		t.Fatal("Stage should fail on malformed tar")
	}

	// Failure must not leave the ID staged; a retry should report
	// ErrStreamUnavailable rather than a half-extracted directory.
	if _, ok := reg.StagedPath("s1"); ok {
		t.Error("StagedPath should be empty after Stage failure")
	}
	_, err = reg.Stage("s1")
	if !errors.Is(err, ErrStreamUnavailable) {
		t.Errorf("retry after Stage failure: expected ErrStreamUnavailable, got %v", err)
	}
}

func TestStreamRegistry_CleanupRemovesStagedDir(t *testing.T) {
	reg := NewStreamRegistry(t.TempDir(), nil)
	reg.Register("s1", makeTar(t, map[string]string{"app.toml": "x"}))
	path, err := reg.Stage("s1")
	if err != nil {
		t.Fatalf("Stage: %v", err)
	}

	if err := reg.Cleanup("s1"); err != nil {
		t.Fatalf("Cleanup: %v", err)
	}
	if stagedFileExists(path, "app.toml") {
		t.Errorf("expected %s to be removed", path)
	}
	if _, ok := reg.StagedPath("s1"); ok {
		t.Error("StagedPath should be empty after Cleanup")
	}
}

func TestStreamRegistry_CleanupIsIdempotent(t *testing.T) {
	reg := NewStreamRegistry(t.TempDir(), nil)
	reg.Register("s1", makeTar(t, map[string]string{"a": "x"}))
	if _, err := reg.Stage("s1"); err != nil {
		t.Fatalf("Stage: %v", err)
	}

	// First cleanup removes the dir.
	if err := reg.Cleanup("s1"); err != nil {
		t.Fatalf("first Cleanup: %v", err)
	}
	// Second cleanup is a no-op — important because both action compensation
	// and a terminal saga step might call it.
	if err := reg.Cleanup("s1"); err != nil {
		t.Fatalf("second Cleanup: %v", err)
	}
	// Cleanup on a never-staged ID is also fine.
	if err := reg.Cleanup("never-seen"); err != nil {
		t.Fatalf("cleanup of unknown id: %v", err)
	}
}

func TestStreamRegistry_RegisterAfterStageDoesNotResetState(t *testing.T) {
	// A confused caller re-registering after staging shouldn't blow away
	// the staged path. Recovery flows might re-register a reader that's
	// already been consumed; we just ignore it.
	reg := NewStreamRegistry(t.TempDir(), nil)
	reg.Register("s1", makeTar(t, map[string]string{"a": "x"}))
	first, err := reg.Stage("s1")
	if err != nil {
		t.Fatalf("Stage: %v", err)
	}

	reg.Register("s1", makeTar(t, map[string]string{"b": "y"}))
	second, err := reg.Stage("s1")
	if err != nil {
		t.Fatalf("Stage after re-register: %v", err)
	}
	if first != second {
		t.Errorf("path changed after Register-after-Stage: first=%s second=%s", first, second)
	}
}

func TestStreamRegistry_ConcurrentStreams(t *testing.T) {
	reg := NewStreamRegistry(t.TempDir(), nil)
	const n = 8
	for i := range n {
		id := streamID(i)
		reg.Register(id, makeTar(t, map[string]string{id: id}))
	}

	var wg sync.WaitGroup
	paths := make([]string, n)
	errs := make([]error, n)
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			paths[i], errs[i] = reg.Stage(streamID(i))
		}(i)
	}
	wg.Wait()

	seen := map[string]bool{}
	for i, err := range errs {
		if err != nil {
			t.Errorf("Stage %d: %v", i, err)
			continue
		}
		if seen[paths[i]] {
			t.Errorf("duplicate staged path %s for stream %d", paths[i], i)
		}
		seen[paths[i]] = true
	}
}

func streamID(i int) string {
	return "s" + string(rune('a'+i))
}
