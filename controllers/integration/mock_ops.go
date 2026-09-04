package integration

import (
	"context"
	"os"

	"miren.dev/runtime/components/diskio"
)

// mockDiskVolumeOps implements diskio.DiskVolumeOps for testing.
type mockDiskVolumeOps struct {
	createdDirs   []string
	existingPaths map[string]bool
}

func newMockDiskVolumeOps() *mockDiskVolumeOps {
	return &mockDiskVolumeOps{
		existingPaths: make(map[string]bool),
	}
}

func (m *mockDiskVolumeOps) CreateVolumeDir(path string) error {
	m.createdDirs = append(m.createdDirs, path)
	m.existingPaths[path] = true
	// Also create the real directory so migration code (os.Create) works
	return os.MkdirAll(path, 0755)
}

func (m *mockDiskVolumeOps) RemoveVolumeDir(path string) error {
	delete(m.existingPaths, path)
	return nil
}

func (m *mockDiskVolumeOps) MoveVolumeDir(src, dst string) error {
	delete(m.existingPaths, src)
	m.existingPaths[dst] = true
	return nil
}

func (m *mockDiskVolumeOps) VolumePathExists(path string) bool {
	return m.existingPaths[path]
}

func (m *mockDiskVolumeOps) CreateDiskImage(path string, _ int64) error {
	m.existingPaths[path] = true
	return nil
}

func (m *mockDiskVolumeOps) RemoveDiskImage(path string) error {
	delete(m.existingPaths, path)
	return nil
}

// Verify interface compliance
var _ diskio.DiskVolumeOps = (*mockDiskVolumeOps)(nil)

// mockDiskMountOps implements diskio.DiskMountOps for testing.
type mockDiskMountOps struct {
	existingMounts map[string]bool
	formattedDisks map[string]string // device -> filesystem
	loopDevices    map[string]string // imagePath -> devicePath
	nextLoopIndex  int
}

func newMockDiskMountOps() *mockDiskMountOps {
	return &mockDiskMountOps{
		existingMounts: make(map[string]bool),
		formattedDisks: make(map[string]string),
		loopDevices:    make(map[string]string),
		nextLoopIndex:  1,
	}
}

func (m *mockDiskMountOps) CreateDir(_ string, _ os.FileMode) error {
	return nil
}

func (m *mockDiskMountOps) RemoveFile(_ string) error {
	return nil
}

func (m *mockDiskMountOps) LoopAttach(imagePath string) (string, error) {
	devPath := "/dev/loop" + string(rune('0'+m.nextLoopIndex))
	m.nextLoopIndex++
	m.loopDevices[imagePath] = devPath
	return devPath, nil
}

func (m *mockDiskMountOps) LoopDetach(devicePath string) error {
	for img, dev := range m.loopDevices {
		if dev == devicePath {
			delete(m.loopDevices, img)
			break
		}
	}
	return nil
}

func (m *mockDiskMountOps) FindLoopByBacking(imagePath string) (string, error) {
	if dev, ok := m.loopDevices[imagePath]; ok {
		return dev, nil
	}
	return "", nil
}

func (m *mockDiskMountOps) FindAllLoopBackings() (map[string]diskio.LoopBacking, error) {
	result := make(map[string]diskio.LoopBacking, len(m.loopDevices))
	for img, dev := range m.loopDevices {
		result[dev] = diskio.LoopBacking{Path: img}
	}
	return result, nil
}

func (m *mockDiskMountOps) LbdAttach(_ context.Context, imagePath, _ string) (string, error) {
	devPath := "/dev/lbd" + string(rune('0'+m.nextLoopIndex))
	m.nextLoopIndex++
	m.loopDevices[imagePath] = devPath
	return devPath, nil
}

func (m *mockDiskMountOps) LbdDetach(_ context.Context, devicePath string) error {
	for img, dev := range m.loopDevices {
		if dev == devicePath {
			delete(m.loopDevices, img)
			break
		}
	}
	return nil
}

func (m *mockDiskMountOps) LbdAvailable() bool {
	return false
}

func (m *mockDiskMountOps) Mount(_, mountPath, _ string, _ bool) error {
	m.existingMounts[mountPath] = true
	return nil
}

func (m *mockDiskMountOps) Unmount(path string) error {
	delete(m.existingMounts, path)
	return nil
}

func (m *mockDiskMountOps) IsMounted(path string) bool {
	return m.existingMounts[path]
}

func (m *mockDiskMountOps) IsDeviceMounted(_ string) (bool, error) {
	return false, nil
}

func (m *mockDiskMountOps) IsFormatted(_ context.Context, device, _ string) (bool, error) {
	_, ok := m.formattedDisks[device]
	return ok, nil
}

func (m *mockDiskMountOps) FormatDevice(_ context.Context, device, filesystem string) error {
	m.formattedDisks[device] = filesystem
	return nil
}

func (m *mockDiskMountOps) Fsck(_ context.Context, _, _ string) error {
	return nil
}

func (m *mockDiskMountOps) FindMounts(_ string) []diskio.ActiveMount {
	return nil
}

// Verify interface compliance
var _ diskio.DiskMountOps = (*mockDiskMountOps)(nil)
