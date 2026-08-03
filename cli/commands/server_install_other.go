//go:build !linux

package commands

import (
	"fmt"
	"runtime"
)

// ServerInstall is not supported on non-Linux platforms
func ServerInstall(ctx *Context, opts struct {
	Address         string            `short:"a" long:"address" description:"Server address to bind to" default:"0.0.0.0:8443"`
	Verbosity       string            `long:"verbosity" description:"Extra verbosity to bake into the unit (e.g. -v); the server defaults to Info without it"`
	Branch          string            `short:"b" long:"branch" description:"Branch to download if release not found" default:"main"`
	Force           bool              `short:"f" long:"force" description:"Overwrite existing service file"`
	NoStart         bool              `long:"no-start" description:"Do not start the service after installation"`
	WithoutCloud    bool              `long:"without-cloud" description:"Skip cloud registration setup"`
	ClusterName     string            `short:"n" long:"name" description:"Cluster name for cloud registration"`
	CloudURL        string            `short:"u" long:"url" description:"Cloud URL for registration" default:"https://miren.cloud"`
	Tags            map[string]string `short:"t" long:"tag" description:"Tags for the cluster (key:value)"`
	SkipSystemCheck bool              `long:"skip-system-check" description:"Skip minimum system requirements check"`
}) error {
	fmt.Println()
	ctx.Warn("The 'server install' command is only available on Linux.")
	fmt.Println()

	if runtime.GOOS == "darwin" {
		ctx.Info("On macOS, run the miren server in a container (Docker or Podman):")
		fmt.Println("  miren server container install")
		fmt.Println()
		ctx.Info("The container restarts automatically while your container runtime is up.")
		fmt.Println("  On macOS, make sure Docker Desktop or the Podman machine starts at login.")
	} else {
		ctx.Info("Run the miren server in a container (Docker or Podman) on this platform:")
		fmt.Println("  miren server container install")
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
	return fmt.Errorf("server uninstall is only available on Linux; use 'miren server container uninstall' instead")
}

// ServerStatus is not supported on non-Linux platforms
func ServerStatus(ctx *Context, opts struct {
	Follow bool `short:"f" long:"follow" description:"Follow logs in real-time"`
}) error {
	return fmt.Errorf("server status is only available on Linux; use 'miren server container status' instead")
}
