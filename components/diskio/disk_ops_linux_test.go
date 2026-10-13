package diskio

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
	compute "miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/storage/storage_v1alpha"
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
		backing, err = ops.DeviceBacking("/dev/lbd1")
		require.NoError(t, err)
		require.Equal(t, "/vol/lbd/disk.img", backing)
		_, err = ops.DeviceBacking("/dev/lbd0")
		require.Error(t, err, "present device without backing identity is not confirmed absence")
		_, err = ops.LoopIdentity("/dev/loop1")
		require.ErrorIs(t, err, os.ErrNotExist, "missing node with a present kernel device is unknown")
		identity, err := ops.LoopIdentity("/dev/loop99")
		require.NoError(t, err)
		require.Nil(t, identity)
		_, err = ops.LoopIdentity("/dev/loop3")
		require.ErrorIs(t, err, syscall.ENOTTY, "unsupported status query is not absence")
		return
	}

	root := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(root, "dev"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "dev/loop3"), nil, 0600))
	for _, dir := range []string{"loop0", "loop1", "loop2/loop", "lbd0", "lbd1/lbd"} {
		require.NoError(t, os.MkdirAll(filepath.Join(root, "sys/block", dir), 0755))
	}
	require.NoError(t, os.Symlink("loop", filepath.Join(root, "sys/block/loop0/loop")))
	require.NoError(t, os.WriteFile(filepath.Join(root, "sys/block/loop2/loop/backing_file"), []byte("/vol/current/disk.img\n"), 0600))
	// The pinned module attaches its attributes in the named "lbd" group.
	// A decoy at the old path makes a wrong-path implementation fail distinctly.
	for path, value := range map[string]string{"lbd1/lbd/backing_path": "/vol/lbd/disk.img\n", "lbd1/backing_path": "/wrong/disk.img\n"} {
		require.NoError(t, os.WriteFile(filepath.Join(root, "sys/block", path), []byte(value), 0600))
	}
	// Isolate the synthetic sysfs in a child; never replace the host's sysfs.
	cmd := exec.Command(os.Args[0], "-test.run=^TestDeviceBackingInspection$", "-test.v")
	cmd.Env = append(os.Environ(), "MIREN_BACKING_INSPECTION_ROOT="+root)
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s", output)
	t.Log(string(output))
}

func TestLoopIdentitySurvivesRenameAndUnlink(t *testing.T) {
	if _, err := os.Stat("/dev/loop-control"); errors.Is(err, os.ErrNotExist) {
		t.Skip("loop-control unavailable")
	}
	dir := t.TempDir()
	image := filepath.Join(dir, "disk.img")
	f, err := os.Create(image)
	require.NoError(t, err)
	require.NoError(t, f.Truncate(8*1024*1024))
	require.NoError(t, f.Close())
	ops := NewRealDiskMountOps(slog.Default())
	device, identity, err := ops.LoopAttach(image)
	if errors.Is(err, syscall.EPERM) {
		t.Skip("loop attach capability unavailable")
	}
	require.NoError(t, err)
	t.Cleanup(func() {
		current, err := ops.LoopIdentity(device)
		require.NoError(t, err)
		if current != nil {
			require.NoError(t, ops.LoopDetach(device))
		}
	})
	file, err := ops.FileIdentity(image)
	require.NoError(t, err)
	require.Equal(t, file, identity)
	reported, err := ops.LoopIdentity(device)
	require.NoError(t, err)
	require.Equal(t, identity, reported)
	moved := filepath.Join(dir, "renamed.img")
	require.NoError(t, os.Rename(image, moved))
	reported, err = ops.LoopIdentity(device)
	require.NoError(t, err)
	require.Equal(t, identity, reported)
	require.NoError(t, os.WriteFile(image, []byte("replacement"), 0600))
	replacement, err := ops.FileIdentity(image)
	require.NoError(t, err)
	require.NotEqual(t, identity, replacement)
	require.NoError(t, os.Remove(moved))
	reported, err = ops.LoopIdentity(device)
	require.NoError(t, err)
	require.Equal(t, identity, reported, "unlinked backing remains identifiable through kernel status")

	// Rebind this exact loop number to the replacement at the original path.
	// Identical path text must not authorize stale cleanup of the new inode.
	require.NoError(t, ops.LoopDetach(device))
	loop, err := os.OpenFile(device, os.O_RDWR, 0)
	require.NoError(t, err)
	defer loop.Close()
	f, err = os.OpenFile(image, os.O_RDWR, 0)
	require.NoError(t, err)
	defer f.Close()
	require.NoError(t, f.Truncate(8*1024*1024))
	require.NoError(t, unix.IoctlLoopConfigure(int(loop.Fd()), &unix.LoopConfig{Fd: uint32(f.Fd()), Size: 4096}))
	// Do not keep an opener that would defer an erroneous LOOP_CLR_FD.
	require.NoError(t, loop.Close())
	state, err := LoadState(dir)
	require.NoError(t, err)
	state.SetMount("stale", &MountState{EntityId: "stale", DevicePath: device, BackingPath: image, BackingIdentity: identity})
	require.NoError(t, state.Save())
	controller := newTestDiskMountController(slog.Default(), dir, "node", nil, state, ops)
	mount := &storage_v1alpha.DiskMount{ID: "stale", DesiredState: storage_v1alpha.DM_WANT_UNMOUNTED, ActualState: storage_v1alpha.DM_ATTACHED}
	require.NoError(t, controller.Reconcile(t.Context(), mount, nil))
	reported, err = ops.LoopIdentity(device)
	require.NoError(t, err)
	require.Equal(t, replacement, reported, "stale cleanup cannot detach a reused device at the same backing pathname")
	require.Nil(t, state.GetMount("stale"))
}

