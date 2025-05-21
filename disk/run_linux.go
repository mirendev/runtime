//go:build linux

package disk

import "golang.org/x/sys/unix"

func mountDisk(devPath string, fsPath string) error {
	return unix.Mount(devPath, fsPath, "ext4", 0, "")
}
