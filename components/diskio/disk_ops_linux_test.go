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
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
	compute "miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/storage/storage_v1alpha"
	"miren.dev/runtime/pkg/entity"
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
		require.ErrorIs(t, err, os.ErrNotExist, "missing sysfs device number cannot authorize node recreation")
		identity, err := ops.LoopIdentity("/dev/loop99")
		require.NoError(t, err)
		require.Nil(t, identity)
		_, err = ops.LoopIdentity("/dev/loop3")
		require.ErrorIs(t, err, syscall.ENOTTY, "unsupported status query is not absence")
		_, err = ops.LoopIdentity("/dev/loop4")
		require.ErrorIs(t, err, syscall.EISDIR)
		_, err = ops.LoopIdentity("/dev/loop5")
		require.ErrorIs(t, err, syscall.ELOOP)
		return
	}

	root := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(root, "dev"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "dev/loop3"), nil, 0600))
	for _, dir := range []string{"loop0", "loop1", "loop2/loop", "loop4/dev", "lbd0", "lbd1/lbd"} {
		require.NoError(t, os.MkdirAll(filepath.Join(root, "sys/block", dir), 0755))
	}
	require.NoError(t, os.Symlink("loop5", filepath.Join(root, "sys/block/loop5")))
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
	require.NoError(t, os.Truncate(image, 8*1024*1024))
	reconfigureLoop(t, ops, device, identity, image)
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
	for _, deletion := range []string{"rename", "unlink", "checkpoint-failure", "device-reused"} {
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
				var otherIdentity *BackingIdentity
				if deletion == "device-reused" {
					if mounted {
						require.NoError(t, ops.Unmount(mountPath))
					}
					require.NoError(t, ops.LoopDetach(device))
					otherDir := filepath.Join(dir, "volumes", "other")
					require.NoError(t, os.MkdirAll(otherDir, 0755))
					require.NoError(t, volumeOps.CreateDiskImage(filepath.Join(otherDir, "disk.img"), 8*1024*1024))
					// Free-device allocation need not reuse this number, especially
					// when other CI packages attach loops on the same kernel.
					reconfigureLoop(t, ops, device, identity, filepath.Join(otherDir, "disk.img"))
					t.Cleanup(func() { require.NoError(t, ops.LoopDetach(device)) })
					otherIdentity, err = ops.FileIdentity(filepath.Join(otherDir, "disk.img"))
					require.NoError(t, err)
					current, err := ops.LoopIdentity(device)
					require.NoError(t, err)
					require.Equal(t, otherIdentity, current, "the original device number is now owned by another volume")
					state.SetVolume("other", &VolumeState{EntityId: "other", DiskPath: otherDir})
				}
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
				if deletion == "device-reused" {
					require.Empty(t, state.GetMount("mount").DevicePath, "disown survives destruction of the original file and restart")
					require.Equal(t, "nonce", state.LeaseNonceForCloudVolume("cloud"))
				} else {
					require.Equal(t, identity, state.GetMount("mount").BackingIdentity, "bootstrap survives deletion and restart")
				}
				controller := newTestDiskMountController(slog.Default(), dir, "node", nil, state, ops)
				cloud := &mockCloudDiskClient{}
				controller.SetCloudClient(cloud)
				mount := &storage_v1alpha.DiskMount{ID: "mount", DesiredState: storage_v1alpha.DM_WANT_UNMOUNTED, ActualState: storage_v1alpha.DM_MOUNTED}
				require.NoError(t, controller.Reconcile(t.Context(), mount, nil))
				require.False(t, ops.IsMounted(mountPath))
				if deletion == "device-reused" {
					current, err := ops.LoopIdentity(device)
					require.NoError(t, err)
					require.Equal(t, otherIdentity, current, "a reused device must survive cleanup after deletion and restart")
				} else {
					// LOOP_CLR_FD defers teardown while another opener (e.g. udev)
					// holds the device. Once detached, another test may reuse it.
					require.EventuallyWithT(t, func(c *assert.CollectT) {
						current, err := ops.LoopIdentity(device)
						assert.NoError(c, err)
						assert.NotEqual(c, identity, current, "the original backing must be detached")
					}, 5*time.Second, 10*time.Millisecond)
				}
				require.Nil(t, state.GetMount("mount"))
				require.Len(t, cloud.releaseLeaseCalls, 1)
			})
		}
	}
}

