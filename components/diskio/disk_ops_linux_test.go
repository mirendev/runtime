package diskio

import (
	"errors"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeviceBackingInspection(t *testing.T) {
	if root := os.Getenv("MIREN_BACKING_INSPECTION_ROOT"); root != "" {
		if err := syscall.Chroot(root); errors.Is(err, syscall.EPERM) {
			t.Skip("chroot capability unavailable")
		} else {
			require.NoError(t, err)
		}
		require.NoError(t, os.Chdir("/"))
		ops := NewRealDiskMountOps(slog.Default())
		_, err := ops.DeviceBacking("/dev/loop0")
		require.ErrorIs(t, err, syscall.ELOOP)
		for _, device := range []string{"/dev/loop1", "/dev/lbd9"} {
			backing, err := ops.DeviceBacking(device)
			require.NoError(t, err)
			require.Empty(t, backing)
		}
		backing, err := ops.DeviceBacking("/dev/loop2")
		require.NoError(t, err)
		require.Equal(t, "/vol/current/disk.img", backing)
		_, err = ops.DeviceBacking("/dev/lbd0")
		require.Error(t, err, "present device without backing identity is not confirmed absence")
		return
	}

	root := t.TempDir()
	for _, dir := range []string{"loop0", "loop1", "loop2/loop", "lbd0"} {
		require.NoError(t, os.MkdirAll(filepath.Join(root, "sys/block", dir), 0755))
	}
	require.NoError(t, os.Symlink("loop", filepath.Join(root, "sys/block/loop0/loop")))
	require.NoError(t, os.WriteFile(filepath.Join(root, "sys/block/loop2/loop/backing_file"), []byte("/vol/current/disk.img\n"), 0600))
	// Isolate the synthetic sysfs in a child; never replace the host's sysfs.
	cmd := exec.Command(os.Args[0], "-test.run=^TestDeviceBackingInspection$", "-test.v")
	cmd.Env = append(os.Environ(), "MIREN_BACKING_INSPECTION_ROOT="+root)
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s", output)
	t.Log(string(output))
}

func TestParseLoopBacking(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want LoopBacking
	}{
		{
			name: "a live backing file",
			raw:  "/var/lib/miren/disk-data/volumes/vol-1/disk.img\n",
			want: LoopBacking{Path: "/var/lib/miren/disk-data/volumes/vol-1/disk.img"},
		},
		{
			// What the kernel reports once the file has lost its last name.
			// The path is where it was when that happened.
			name: "a backing file that has been unlinked",
			raw:  "/var/lib/miren/disk-data/volumes/vol-1/disk.img (deleted)\n",
			want: LoopBacking{Path: "/var/lib/miren/disk-data/volumes/vol-1/disk.img", Deleted: true},
		},
		{
			// A renamed file is followed to its new name with no marker: the
			// kernel tracks the inode, and the inode still has a name. Only
			// losing its last name marks it.
			name: "a backing file that was renamed rather than removed",
			raw:  "/var/lib/miren/disk-data/volumes/vol-1/renamed.img",
			want: LoopBacking{Path: "/var/lib/miren/disk-data/volumes/vol-1/renamed.img"},
		},
		{
			name: "an unlinked file whose own name ended in the marker",
			raw:  "/tmp/weird (deleted) (deleted)",
			want: LoopBacking{Path: "/tmp/weird (deleted)", Deleted: true},
		},
		{
			// Genuinely ambiguous — a live file named "weird (deleted)" reads
			// identically to a deleted file named "weird", and the kernel gives
			// no way to tell. Read as deleted, which is the safe way to be
			// wrong: it costs an extra loop device rather than stale data.
			name: "the ambiguous case is read as deleted",
			raw:  "/tmp/weird (deleted)",
			want: LoopBacking{Path: "/tmp/weird", Deleted: true},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, parseLoopBacking(tc.raw))
		})
	}
}
