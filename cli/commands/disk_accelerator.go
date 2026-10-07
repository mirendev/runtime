//go:build linux

package commands

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"miren.dev/runtime/clientconfig"
	runtimeserver "miren.dev/runtime/components/server"
	"miren.dev/runtime/pkg/lbdmod"
	"miren.dev/runtime/pkg/runnerconfig"
	"miren.dev/runtime/pkg/serverconfig"
	"miren.dev/runtime/pkg/ui"
)

func currentDiskAcceleratorNode(ctx *Context, runnerConfigPath, serverUnitPath, serverConfigPath string) (string, *runnerconfig.Config, string, error) {
	runner, err := runnerconfig.Load(runnerConfigPath)
	if err == nil {
		if id := strings.TrimSpace(runner.RunnerID); id != "" {
			return id, runner, "", nil
		}
		return "", nil, "", fmt.Errorf("runner config %s has no runner ID", runnerConfigPath)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", nil, "", fmt.Errorf("reading local runner identity: %w", err)
	}
	if _, err := os.Stat(serverUnitPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil, "", fmt.Errorf("no local runner or installed server found; pass a node name or ID, or use 'miren server install --disk-accelerator' before startup")
		}
		return "", nil, "", fmt.Errorf("checking local server installation: %w", err)
	}
	server, err := serverconfig.Load(serverConfigPath, serverconfig.NewCLIFlags(), ctx.Log)
	if err != nil {
		return "", nil, "", fmt.Errorf("reading local server identity: %w", err)
	}
	if id := strings.TrimSpace(server.Server.GetRunnerID()); id != "" {
		clusterName := server.Server.GetConfigClusterName()
		if clusterName == "" {
			clusterName = "local"
		}
		return id, nil, clusterName, nil
	}
	return "", nil, "", fmt.Errorf("local server has no runner ID")
}

func localDiskAcceleratorCluster(ctx *Context, name, serverConfigPath string) (*clientconfig.ClusterConfig, error) {
	server, err := serverconfig.Load(serverConfigPath, serverconfig.NewCLIFlags(), ctx.Log)
	if err != nil {
		return nil, fmt.Errorf("reading installed server configuration: %w", err)
	}
	address := runtimeserver.LocalClientAddress(ctx.Log, server.Server.GetAddress())
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("invalid local server address %q: %w", address, err)
	}
	ip := net.ParseIP(host)
	if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return nil, fmt.Errorf("server address %q is not loopback; pass a node name or ID with a configured cluster", address)
	}
	if host == "localhost" {
		host = "127.0.0.1"
	}
	ca, err := os.ReadFile(filepath.Join(server.Server.GetDataPath(), "server", "ca.crt"))
	if err != nil {
		return nil, fmt.Errorf("reading installed server CA: %w", err)
	}
	cluster, err := clientconfig.LoadLocalServerCluster("local")
	if err != nil && name != "local" {
		cluster, err = clientconfig.LoadLocalServerCluster(name)
	}
	if err != nil {
		return nil, err
	}
	if string(ca) != cluster.CACert || cluster.ClientCert == "" || cluster.ClientKey == "" {
		return nil, fmt.Errorf("local server credentials do not match the installed server; pass a node name or ID with a configured cluster")
	}
	_, port, err := net.SplitHostPort(cluster.Hostname)
	if err != nil {
		return nil, fmt.Errorf("invalid local server credential address %q: %w", cluster.Hostname, err)
	}
	return &clientconfig.ClusterConfig{
		Hostname:   net.JoinHostPort(host, port),
		CACert:     cluster.CACert,
		ClientCert: cluster.ClientCert,
		ClientKey:  cluster.ClientKey,
	}, nil
}

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

	rows := []ui.Row{
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
			ui.Row{"Installed version", status.Marker.LbdVersion},
			ui.Row{"Built for kernel", status.Marker.KernelRelease},
			ui.Row{"Built at", status.Marker.BuiltAt.Local().Format(time.RFC3339)},
		)
	}
	table := ui.NewTable(ui.WithColumns(ui.AutoSizeColumns([]string{"", ""}, rows, nil)), ui.WithRows(rows))
	ctx.Printf("%s\n", table.Render())

	switch {
	case status.Available() && !status.Stale():
		return nil
	case status.Stale():
		ctx.Warn("The installed module no longer matches this host. Run: miren disk accelerator install --force")
	case status.Host.HeadersDir == "" && status.Host.CanFetchHeaders():
		ctx.Info("This host has no kernel headers; the builder will fetch them. Run: miren disk accelerator install")
	case status.Host.HeadersDir == "":
		ctx.Warn("This host has no kernel headers, which the build needs. %s", status.Host.InstallHint())
	default:
		ctx.Info("To enable accelerator mode, run: miren disk accelerator install")
	}
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
