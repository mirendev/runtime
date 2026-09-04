package diskio

import (
	"context"
	"os"
	"strings"
)

// mockDiskVolumeOps implements DiskVolumeOps for testing
type mockDirMove struct {
	src string
	dst string
}

type mockDiskVolumeOps struct {
	createdDirs   []string
	removedDirs   []string
	movedDirs     []mockDirMove
	existingPaths map[string]bool
	createdImages []mockDiskImage
	removedImages []string

	createDirErr   error
	removeDirErr   error
	moveDirErr     error
	createImageErr error
	removeImageErr error
}

type mockDiskImage struct {
	path      string
	sizeBytes int64
}

func newMockDiskVolumeOps() *mockDiskVolumeOps {
	return &mockDiskVolumeOps{
		existingPaths: make(map[string]bool),
	}
}

func (m *mockDiskVolumeOps) CreateVolumeDir(path string) error {
	if m.createDirErr != nil {
		return m.createDirErr
	}
	m.createdDirs = append(m.createdDirs, path)
	m.existingPaths[path] = true
	return nil
}

func (m *mockDiskVolumeOps) RemoveVolumeDir(path string) error {
	if m.removeDirErr != nil {
		return m.removeDirErr
	}
	m.removedDirs = append(m.removedDirs, path)
	delete(m.existingPaths, path)
	return nil
}

func (m *mockDiskVolumeOps) MoveVolumeDir(src, dst string) error {
	if m.moveDirErr != nil {
		return m.moveDirErr
	}
	m.movedDirs = append(m.movedDirs, mockDirMove{src: src, dst: dst})
	delete(m.existingPaths, src)
	m.existingPaths[dst] = true
	return nil
}

func (m *mockDiskVolumeOps) VolumePathExists(path string) bool {
	return m.existingPaths[path]
}

func (m *mockDiskVolumeOps) CreateDiskImage(path string, sizeBytes int64) error {
	if m.createImageErr != nil {
		return m.createImageErr
	}
	m.createdImages = append(m.createdImages, mockDiskImage{path: path, sizeBytes: sizeBytes})
	m.existingPaths[path] = true
	return nil
}

func (m *mockDiskVolumeOps) RemoveDiskImage(path string) error {
	if m.removeImageErr != nil {
		return m.removeImageErr
	}
	m.removedImages = append(m.removedImages, path)
	delete(m.existingPaths, path)
	return nil
}

// mockDiskMountOps implements DiskMountOps for testing
type mockDiskMountOps struct {
	createdDirs   []string
	removedFiles  []string
	attachedLoops []string
	detachedLoops []string
	attachedLbds  []mockLbdAttach
	detachedLbds  []string
	mounts        []diskMockMount
	unmounts      []string
	mountedPaths  map[string]bool
	mountDevices  map[string]string // mount path → device, for FindMounts
	formattedDevs map[string]string
	formatCalls   []diskMockFormat
	// deletedBacking marks image paths whose loop device holds an unlinked
	// inode — the kernel still reports the path, but the file there now is a
	// different file. Set it to model a ghost loop.
	deletedBacking map[string]bool

	// loopBacking maps imagePath → existing loop device, for FindLoopByBacking.
	// Tests populate this to simulate a pre-existing attachment.
	loopBacking map[string]string
	// findLoopErr, when non-nil, is returned from FindLoopByBacking and
	// FindAllLoopBackings. Used to simulate a sysfs-degraded environment.
	findLoopErr error
	// opsLog records the relative order of Unmount and LoopDetach
	// calls, so tests can assert ordering without parallel slices.
	opsLog []string

	createDirErr       error
	attachErr          error
	detachErr          error
	isDeviceMountedErr error
	lbdAttachErr       error
	lbdDetachErr       error
	lbdAvailable       bool
	mountErr           error
	unmountErr         error
	isFormattedFn      func(device, filesystem string) (bool, error)
	formatErr          error
	fsckCalls          []diskMockFsck
	fsckErr            error
	fsckFn             func(device, filesystem string) error

	nextLoopDevice string
	nextLbdDevice  string
}

type mockLbdAttach struct {
	imagePath string
	logDir    string
}

type diskMockMount struct {
	device     string
	mountPath  string
	filesystem string
	readOnly   bool
}

type diskMockFormat struct {
	device     string
	filesystem string
}

type diskMockFsck struct {
	device     string
	filesystem string
}

func newMockDiskMountOps() *mockDiskMountOps {
	return &mockDiskMountOps{
		mountedPaths:   make(map[string]bool),
		mountDevices:   make(map[string]string),
		formattedDevs:  make(map[string]string),
		nextLoopDevice: "/dev/loop0",
		nextLbdDevice:  "/dev/lbd0",
	}
}

func (m *mockDiskMountOps) CreateDir(path string, _ os.FileMode) error {
	if m.createDirErr != nil {
		return m.createDirErr
	}
	m.createdDirs = append(m.createdDirs, path)
	return nil
}

func (m *mockDiskMountOps) RemoveFile(path string) error {
	m.removedFiles = append(m.removedFiles, path)
	return nil
}

