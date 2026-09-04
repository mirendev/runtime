package diskio

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
	"miren.dev/runtime/pkg/lbdmod"
)

const (
	loopCtlGetFree = 0x4C82
	loopClrFd      = 0x4C01
)

// realDiskVolumeOps implements DiskVolumeOps with real OS operations
type realDiskVolumeOps struct {
	log *slog.Logger
}

func NewRealDiskVolumeOps(log *slog.Logger) DiskVolumeOps {
	return &realDiskVolumeOps{log: log}
}

func (r *realDiskVolumeOps) CreateVolumeDir(path string) error {
	return os.MkdirAll(path, 0755)
}

func (r *realDiskVolumeOps) RemoveVolumeDir(path string) error {
	return os.RemoveAll(path)
}

func (r *realDiskVolumeOps) MoveVolumeDir(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return fmt.Errorf("failed to create parent directory for %s: %w", dst, err)
	}
	return os.Rename(src, dst)
}

func (r *realDiskVolumeOps) VolumePathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func (r *realDiskVolumeOps) CreateDiskImage(path string, sizeBytes int64) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("failed to create disk image: %w", err)
	}
	defer f.Close()

	if err := f.Truncate(sizeBytes); err != nil {
		return fmt.Errorf("failed to truncate disk image to %d bytes: %w", sizeBytes, err)
	}

	return nil
}

func (r *realDiskVolumeOps) RemoveDiskImage(path string) error {
	return os.Remove(path)
}

// realDiskMountOps implements DiskMountOps with real loop device operations
type realDiskMountOps struct {
	log *slog.Logger
}

func NewRealDiskMountOps(log *slog.Logger) DiskMountOps {
	return &realDiskMountOps{log: log}
}

func (r *realDiskMountOps) CreateDir(path string, perm os.FileMode) error {
	return os.MkdirAll(path, perm)
}

func (r *realDiskMountOps) RemoveFile(path string) error {
	return os.Remove(path)
}

func (r *realDiskMountOps) LoopAttach(imagePath string) (string, error) {
	// Open loop-control to get a free device
	ctl, err := os.OpenFile("/dev/loop-control", os.O_RDWR, 0)
	if err != nil {
		return "", fmt.Errorf("failed to open /dev/loop-control: %w", err)
	}
	defer ctl.Close()

	idx, _, errno := syscall.Syscall(syscall.SYS_IOCTL, ctl.Fd(), loopCtlGetFree, 0)
	if errno != 0 {
		return "", fmt.Errorf("LOOP_CTL_GET_FREE failed: %w", errno)
	}

	devicePath := fmt.Sprintf("/dev/loop%d", idx)

	// Ensure the device node exists (may be missing in containers)
	if _, err := os.Stat(devicePath); err != nil {
		dev := unix.Mkdev(loopMajor, uint32(idx))
		if err := unix.Mknod(devicePath, unix.S_IFBLK|0660, int(dev)); err != nil && !errors.Is(err, unix.EEXIST) {
			return "", fmt.Errorf("failed to create device node %s: %w", devicePath, err)
		}
	}

	// Open the loop device
	loopDev, err := os.OpenFile(devicePath, os.O_RDWR, 0)
	if err != nil {
		return "", fmt.Errorf("failed to open loop device %s: %w", devicePath, err)
	}
	defer loopDev.Close()

	// Open the backing file
	backingFile, err := os.OpenFile(imagePath, os.O_RDWR, 0)
	if err != nil {
		return "", fmt.Errorf("failed to open backing file %s: %w", imagePath, err)
	}
	defer backingFile.Close()

	// Use LOOP_CONFIGURE to attach with 4K sector size in a single ioctl.
	//
	// LO_FLAGS_DIRECT_IO tells the kernel to bypass the loop device's own
	// page cache and read/write the backing file via O_DIRECT. Without
	// this flag the loop device keeps an independent page cache layered
	// on top of the backing filesystem's page cache, and the two can
	// diverge across an unclean shutdown, losing writes the filesystem
	// believed had been flushed. Direct I/O also avoids the
	// double-buffer overhead. Every filesystem miren runs on (ext4,
	// xfs, btrfs) supports O_DIRECT on regular files, so this is safe;
	// if the backing file ever lives on a filesystem that doesn't, the
	// ioctl will fail loudly at attach time.
	config := unix.LoopConfig{
		Fd:   uint32(backingFile.Fd()),
		Size: 4096, // 4K logical block size
		Info: unix.LoopInfo64{
			Flags: unix.LO_FLAGS_DIRECT_IO,
		},
	}

	if err := unix.IoctlLoopConfigure(int(loopDev.Fd()), &config); err != nil {
		return "", fmt.Errorf("LOOP_CONFIGURE failed for %s: %w", devicePath, err)
	}

	r.log.Info("attached loop device",
		"device", devicePath,
		"image_path", imagePath,
		"direct_io", true,
	)

	return devicePath, nil
}

