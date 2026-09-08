//go:build !linux

package commands

import "fmt"

// DiskAcceleratorStatus is not supported on non-Linux platforms
func DiskAcceleratorStatus(ctx *Context, opts struct {
	FormatOptions
	DataPath string `long:"data-path" description:"Path to miren data" default:"/var/lib/miren"`
}) error {
	return fmt.Errorf("disk accelerator status is only available on Linux")
}

// DiskAcceleratorInstall is not supported on non-Linux platforms
func DiskAcceleratorInstall(ctx *Context, opts struct {
	ConfigCentric

	Force bool   `short:"f" long:"force" description:"Rebuild even when the module is already current"`
	Node  string `position:"0" usage:"Runner to install on (name, ID, or short ID)" required:"true"`
}) error {
	return fmt.Errorf("disk accelerator install is only available on Linux")
}

// DiskAcceleratorUninstall is not supported on non-Linux platforms
func DiskAcceleratorUninstall(ctx *Context, opts struct {
	DataPath string `long:"data-path" description:"Path to miren data" default:"/var/lib/miren"`
}) error {
	return fmt.Errorf("disk accelerator uninstall is only available on Linux")
}
