//go:build !linux

package commands

import (
	"fmt"
	"runtime"
)

// ServerInstall is not supported on non-Linux platforms
func ServerInstall(ctx *Context, opts struct {
	Address      string            `short:"a" long:"address" description:"Server address to bind to" default:"0.0.0.0:8443"`
	Verbosity    string            `short:"v" long:"verbosity" description:"Verbosity level" default:"-vv"`
	Branch       string            `short:"b" long:"branch" description:"Branch to download if release not found" default:"main"`
	Force        bool              `short:"f" long:"force" description:"Overwrite existing service file"`
	NoStart      bool              `long:"no-start" description:"Do not start the service after installation"`
	WithoutCloud bool              `long:"without-cloud" description:"Skip cloud registration setup"`
	ClusterName  string            `short:"n" long:"name" description:"Cluster name for cloud registration"`
	CloudURL     string            `short:"u" long:"url" description:"Cloud URL for registration" default:"https://miren.cloud"`
	Tags         map[string]string `short:"t" long:"tag" description:"Tags for the cluster (key:value)"`
}) error {
	fmt.Println()
	ctx.Warn("The 'server install' command is only available on Linux.")
	fmt.Println()

	if runtime.GOOS == "darwin" {
		ctx.Info("On macOS, use Docker to run the miren server:")
		fmt.Println("  miren server docker install")
		fmt.Println()
		ctx.Info("This will run the miren server in a Docker container with automatic restarts.")
	} else {
		ctx.Info("Use Docker to run the miren server on this platform:")
		fmt.Println("  miren server docker install")
	}

	fmt.Println()
	return fmt.Errorf("server install requires Linux")
}

// ServerUninstall is not supported on non-Linux platforms
func ServerUninstall(ctx *Context, opts struct {
	RemoveData bool   `long:"remove-data" description:"Remove /var/lib/miren directory after backing it up"`
	BackupDir  string `long:"backup-dir" description:"Directory to save backup tarball" default:"."`
	SkipBackup bool   `long:"skip-backup" description:"Skip backup when removing data (dangerous)"`
}) error {
	return fmt.Errorf("server uninstall is only available on Linux; use 'miren server docker uninstall' instead")
}

// ServerStatus is not supported on non-Linux platforms
func ServerStatus(ctx *Context, opts struct {
	Follow bool `short:"f" long:"follow" description:"Follow logs in real-time"`
}) error {
	return fmt.Errorf("server status is only available on Linux; use 'miren server docker status' instead")
}