func (r *realDiskMountOps) LoopDetach(devicePath string) error {
	loopDev, err := os.OpenFile(devicePath, os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("failed to open loop device %s: %w", devicePath, err)
	}
	defer loopDev.Close()

	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, loopDev.Fd(), loopClrFd, 0)
	if errno != 0 {
		return fmt.Errorf("LOOP_CLR_FD failed for %s: %w", devicePath, errno)
	}

	return nil
}

// FindLoopByBacking walks /sys/block/loop*/loop/backing_file and returns the
// loop device path currently backing imagePath, or "" if none is attached.
//
// The kernel never deletes a stale loop device just because miren restarted,
// so finding a match here means a previous miren (or an uncleanly shut down
// container) already attached this image. Attaching it a second time would
// produce two loop devices with independent, incoherent page caches and
// corrupt the filesystem. Callers should reuse the returned device, fail
// loudly, or detach it explicitly.
func (r *realDiskMountOps) FindLoopByBacking(imagePath string) (string, error) {
	absPath, err := filepath.Abs(imagePath)
	if err != nil {
		return "", fmt.Errorf("failed to resolve absolute path for %s: %w", imagePath, err)
	}

	all, err := r.FindAllLoopBackings()
	if err != nil {
		return "", err
	}
	for dev, backing := range all {
		if backing == absPath {
			return dev, nil
		}
	}
	return "", nil
}

// FindAllLoopBackings walks /sys/block/loop*/loop/backing_file and returns
// a map of loop device path → backing file path for every loop device in
// the kernel. Devices that race with a concurrent detach are skipped.
func (r *realDiskMountOps) FindAllLoopBackings() (map[string]string, error) {
	entries, err := filepath.Glob("/sys/block/loop*/loop/backing_file")
	if err != nil {
		return nil, fmt.Errorf("failed to glob loop backing files: %w", err)
	}

	result := make(map[string]string, len(entries))
	for _, entry := range entries {
		data, err := os.ReadFile(entry)
		if err != nil {
			// Loop device may have been detached between glob and read — skip.
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, fmt.Errorf("failed to read %s: %w", entry, err)
		}

		backing := strings.TrimSpace(string(data))
		// The kernel appends " (deleted)" when the backing inode is
		// unlinked; strip it so callers can compare against a live path.
		backing = strings.TrimSuffix(backing, " (deleted)")

		// entry is /sys/block/loopN/loop/backing_file — extract loopN.
		loopName := filepath.Base(filepath.Dir(filepath.Dir(entry)))
		result["/dev/"+loopName] = backing
	}

	return result, nil
}