func (m *mockDiskMountOps) LoopAttach(imagePath string) (string, error) {
	if m.attachErr != nil {
		return "", m.attachErr
	}
	m.attachedLoops = append(m.attachedLoops, imagePath)
	if m.loopBacking == nil {
		m.loopBacking = make(map[string]string)
	}
	m.loopBacking[imagePath] = m.nextLoopDevice
	return m.nextLoopDevice, nil
}

func (m *mockDiskMountOps) LoopDetach(devicePath string) error {
	if m.detachErr != nil {
		return m.detachErr
	}
	m.detachedLoops = append(m.detachedLoops, devicePath)
	m.opsLog = append(m.opsLog, "LoopDetach:"+devicePath)
	for imagePath, dev := range m.loopBacking {
		if dev == devicePath {
			delete(m.loopBacking, imagePath)
		}
	}
	return nil
}

func (m *mockDiskMountOps) FindLoopByBacking(imagePath string) (string, error) {
	if m.findLoopErr != nil {
		return "", m.findLoopErr
	}
	if m.loopBacking == nil {
		return "", nil
	}
	// A device holding an unlinked inode is not this image, so it is not a
	// match — the same rule the real implementation follows.
	if m.deletedBacking[imagePath] {
		return "", nil
	}
	return m.loopBacking[imagePath], nil
}

func (m *mockDiskMountOps) FindAllLoopBackings() (map[string]LoopBacking, error) {
	if m.findLoopErr != nil {
		return nil, m.findLoopErr
	}
	result := make(map[string]LoopBacking, len(m.loopBacking))
	for imagePath, dev := range m.loopBacking {
		result[dev] = LoopBacking{Path: imagePath, Deleted: m.deletedBacking[imagePath]}
	}
	return result, nil
}

func (m *mockDiskMountOps) LbdAttach(_ context.Context, imagePath, logDir string) (string, error) {
	if m.lbdAttachErr != nil {
		return "", m.lbdAttachErr
	}
	m.attachedLbds = append(m.attachedLbds, mockLbdAttach{imagePath: imagePath, logDir: logDir})
	return m.nextLbdDevice, nil
}

func (m *mockDiskMountOps) LbdDetach(_ context.Context, devicePath string) error {
	if m.lbdDetachErr != nil {
		return m.lbdDetachErr
	}
	m.detachedLbds = append(m.detachedLbds, devicePath)
	return nil
}

func (m *mockDiskMountOps) LbdAvailable() bool {
	return m.lbdAvailable
}

func (m *mockDiskMountOps) Mount(device, mountPath, filesystem string, readOnly bool) error {
	if m.mountErr != nil {
		return m.mountErr
	}
	m.mounts = append(m.mounts, diskMockMount{
		device:     device,
		mountPath:  mountPath,
		filesystem: filesystem,
		readOnly:   readOnly,
	})
	m.mountedPaths[mountPath] = true
	return nil
}

func (m *mockDiskMountOps) Unmount(path string) error {
	if m.unmountErr != nil {
		return m.unmountErr
	}
	m.unmounts = append(m.unmounts, path)
	m.opsLog = append(m.opsLog, "Unmount:"+path)
	delete(m.mountedPaths, path)
	return nil
}

func (m *mockDiskMountOps) IsMounted(path string) bool {
	return m.mountedPaths[path]
}

func (m *mockDiskMountOps) IsDeviceMounted(device string) (bool, error) {
	if m.isDeviceMountedErr != nil {
		return false, m.isDeviceMountedErr
	}
	for path, mounted := range m.mountedPaths {
		if !mounted {
			continue
		}
		if m.mountDevices[path] == device {
			return true, nil
		}
	}
	return false, nil
}

func (m *mockDiskMountOps) FindMounts(pathPrefix string) []ActiveMount {
	var result []ActiveMount
	for path := range m.mountedPaths {
		if strings.HasPrefix(path, pathPrefix) {
			result = append(result, ActiveMount{
				Device:    m.mountDevices[path],
				MountPath: path,
			})
		}
	}
	return result
}

func (m *mockDiskMountOps) IsFormatted(_ context.Context, device, filesystem string) (bool, error) {
	if m.isFormattedFn != nil {
		return m.isFormattedFn(device, filesystem)
	}
	if fs, ok := m.formattedDevs[device]; ok {
		return fs == filesystem, nil
	}
	return false, nil
}

func (m *mockDiskMountOps) FormatDevice(_ context.Context, device, filesystem string) error {
	if m.formatErr != nil {
		return m.formatErr
	}
	m.formatCalls = append(m.formatCalls, diskMockFormat{device: device, filesystem: filesystem})
	m.formattedDevs[device] = filesystem
	return nil
}

func (m *mockDiskMountOps) Fsck(_ context.Context, device, filesystem string) error {
	m.fsckCalls = append(m.fsckCalls, diskMockFsck{device: device, filesystem: filesystem})
	if m.fsckFn != nil {
		return m.fsckFn(device, filesystem)
	}
	return m.fsckErr
}
