//go:build darwin

package disk

import (
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

// NOTE: This is very unlikely to work whatsoever... possibly should just noop
// instead, but there's a unix.Mount impl so I figured why not at least attempt
// to wire it!
func mountDisk(devPath string, fsPath string) error {
	// Create a C string of the device path
	cDevPath, err := syscall.BytePtrFromString(devPath)
	if err != nil {
		return err
	}

	return unix.Mount("ext4", fsPath, 0, unsafe.Pointer(cDevPath))
}