func (r *realDiskMountOps) LbdAttach(ctx context.Context, imagePath, logDir string) (string, error) {
	cmd := exec.CommandContext(ctx, "lbdctl", "add", "--json", imagePath, "--log-dir", logDir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("lbdctl add failed: %w\noutput: %s", err, string(output))
	}

	var result struct {
		Device string `json:"device"`
	}
	if err := json.Unmarshal(output, &result); err != nil {
		return "", fmt.Errorf("lbdctl add: failed to parse JSON output: %w\noutput: %s", err, string(output))
	}

	if result.Device == "" {
		return "", fmt.Errorf("lbdctl add returned empty device path")
	}

	// The kernel registers the block device in sysfs, but the /dev node is
	// normally created by udev. Containers whose /dev is a private tmpfs have
	// no udev, so the node never appears and the mount that follows fails with
	// ENOENT. Create it ourselves from the major:minor sysfs reports.
	if err := ensureDeviceNode(result.Device); err != nil {
		// lbdctl add already attached the device, but the caller receives
		// only an error (no path), so it cannot detach. Clean up here, or a
		// repeated failure leaks a kernel device with no state to reclaim it.
		// Give the detach an independent context so it can still complete even
		// if the caller's ctx has already been canceled.
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if derr := r.LbdDetach(cleanupCtx, result.Device); derr != nil {
			return "", fmt.Errorf("lbdctl add: %w (detach after failure also failed: %v)", err, derr)
		}
		return "", fmt.Errorf("lbdctl add: %w", err)
	}

	return result.Device, nil
}

// ensureDeviceNode makes sure the block device node at devicePath (e.g.
// /dev/lbd0) exists, creating it from /sys/block/<name>/dev when it is missing.
// This lets accelerator disks mount in environments without udev. A node that
// already exists must be the block device sysfs describes; anything else (a
// stale file, a character device, a different device number) is rejected so the
// caller never mounts the wrong thing.
func ensureDeviceNode(devicePath string) error {
	name := filepath.Base(devicePath)
	if name == "" || name == "." || name == ".." || strings.ContainsRune(name, '/') {
		return fmt.Errorf("unexpected device path %q", devicePath)
	}

	sysDev := filepath.Join("/sys/block", name, "dev")
	data, err := os.ReadFile(sysDev)
	if err != nil {
		return fmt.Errorf("read %s: %w", sysDev, err)
	}

	majStr, minStr, ok := strings.Cut(strings.TrimSpace(string(data)), ":")
	if !ok {
		return fmt.Errorf("unexpected %s contents %q", sysDev, string(data))
	}
	// ParseUint with bitSize 32 bounds the values to what unix.Mkdev accepts,
	// so the uint32 conversions below cannot overflow.
	major, err := strconv.ParseUint(majStr, 10, 32)
	if err != nil {
		return fmt.Errorf("parse major from %q: %w", string(data), err)
	}
	minor, err := strconv.ParseUint(minStr, 10, 32)
	if err != nil {
		return fmt.Errorf("parse minor from %q: %w", string(data), err)
	}
	want := unix.Mkdev(uint32(major), uint32(minor))

	// If a node is already present, make sure it is the device we expect
	// rather than a leftover file or a different device.
	if fi, err := os.Stat(devicePath); err == nil {
		return verifyBlockDevice(devicePath, fi, want)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat %s: %w", devicePath, err)
	}

	if err := unix.Mknod(devicePath, unix.S_IFBLK|0o660, int(want)); err != nil {
		// A concurrent udev (or a retry) may have created it already; make
		// sure what landed there is the right device.
		if errors.Is(err, os.ErrExist) {
			fi, serr := os.Stat(devicePath)
			if serr != nil {
				return fmt.Errorf("stat %s after mknod reported it exists: %w", devicePath, serr)
			}
			return verifyBlockDevice(devicePath, fi, want)
		}
		return fmt.Errorf("mknod %s (%d:%d): %w", devicePath, major, minor, err)
	}
	return nil
}

