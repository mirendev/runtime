package mountinfo

import (
	"path/filepath"
	"strings"

	"github.com/moby/sys/mountinfo"
)

func CurrentMounts() ([]*mountinfo.Info, error) {
	return mountinfo.GetMounts(nil)
}

func MountPoint(dir string) (*mountinfo.Info, error) {
	mounts, err := CurrentMounts()
	if err != nil {
		return nil, err
	}

	dir, err = filepath.Abs(dir)
	if err != nil {
		return nil, err
	}

	var found *mountinfo.Info

	for _, m := range mounts {
		if strings.HasPrefix(dir, m.Mountpoint) {
			if found == nil || len(m.Mountpoint) > len(found.Mountpoint) {
				found = m
			}
		}
	}

	return found, nil
}
