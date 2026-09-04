package diskio

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
)

type stubDiskVolumeOps struct{}

func NewRealDiskVolumeOps(_ *slog.Logger) DiskVolumeOps {
	return &stubDiskVolumeOps{}
}

func (s *stubDiskVolumeOps) CreateVolumeDir(path string) error {
	return os.MkdirAll(path, 0755)
}

func (s *stubDiskVolumeOps) RemoveVolumeDir(path string) error {
	return os.RemoveAll(path)
}

func (s *stubDiskVolumeOps) MoveVolumeDir(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return fmt.Errorf("failed to create parent directory for %s: %w", dst, err)
	}
	return os.Rename(src, dst)
}

func (s *stubDiskVolumeOps) VolumePathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func (s *stubDiskVolumeOps) CreateDiskImage(path string, sizeBytes int64) error {
	return fmt.Errorf("disk images not supported on darwin")
}

func (s *stubDiskVolumeOps) RemoveDiskImage(path string) error {
	return os.Remove(path)
}

type stubDiskMountOps struct{}

func NewRealDiskMountOps(_ *slog.Logger) DiskMountOps {
	return &stubDiskMountOps{}
}

func (s *stubDiskMountOps) CreateDir(path string, perm os.FileMode) error {
	return os.MkdirAll(path, perm)
}

func (s *stubDiskMountOps) RemoveFile(path string) error {
	return os.Remove(path)
}

func (s *stubDiskMountOps) LoopAttach(_ string) (string, error) {
	return "", fmt.Errorf("loop devices not supported on darwin")
}

func (s *stubDiskMountOps) LoopDetach(_ string) error {
	return fmt.Errorf("loop devices not supported on darwin")
}

func (s *stubDiskMountOps) FindLoopByBacking(_ string) (string, error) {
	return "", nil
}

func (s *stubDiskMountOps) FindAllLoopBackings() (map[string]LoopBacking, error) {
	return nil, nil
}

func (s *stubDiskMountOps) LbdAttach(_ context.Context, _, _ string) (string, error) {
	return "", fmt.Errorf("lbd not supported on darwin")
}

func (s *stubDiskMountOps) LbdDetach(_ context.Context, _ string) error {
	return fmt.Errorf("lbd not supported on darwin")
}

func (s *stubDiskMountOps) LbdAvailable() bool {
	return false
}

func (s *stubDiskMountOps) Mount(_, _, _ string, _ bool) error {
	return fmt.Errorf("mount not supported on darwin")
}

func (s *stubDiskMountOps) Unmount(_ string) error {
	return fmt.Errorf("unmount not supported on darwin")
}

func (s *stubDiskMountOps) IsMounted(_ string) bool {
	return false
}

func (s *stubDiskMountOps) IsDeviceMounted(_ string) (bool, error) {
	return false, nil
}

func (s *stubDiskMountOps) FindMounts(_ string) []ActiveMount {
	return nil
}

func (s *stubDiskMountOps) IsFormatted(_ context.Context, _, _ string) (bool, error) {
	return false, fmt.Errorf("not supported on darwin")
}

func (s *stubDiskMountOps) FormatDevice(_ context.Context, _, _ string) error {
	return fmt.Errorf("not supported on darwin")
}

func (s *stubDiskMountOps) Fsck(_ context.Context, _, _ string) error {
	return fmt.Errorf("not supported on darwin")
}

func EnsureLoopDevices(_ *slog.Logger) error {
	return fmt.Errorf("loop devices not supported on darwin")
}

func EnsureLbdDevices(_ *slog.Logger) error {
	return fmt.Errorf("lbd not supported on darwin")
}

func LoopDeviceAvailable() bool {
	return false
}