// verifyBlockDevice returns an error unless path is a block device whose device
// number matches want.
func verifyBlockDevice(path string, fi os.FileInfo, want uint64) error {
	if fi.Mode()&os.ModeDevice == 0 || fi.Mode()&os.ModeCharDevice != 0 {
		return fmt.Errorf("%s exists but is not a block device", path)
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("%s: unexpected stat type %T", path, fi.Sys())
	}
	if uint64(st.Rdev) != want {
		return fmt.Errorf("%s is device %d:%d, expected %d:%d", path,
			unix.Major(uint64(st.Rdev)), unix.Minor(uint64(st.Rdev)),
			unix.Major(want), unix.Minor(want))
	}
	return nil
}

func (r *realDiskMountOps) LbdDetach(ctx context.Context, devicePath string) error {
	// lbdctl remove takes the numeric device index, not a path -- it parses its
	// argument with atoi, so passing "/dev/lbd1" would remove index 0 and detach
	// the wrong volume.
	index, err := lbdDeviceIndex(devicePath)
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, "lbdctl", "remove", "--json", index)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("lbdctl remove failed: %w\noutput: %s", err, string(output))
	}
	_ = output // JSON output available but not needed for remove
	return nil
}

// lbdDeviceIndex extracts the numeric index from an lbd device path, e.g.
// "/dev/lbd7" -> "7". It errors on anything that isn't a numbered lbd device so
// a malformed path can't be turned into the wrong index.
func lbdDeviceIndex(devicePath string) (string, error) {
	name := filepath.Base(devicePath)
	// Only ever remove a device that lives at /dev/lbdN; a stray path like
	// /tmp/lbd7 must not be turned into an index that detaches a real volume.
	if devicePath != filepath.Join("/dev", name) {
		return "", fmt.Errorf("not an lbd device path: %q", devicePath)
	}
	index := strings.TrimPrefix(name, "lbd")
	if index == name || index == "" {
		return "", fmt.Errorf("not an lbd device path: %q", devicePath)
	}
	if _, err := strconv.ParseUint(index, 10, 32); err != nil {
		return "", fmt.Errorf("lbd device path %q has a non-numeric index: %w", devicePath, err)
	}
	return index, nil
}

func (r *realDiskMountOps) LbdAvailable() bool {
	return lbdmod.Available(lbdmod.Options{})
}

func (r *realDiskMountOps) Mount(device, mountPath, filesystem string, readOnly bool) error {
	filesystem = normalizeFilesystem(filesystem)
	flags := uintptr(0)
	if readOnly {
		flags |= syscall.MS_RDONLY
	}

	err := syscall.Mount(device, mountPath, filesystem, flags, "")
	if err != nil {
		return fmt.Errorf("mount %s on %s failed: %w", device, mountPath, err)
	}
	return nil
}

func (r *realDiskMountOps) Unmount(path string) error {
	err := syscall.Unmount(path, 0)
	if err != nil {
		return fmt.Errorf("unmount %s failed: %w", path, err)
	}
	return nil
}

func (r *realDiskMountOps) IsMounted(path string) bool {
	f, err := os.Open("/proc/mounts")
	if err != nil {
		return false
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) >= 2 && fields[1] == path {
			return true
		}
	}
	return false
}

func (r *realDiskMountOps) IsDeviceMounted(device string) (bool, error) {
	f, err := os.Open("/proc/mounts")
	if err != nil {
		return false, fmt.Errorf("open /proc/mounts: %w", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) >= 1 && fields[0] == device {
			return true, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return false, fmt.Errorf("scan /proc/mounts: %w", err)
	}
	return false, nil
}

func (r *realDiskMountOps) FindMounts(pathPrefix string) []ActiveMount {
	f, err := os.Open("/proc/mounts")
	if err != nil {
		return nil
	}
	defer f.Close()

	var result []ActiveMount
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) >= 2 && strings.HasPrefix(fields[1], pathPrefix) {
			result = append(result, ActiveMount{
				Device:    fields[0],
				MountPath: fields[1],
			})
		}
	}
	return result
}

