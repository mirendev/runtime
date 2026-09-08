//go:build linux

package commands

import (
	"fmt"
	"time"

	"miren.dev/runtime/api/runner/runner_v1alpha"
	"miren.dev/runtime/pkg/lbdmod"
	"miren.dev/runtime/pkg/rpc"
)

// DiskAcceleratorStatus reports whether accelerator mode can run on this host.
// It only reads, so it does not need root.
func DiskAcceleratorStatus(ctx *Context, opts struct {
	FormatOptions
	DataPath string `long:"data-path" description:"Path to miren data" default:"/var/lib/miren"`
}) error {
	status, err := lbdmod.Probe(lbdmod.HostOptions(opts.DataPath))
	if err != nil {
		return err
	}

	if opts.IsJSON() {
		return PrintJSON(newAcceleratorStatusJSON(status))
	}

	rows := [][]string{
		{"Available", yesNo(status.Available())},
		{"State", status.Explain()},
		{"Kernel", status.Host.KernelRelease},
		{"Module loaded", yesNo(status.Loaded)},
		{"Control device", yesNo(status.ControlDevicePresent)},
		{"Module installed", yesNo(status.ModuleInstalled)},
		{"lbdctl", orNone(status.LbdctlPath)},
		{"Kernel headers", orNone(status.Host.HeadersDir)},
		{"Bundled lbd version", status.EmbeddedVersion},
	}
	if status.Marker != nil {
		rows = append(rows,
			[]string{"Installed version", status.Marker.LbdVersion},
			[]string{"Built for kernel", status.Marker.KernelRelease},
			[]string{"Built at", status.Marker.BuiltAt.Local().Format(time.RFC3339)},
		)
	}
	ctx.DisplayTable([]string{"", ""}, rows)

	switch {
	case status.Available() && !status.Stale():
		return nil
	case status.Stale():
		ctx.Warn("The installed module no longer matches this host. Run: miren disk accelerator install <node>")
	case status.Host.HeadersDir == "" && status.Host.CanFetchHeaders():
		ctx.Info("This host has no kernel headers; the builder will fetch them. Run: miren disk accelerator install <node>")
	case status.Host.HeadersDir == "":
		ctx.Warn("This host has no kernel headers, which the build needs. %s", status.Host.InstallHint())
	default:
		ctx.Info("To enable accelerator mode, run: miren disk accelerator install <node>")
	}
	return nil
}

// DiskAcceleratorInstall asks the cluster to build and load the lbd kernel
// module on a node, so its disks use accelerator mode instead of loop devices.
//
// This runs through the server rather than locally because the toolchain image
// lives in the cluster registry, and reaching it needs an identity the CLI does
// not hold. The coordinator builds the image if it is missing, then hands the
// work to the node, which is where the module has to be compiled anyway.
func DiskAcceleratorInstall(ctx *Context, opts struct {
	ConfigCentric

	Force bool   `short:"f" long:"force" description:"Rebuild even when the module is already current"`
	Node  string `position:"0" usage:"Runner to install on (name, ID, or short ID)" required:"true"`
}) error {
	client, err := ctx.RPCClient(rpc.ServiceRunner)
	if err != nil {
		return err
	}
	defer client.Close()

	rc := runner_v1alpha.NewRunnerRegistrationClient(client)

	ctx.Begin("Installing the lbd kernel module on %s", opts.Node)

	res, err := rc.InstallDiskAccelerator(ctx, opts.Node, opts.Force)
	if err != nil {
		return err
	}
	if res.Error() != "" {
		return fmt.Errorf("%s", res.Error())
	}

	ctx.Completed("Accelerator mode is ready on %s, kernel %s", res.Name(), res.KernelRelease())
	ctx.Info("Restart that node's miren service to pick it up")
	return nil
}

// DiskAcceleratorUninstall unloads the module and removes what the install put
// on the host, including the record that would otherwise rebuild it after a
// kernel upgrade.
func DiskAcceleratorUninstall(ctx *Context, opts struct {
	DataPath string `long:"data-path" description:"Path to miren data" default:"/var/lib/miren"`
}) error {
	installer := &lbdmod.Installer{
		Log:     ctx.Log,
		Options: lbdmod.HostOptions(opts.DataPath),
	}

	ctx.Begin("Removing the lbd kernel module")
	if err := installer.Uninstall(ctx); err != nil {
		return err
	}

	ctx.Completed("Accelerator mode removed; disks will use loop devices")
	return nil
}

// acceleratorStatusJSON is the machine-readable shape of the status command.
type acceleratorStatusJSON struct {
	Available            bool   `json:"available"`
	State                string `json:"state"`
	Kernel               string `json:"kernel"`
	ModuleLoaded         bool   `json:"module_loaded"`
	ControlDevicePresent bool   `json:"control_device_present"`
	ModuleInstalled      bool   `json:"module_installed"`
	Stale                bool   `json:"stale"`
	LbdctlPath           string `json:"lbdctl_path"`
	KernelHeaders        string `json:"kernel_headers"`
	HeaderPackage        string `json:"header_package"`
	BundledVersion       string `json:"bundled_version"`
	InstalledVersion     string `json:"installed_version,omitempty"`
	BuiltForKernel       string `json:"built_for_kernel,omitempty"`
	BuiltAt              string `json:"built_at,omitempty"`
}

func newAcceleratorStatusJSON(s lbdmod.Status) acceleratorStatusJSON {
	out := acceleratorStatusJSON{
		Available:            s.Available(),
		State:                s.Explain(),
		Kernel:               s.Host.KernelRelease,
		ModuleLoaded:         s.Loaded,
		ControlDevicePresent: s.ControlDevicePresent,
		ModuleInstalled:      s.ModuleInstalled,
		Stale:                s.Stale(),
		LbdctlPath:           s.LbdctlPath,
		KernelHeaders:        s.Host.HeadersDir,
		HeaderPackage:        s.Host.HeaderPackage(),
		BundledVersion:       s.EmbeddedVersion,
	}
	if s.Marker != nil {
		out.InstalledVersion = s.Marker.LbdVersion
		out.BuiltForKernel = s.Marker.KernelRelease
		out.BuiltAt = s.Marker.BuiltAt.UTC().Format(time.RFC3339)
	}
	return out
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func orNone(s string) string {
	if s == "" {
		return "not found"
	}
	return s
}