func TestLegacyLoopDeletionRecovery(t *testing.T) {
	if _, err := os.Stat("/dev/loop-control"); errors.Is(err, os.ErrNotExist) {
		t.Skip("loop-control unavailable")
	}
	for _, deletion := range []string{"rename", "unlink", "checkpoint-failure"} {
		for _, mounted := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/mounted=%t", deletion, mounted), func(t *testing.T) {
				dir := t.TempDir()
				backingDir := filepath.Join(dir, "volumes", "original")
				require.NoError(t, os.MkdirAll(backingDir, 0755))
				image := filepath.Join(backingDir, "disk.img")
				volumeOps := NewRealDiskVolumeOps(slog.Default())
				require.NoError(t, volumeOps.CreateDiskImage(image, 8*1024*1024))
				ops := NewRealDiskMountOps(slog.Default())
				device, identity, err := ops.LoopAttach(image)
				if errors.Is(err, syscall.EPERM) {
					t.Skip("loop attach capability unavailable")
				}
				require.NoError(t, err)
				mountPath := filepath.Join(dir, "mount")
				t.Cleanup(func() {
					if ops.IsMounted(mountPath) {
						require.NoError(t, ops.Unmount(mountPath))
					}
					current, err := ops.LoopIdentity(device)
					require.NoError(t, err)
					if current != nil && *current == *identity {
						require.NoError(t, ops.LoopDetach(device))
					}
				})
				if mounted {
					require.NoError(t, ops.FormatDevice(t.Context(), device, "ext4"))
					require.NoError(t, os.Mkdir(mountPath, 0755))
					require.NoError(t, ops.Mount(device, mountPath, "ext4", false))
				}
				state, err := LoadState(dir)
				require.NoError(t, err)
				state.SetVolume("disk_volume/original", &VolumeState{EntityId: "disk_volume/original", VolumeId: "original", DiskPath: backingDir})
				state.SetMount("mount", &MountState{EntityId: "mount", VolumeId: "disk_volume/original", DevicePath: device,
					BackingPath: image, MountPath: mountPath, Mounted: mounted, CloudVolumeId: "cloud", LeaseNonce: "nonce", LeaseOwned: true})
				require.NoError(t, state.Save())
				vc := NewDiskVolumeController(slog.Default(), dir, compute.NewNodeId("node"), state, volumeOps, ops)
				actual := storage_v1alpha.DV_READY
				if deletion == "unlink" {
					actual = storage_v1alpha.DV_DELETED
				}
				volume := &storage_v1alpha.DiskVolume{ID: "disk_volume/original", NodeId: compute.NewNodeId("node").Id(), DesiredState: storage_v1alpha.DV_ABSENT, ActualState: actual}
				if deletion == "checkpoint-failure" {
					state.SetPath(filepath.Join(dir, "missing"))
					for range 2 {
						require.Error(t, vc.Reconcile(t.Context(), volume, nil))
						_, err := os.Stat(image)
						require.NoError(t, err, "retry must not rename/unlink after failed durable bootstrap")
						persisted, err := LoadState(dir)
						require.NoError(t, err)
						require.Nil(t, persisted.GetMount("mount").BackingIdentity)
					}
					state.SetPath(dir)
				}
				require.NoError(t, vc.Reconcile(t.Context(), volume, nil))
				_, err = os.Stat(image)
				require.ErrorIs(t, err, os.ErrNotExist)
				state, err = LoadState(dir)
				require.NoError(t, err)
				require.Nil(t, state.GetVolume("disk_volume/original"))
				require.Equal(t, identity, state.GetMount("mount").BackingIdentity, "bootstrap survives deletion and restart")
				controller := newTestDiskMountController(slog.Default(), dir, "node", nil, state, ops)
				cloud := &mockCloudDiskClient{}
				controller.SetCloudClient(cloud)
				mount := &storage_v1alpha.DiskMount{ID: "mount", DesiredState: storage_v1alpha.DM_WANT_UNMOUNTED, ActualState: storage_v1alpha.DM_MOUNTED}
				require.NoError(t, controller.Reconcile(t.Context(), mount, nil))
				require.False(t, ops.IsMounted(mountPath))
				current, err := ops.LoopIdentity(device)
				require.NoError(t, err)
				require.Nil(t, current)
				require.Nil(t, state.GetMount("mount"))
				require.Len(t, cloud.releaseLeaseCalls, 1)
			})
		}
	}
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