func (r *realDiskMountOps) IsFormatted(ctx context.Context, device, filesystem string) (bool, error) {
	filesystem = normalizeFilesystem(filesystem)
	cmd := exec.CommandContext(ctx, "blkid", "-o", "value", "-s", "TYPE", device)
	output, err := cmd.Output()
	if err != nil {
		// blkid returns exit code 2 if no filesystem is found
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 2 {
			return false, nil
		}
		return false, fmt.Errorf("blkid probe failed: %w", err)
	}

	detectedFS := strings.TrimSpace(string(output))
	return detectedFS == filesystem, nil
}

// Fsck runs a filesystem check-and-repair on device. For ext4 we use
// `fsck.ext4 -y -f` (answer yes to all questions, force even if the
// journal looks clean). xfs uses `xfs_repair`. btrfs uses `btrfs check
// --repair`. The caller is responsible for ensuring the device is
// detached from any mountpoint before calling this.
func (r *realDiskMountOps) Fsck(ctx context.Context, device, filesystem string) error {
	filesystem = normalizeFilesystem(filesystem)
	var cmd *exec.Cmd
	switch filesystem {
	case "ext4":
		cmd = exec.CommandContext(ctx, "fsck.ext4", "-y", "-f", device)
	case "xfs":
		cmd = exec.CommandContext(ctx, "xfs_repair", device)
	case "btrfs":
		cmd = exec.CommandContext(ctx, "btrfs", "check", "--repair", device)
	default:
		return fmt.Errorf("unsupported filesystem for fsck: %s", filesystem)
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		// fsck.ext4 exits non-zero for a variety of recoverable
		// conditions (e.g. 1 = filesystem errors corrected). Treat the
		// "corrected" exit code as success so we can retry the mount.
		if filesystem == "ext4" {
			if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
				r.log.Info("fsck.ext4 corrected filesystem errors",
					"device", device,
					"output", string(output),
				)
				return nil
			}
		}
		return fmt.Errorf("fsck.%s failed: %w\noutput: %s", filesystem, err, string(output))
	}

	r.log.Info("fsck completed",
		"device", device,
		"filesystem", filesystem,
	)
	return nil
}

func (r *realDiskMountOps) FormatDevice(ctx context.Context, device, filesystem string) error {
	filesystem = normalizeFilesystem(filesystem)
	var cmd *exec.Cmd
	switch filesystem {
	case "ext4":
		cmd = exec.CommandContext(ctx, "mkfs.ext4", "-F", device)
	case "xfs":
		cmd = exec.CommandContext(ctx, "mkfs.xfs", "-f", device)
	case "btrfs":
		cmd = exec.CommandContext(ctx, "mkfs.btrfs", "-f", device)
	default:
		return fmt.Errorf("unsupported filesystem: %s", filesystem)
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("mkfs.%s failed: %w\noutput: %s", filesystem, err, string(output))
	}

	return nil
}

// normalizeFilesystem strips the "filesystem." enum prefix if present,
// returning the plain filesystem name (e.g., "ext4").
func normalizeFilesystem(fs string) string {
	return strings.TrimPrefix(fs, "filesystem.")
}

const (
	// Device numbers for loop devices
	loopControlMajor = 10  // misc device
	loopControlMinor = 237 // loop-control minor
	loopMajor        = 7   // loop block devices
)

// EnsureLoopDevices ensures /dev/loop-control and loop device nodes are available.
// In containers, /dev may be a fresh mount that doesn't include loop devices even
// when the kernel supports them. This function creates the device nodes via mknod
// if they're missing.
func EnsureLoopDevices(log *slog.Logger) error {
	if err := ensureLoopControl(log); err != nil {
		return err
	}

	// Verify the kernel actually supports loop devices by issuing an ioctl.
	idx, err := probeLoopControl()
	if err != nil {
		return fmt.Errorf("loop device support not available: %w", err)
	}

	// Ensure the device node for the free loop device exists.
	if err := ensureLoopDeviceNode(log, idx); err != nil {
		return err
	}

	log.Info("Loop devices available", "free_device_index", idx)
	return nil
}