// reconfigureLoop points device at image right after the caller detached it
// from the backing identified by detached. LOOP_CLR_FD only defers teardown while anything else holds the
// device open (udev probing a freshly attached loop, say), and LOOP_CONFIGURE
// answers EBUSY until that last opener closes it. Wait it out rather than race
// it, closing our own fd between attempts so we don't extend the wait.
func reconfigureLoop(t *testing.T, ops DiskMountOps, device string, detached *BackingIdentity, image string) {
	t.Helper()
	backing, err := os.OpenFile(image, os.O_RDWR, 0)
	require.NoError(t, err)
	defer backing.Close()
	configure := func() error {
		loop, err := os.OpenFile(device, os.O_RDWR, 0)
		require.NoError(t, err)
		defer loop.Close()
		return unix.IoctlLoopConfigure(int(loop.Fd()), &unix.LoopConfig{
			Fd: uint32(backing.Fd()), Size: 4096, Info: unix.LoopInfo64{Flags: unix.LO_FLAGS_DIRECT_IO},
		})
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		err := configure()
		if !errors.Is(err, unix.EBUSY) {
			require.NoError(t, err)
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	current, err := ops.LoopIdentity(device)
	require.NoError(t, err)
	switch {
	case current == nil:
		// Teardown may have finished since the last attempt.
		require.NoError(t, configure(), "%s is unbound but LOOP_CONFIGURE still reports EBUSY", device)
	case *current == *detached:
		require.FailNow(t, fmt.Sprintf("%s never finished detaching", device))
	default:
		t.Skipf("%s was claimed by another loop user after detach", device)
	}
}

func TestLegacySwappedLoopRecovery(t *testing.T) {
	if _, err := os.Stat("/dev/loop-control"); errors.Is(err, os.ErrNotExist) {
		t.Skip("loop-control unavailable")
	}
	for _, order := range [][]string{{"a", "b"}, {"b", "a"}} {
		t.Run(order[0]+"-first", func(t *testing.T) {
			dir := t.TempDir()
			ops := NewRealDiskMountOps(slog.Default())
			state, err := LoadState(dir)
			require.NoError(t, err)
			devices := make(map[string]string)
			identities := make(map[string]*BackingIdentity)
			for _, id := range []string{"a", "b"} {
				backingDir := filepath.Join(dir, "volumes", id)
				require.NoError(t, os.MkdirAll(backingDir, 0755))
				image := filepath.Join(backingDir, "disk.img")
				require.NoError(t, NewRealDiskVolumeOps(slog.Default()).CreateDiskImage(image, 8*1024*1024))
				device, identity, err := ops.LoopAttach(image)
				if errors.Is(err, syscall.EPERM) {
					t.Skip("loop attach capability unavailable")
				}
				require.NoError(t, err)
				devices[id], identities[id] = device, identity
				mountPath := filepath.Join(dir, "mount", id)
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
				state.SetVolume(id, &VolumeState{EntityId: id, DiskPath: backingDir, Filesystem: "ext4"})
			}
			state.SetMount("a", &MountState{EntityId: "a", VolumeId: "a", DevicePath: devices["b"], MountPath: filepath.Join(dir, "mount", "a"), Mounted: true})
			state.SetMount("b", &MountState{EntityId: "b", VolumeId: "b", DevicePath: devices["a"], MountPath: filepath.Join(dir, "mount", "b"), Mounted: true})
			require.NoError(t, state.Save())
			state, err = LoadState(dir)
			require.NoError(t, err)
			controller := newTestDiskMountController(slog.Default(), dir, "node", nil, state, ops)
			for _, id := range order {
				mount := &storage_v1alpha.DiskMount{ID: entity.Id(id), VolumeId: entity.Id(id), MountPath: filepath.Join(dir, "mount", id),
					DesiredState: storage_v1alpha.DM_WANT_MOUNTED, ActualState: storage_v1alpha.DM_MOUNTED}
				require.NoError(t, controller.Reconcile(t.Context(), mount, nil))
				device, err := ops.MountDevice(mount.MountPath)
				require.NoError(t, err)
				require.Equal(t, devices[id], device)
				require.Equal(t, identities[id], state.GetMount(id).BackingIdentity)
			}
		})
	}
}

func TestLoopIdentityPrivateDeviceNode(t *testing.T) {
	if root := os.Getenv("MIREN_LOOP_NODE_ROOT"); root != "" {
		require.NoError(t, syscall.Chroot(root))
		require.NoError(t, os.Chdir("/"))
		device := os.Getenv("MIREN_LOOP_NODE_DEVICE")
		identity, err := NewRealDiskMountOps(slog.Default()).LoopIdentity(device)
		require.NoError(t, err)
		if os.Getenv("MIREN_LOOP_NODE_BOUND") == "true" {
			var expected BackingIdentity
			_, err := fmt.Sscanf(os.Getenv("MIREN_LOOP_NODE_IDENTITY"), "%d:%d", &expected.Device, &expected.Inode)
			require.NoError(t, err)
			require.Equal(t, &expected, identity)
		} else {
			require.Nil(t, identity)
		}
		info, err := os.Stat(device)
		require.NoError(t, err)
		require.NotZero(t, info.Mode()&os.ModeDevice)
		return
	}
	if _, err := os.Stat("/dev/loop-control"); errors.Is(err, os.ErrNotExist) {
		t.Skip("loop-control unavailable")
	}
	dir := t.TempDir()
	image := filepath.Join(dir, "disk.img")
	require.NoError(t, NewRealDiskVolumeOps(slog.Default()).CreateDiskImage(image, 8*1024*1024))
	info, err := os.Stat(image)
	require.NoError(t, err)
	stat := info.Sys().(*syscall.Stat_t)
	ops := NewRealDiskMountOps(slog.Default())
	device, _, err := ops.LoopAttach(image)
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
	data, err := os.ReadFile(filepath.Join("/sys/block", filepath.Base(device), "dev"))
	require.NoError(t, err)
	for _, bound := range []bool{true, false} {
		t.Run(fmt.Sprintf("bound=%t", bound), func(t *testing.T) {
			if !bound {
				require.NoError(t, ops.LoopDetach(device))
				// LOOP_CLR_FD defers teardown while another opener remains.
				require.Eventually(t, func() bool {
					current, err := ops.LoopIdentity(device)
					return err == nil && current == nil
				}, 5*time.Second, 10*time.Millisecond, "%s never finished detaching", device)
			}
			root := t.TempDir()
			require.NoError(t, os.Mkdir(filepath.Join(root, "dev"), 0755))
			sysDir := filepath.Join(root, "sys/block", filepath.Base(device))
			require.NoError(t, os.MkdirAll(sysDir, 0755))
			require.NoError(t, os.WriteFile(filepath.Join(sysDir, "dev"), data, 0600))
			// Only the child gets a private device namespace; host nodes and
			// sysfs are untouched. The recreated node targets this real loop.
			cmd := exec.Command(os.Args[0], "-test.run=^TestLoopIdentityPrivateDeviceNode$", "-test.v")
			cmd.Env = append(os.Environ(), "MIREN_LOOP_NODE_ROOT="+root, "MIREN_LOOP_NODE_DEVICE="+device,
				fmt.Sprintf("MIREN_LOOP_NODE_BOUND=%t", bound), fmt.Sprintf("MIREN_LOOP_NODE_IDENTITY=%d:%d", stat.Dev, stat.Ino))
			output, err := cmd.CombinedOutput()
			require.NoError(t, err, "%s", output)
			t.Log(string(output))
		})
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
