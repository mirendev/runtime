package sandbox

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/pkg/namespaces"
	"github.com/containerd/containerd/v2/pkg/oci"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"

	"miren.dev/runtime/pkg/testutils"
)

func TestResolveDiskOwner(t *testing.T) {
	cases := []struct {
		name           string
		owner          string
		runUID, runGID uint32
		wantUID        uint32
		wantGID        uint32
		wantSkip       bool
		wantErr        bool
	}{
		{name: "keep opts out", owner: "keep", runUID: 2010, runGID: 2011, wantSkip: true},
		{name: "derive from non-root run user", owner: "", runUID: 2010, runGID: 2011, wantUID: 2010, wantGID: 2011},
		{name: "root run user is a no-op", owner: "", runUID: 0, runGID: 0, wantSkip: true},
		{name: "explicit uid defaults gid to uid", owner: "1000", wantUID: 1000, wantGID: 1000},
		{name: "explicit uid:gid", owner: "1000:2000", wantUID: 1000, wantGID: 2000},
		{name: "explicit owner ignores run user", owner: "1000:2000", runUID: 2010, runGID: 2011, wantUID: 1000, wantGID: 2000},
		{name: "non-numeric uid errors", owner: "app", wantErr: true},
		{name: "non-numeric gid errors", owner: "1000:staff", wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			uid, gid, skip, err := resolveDiskOwner(tc.owner, tc.runUID, tc.runGID)
			if tc.wantErr {
				r.Error(err)
				return
			}
			r.NoError(err)
			r.Equal(tc.wantSkip, skip)
			if !skip {
				r.Equal(tc.wantUID, uid)
				r.Equal(tc.wantGID, gid)
			}
		})
	}
}

func TestParseOwner(t *testing.T) {
	r := require.New(t)

	uid, gid, err := parseOwner("2010")
	r.NoError(err)
	r.Equal(uint32(2010), uid)
	r.Equal(uint32(2010), gid)

	uid, gid, err = parseOwner("2010:2011")
	r.NoError(err)
	r.Equal(uint32(2010), uid)
	r.Equal(uint32(2011), gid)

	_, _, err = parseOwner("")
	r.Error(err)

	_, _, err = parseOwner("nobody")
	r.Error(err)
}

func statOwner(t *testing.T, path string) (uint32, uint32) {
	t.Helper()
	fi, err := os.Stat(path)
	require.NoError(t, err)
	st, ok := fi.Sys().(*syscall.Stat_t)
	require.True(t, ok)
	return st.Uid, st.Gid
}

func TestChownDiskRoot(t *testing.T) {
	r := require.New(t)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	t.Run("already-correct owner is a no-op fast path", func(t *testing.T) {
		dir := t.TempDir()
		uid, gid := statOwner(t, dir)
		// Chowning to the current owner must succeed without needing privileges.
		r.NoError(chownDiskRoot(log, dir, uid, gid))
	})

	t.Run("mismatch recursively chowns", func(t *testing.T) {
		if os.Geteuid() != 0 {
			t.Skip("chowning to an arbitrary uid requires root")
		}
		dir := t.TempDir()
		sub := filepath.Join(dir, "nested")
		r.NoError(os.MkdirAll(sub, 0o750))
		f := filepath.Join(sub, "file")
		r.NoError(os.WriteFile(f, []byte("x"), 0o640))

		r.NoError(chownDiskRoot(log, dir, 2010, 2011))

		for _, p := range []string{dir, sub, f} {
			uid, gid := statOwner(t, p)
			r.Equal(uint32(2010), uid, "uid of %s", p)
			r.Equal(uint32(2011), gid, "gid of %s", p)
		}
	})
}

func TestChownTreeRootLast(t *testing.T) {
	// A tree: root/, root/a, root/b/, root/b/c
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "b"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a"), []byte("x"), 0o640))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "b", "c"), []byte("x"), 0o640))

	t.Run("chowns the root last", func(t *testing.T) {
		var order []string
		chown := func(name string, _ int, _ int) error {
			order = append(order, name)
			return nil
		}
		require.NoError(t, chownTreeRootLast(dir, 2010, 2011, chown))

		require.NotEmpty(t, order)
		require.Equal(t, dir, order[len(order)-1], "mount root must be chowned last")
		// Every descendant is chowned before the root.
		require.Len(t, order, 4)
		require.Contains(t, order[:len(order)-1], filepath.Join(dir, "a"))
		require.Contains(t, order[:len(order)-1], filepath.Join(dir, "b", "c"))
	})

	t.Run("a mid-walk failure never chowns the root", func(t *testing.T) {
		boom := filepath.Join(dir, "b", "c")
		var chowned []string
		chown := func(name string, _ int, _ int) error {
			if name == boom {
				return errFakeChown
			}
			chowned = append(chowned, name)
			return nil
		}
		err := chownTreeRootLast(dir, 2010, 2011, chown)
		require.ErrorIs(t, err, errFakeChown)
		// The root was never chowned, so chownDiskRoot's fast path stays
		// mismatched and the pass is retried on the next boot.
		require.NotContains(t, chowned, dir)
	})
}

var errFakeChown = errors.New("fake chown failure")

// TestWithDiskOwnershipResolvesRunUser drives the ownership SpecOpt through the
// real containerd spec-generation path: an image whose config.User is the NAME
// "app" (resolved via /etc/passwd to 2010:2011) makes a root-owned disk
// directory writable by that user, proving we can read the run uid straight off
// the resolved spec.Process.User rather than parsing passwd ourselves.
func TestWithDiskOwnershipResolvesRunUser(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("chowning the disk dir requires root")
	}
	r := require.New(t)

	testDeps, cleanup := testutils.NewTestDeps()
	defer cleanup()

	cc := testDeps.CC
	ii := testDeps.NewImageImporter()
	ns := ii.Namespace

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	ctx = namespaces.WithNamespace(ctx, ns)

	passwd := []byte("root:x:0:0:root:/root:/bin/sh\napp:x:2010:2011:app:/home/app:/bin/sh\n")
	o := buildGoOCIImage(t, "./testdata/sort", "/bin/tp",
		[]testImageFile{
			{Path: "etc", IsDir: true},
			{Path: "etc/passwd", Content: passwd},
		},
		func(img *ocispecs.Image) { img.Config.User = "app" },
	)
	defer o.Close()
	r.NoError(ii.ImportImage(ctx, o, "diskowner-test:latest"))

	img, err := cc.GetImage(ctx, "diskowner-test:latest")
	r.NoError(err)

	// A freshly provisioned disk directory starts out root-owned.
	diskDir := t.TempDir()
	r.NoError(os.Chown(diskDir, 0, 0))

	sc := &SandboxController{Log: testDeps.Log}

	c, err := cc.NewContainer(ctx,
		"diskowner-test",
		containerd.WithNewSnapshot("diskowner-test-snap", img),
		containerd.WithNewSpec(
			oci.WithImageConfig(img),
			sc.withDiskOwnership([]diskMount{{hostPath: diskDir, owner: ""}}),
		),
	)
	r.NoError(err)
	defer func() { _ = c.Delete(ctx, containerd.WithSnapshotCleanup) }()

	uid, gid := statOwner(t, diskDir)
	r.Equal(uint32(2010), uid, "disk dir should be owned by the resolved run user")
	r.Equal(uint32(2011), gid)
}