// ensureLoopControl makes sure /dev/loop-control exists.
func ensureLoopControl(log *slog.Logger) error {
	if _, err := os.Stat("/dev/loop-control"); err == nil {
		return nil
	}

	// Try modprobe first — it may create the device node automatically.
	log.Info("Loading loop kernel module")
	if out, err := exec.Command("modprobe", "loop").CombinedOutput(); err != nil {
		log.Warn("modprobe loop failed, will try mknod", "error", err, "output", string(out))
	}

	if _, err := os.Stat("/dev/loop-control"); err == nil {
		return nil
	}

	// Create the device node directly.
	log.Info("Creating /dev/loop-control via mknod")
	dev := unix.Mkdev(loopControlMajor, loopControlMinor)
	if err := unix.Mknod("/dev/loop-control", unix.S_IFCHR|0660, int(dev)); err != nil && !errors.Is(err, unix.EEXIST) {
		return fmt.Errorf("mknod /dev/loop-control: %w", err)
	}

	return nil
}

// probeLoopControl opens /dev/loop-control and issues LOOP_CTL_GET_FREE to
// verify the kernel supports loop devices. Returns the index of a free device.
func probeLoopControl() (int, error) {
	ctl, err := os.OpenFile("/dev/loop-control", os.O_RDWR, 0)
	if err != nil {
		return 0, fmt.Errorf("failed to open /dev/loop-control: %w", err)
	}
	defer ctl.Close()

	idx, _, errno := syscall.Syscall(syscall.SYS_IOCTL, ctl.Fd(), loopCtlGetFree, 0)
	if errno != 0 {
		return 0, fmt.Errorf("LOOP_CTL_GET_FREE ioctl failed: %w", errno)
	}

	return int(idx), nil
}

// ensureLoopDeviceNode ensures /dev/loopN exists, creating it via mknod if needed.
func ensureLoopDeviceNode(log *slog.Logger, index int) error {
	path := fmt.Sprintf("/dev/loop%d", index)
	if _, err := os.Stat(path); err == nil {
		return nil
	}

	log.Info("Creating loop device node via mknod", "path", path)
	dev := unix.Mkdev(loopMajor, uint32(index))
	if err := unix.Mknod(path, unix.S_IFBLK|0660, int(dev)); err != nil && !errors.Is(err, unix.EEXIST) {
		return fmt.Errorf("mknod %s: %w", path, err)
	}

	return nil
}

// EnsureLbdDevices loads the lbd kernel module and proves it is usable.
//
// modprobe's exit code is not the test: a module can be absent, or present but
// wedged, and lbdctl can be installed on a host whose module never loaded.
// What settles it is the same probe accelerator mode itself relies on, so a
// node never selects accelerator mode it cannot serve.
func EnsureLbdDevices(log *slog.Logger) error {
	if out, err := exec.Command("modprobe", lbdmod.ModuleName).CombinedOutput(); err != nil {
		log.Debug("modprobe lbd failed", "error", err, "output", strings.TrimSpace(string(out)))
	}

	status, err := lbdmod.Probe(lbdmod.Options{})
	if err != nil {
		return err
	}
	if !status.Available() {
		return errors.New(status.Explain())
	}

	log.Info("lbd devices available", "kernel", status.Host.KernelRelease, "lbdctl", status.LbdctlPath)
	return nil
}

// LoopDeviceAvailable checks if loop devices can be used.
func LoopDeviceAvailable() bool {
	_, err := os.Stat("/dev/loop-control")
	return err == nil
}

// ensure unsafe is used (needed for ioctl alignment)
var _ = unsafe.Sizeof(0)
