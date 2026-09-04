package lbdmod

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"miren.dev/runtime/pkg/imagerefs"
)

const (
	// buildContainerName is fixed so a build killed before its own cleanup
	// leaves something the next run can find and remove.
	buildContainerName = "miren-lbd-build"

	// lbdctlInstallDir is where lbdctl goes. It has to be somewhere on the
	// server process's PATH: the release directory is prepended to
	// containerd's PATH, not miren's.
	lbdctlInstallDir = "/usr/local/bin"

	// modulesLoadConf makes the kernel load lbd at boot, so a reboot does not
	// depend on miren starting first.
	modulesLoadConf = "/etc/modules-load.d/lbd.conf"
)

// Installer builds and installs the lbd kernel module on this host.
type Installer struct {
	// Log receives progress and the builder container's output.
	Log *slog.Logger

	// Builder runs the builder image. Required for Install; Uninstall does
	// not need it. pkg/lbdmod/ctrbuild provides the containerd one.
	Builder Builder

	// Options say where to read host state and keep the install record.
	Options Options

	// Image overrides the builder image. Empty means imagerefs.LbdBuilder.
	Image string
}

// buildDir is the scratch directory a build works in. It is keyed by kernel
// and module version so a rebuild after a kernel upgrade cannot pick up stale
// object files from the previous kernel.
func (i *Installer) buildDir(release string) string {
	key := fmt.Sprintf("%s-%s", SourceVersion(), release)
	return filepath.Join(i.Options.dataPath(), "lbd", "build", key)
}

func (i *Installer) image() string {
	if i.Image != "" {
		return i.Image
	}
	return imagerefs.LbdBuilder
}

// Install compiles the module against the running kernel and loads it. It is
// safe to call when the module is already installed and current: that is
// reported as a no-op unless force is set.
//
// The caller must be root.
func (i *Installer) Install(ctx context.Context, force bool) (Status, error) {
	status, err := Probe(i.Options)
	if err != nil {
		return status, err
	}

	if !force && status.Available() && !status.Stale() {
		i.Log.Info("lbd is already installed and current", "kernel", status.Host.KernelRelease)
		return status, nil
	}

	if err := i.checkCanBuild(status); err != nil {
		return status, err
	}

	// Held for the whole build-and-load, so a concurrent install cannot clear
	// the build directory or delete the builder container out from under this
	// one. Taken after the cheap checks so an obviously impossible install
	// still fails with the real reason rather than a lock error.
	lock, err := acquireBuildLock(i.Options.dataPath())
	if err != nil {
		return status, err
	}
	defer lock.release()

	// Another process may have finished the very build this one was about to
	// start while we waited to be let in.
	if !force {
		if current, err := Probe(i.Options); err == nil && current.Available() && !current.Stale() {
			i.Log.Info("another process installed lbd while this one waited",
				"kernel", current.Host.KernelRelease)
			return current, nil
		}
	}

	if err := i.build(ctx, status.Host); err != nil {
		return status, err
	}

	if err := i.load(ctx, status.Host); err != nil {
		return status, err
	}

	// Prove the module is usable before recording the install. Writing the
	// marker first would leave a record claiming success behind a failure, so
	// `status` would show an installed version and build time for something
	// that never worked. Availability does not depend on the marker, so this
	// check is meaningful without it.
	verified, err := Probe(i.Options)
	if err != nil {
		return status, err
	}
	if !verified.Available() {
		return verified, fmt.Errorf("lbd was built and loaded but is still not usable: %s", verified.Explain())
	}

	marker := Marker{
		LbdVersion:    SourceVersion(),
		KernelRelease: status.Host.KernelRelease,
		ModulePath:    modulePath(status.Host.KernelRelease),
		LbdctlPath:    filepath.Join(lbdctlInstallDir, "lbdctl"),
		BuiltAt:       time.Now().UTC(),
	}
	if err := writeMarker(i.Options.dataPath(), marker); err != nil {
		return verified, err
	}

	// Re-probe so the caller gets a status that includes the record just
	// written, which is what `status` renders.
	after, err := Probe(i.Options)
	if err != nil {
		return verified, err
	}

	i.Log.Info("lbd installed",
		"kernel", after.Host.KernelRelease,
		"version", SourceVersion(),
		"module", marker.ModulePath)
	return after, nil
}

// EnsureCurrent rebuilds the module when this host has installed it before but
// what is on disk no longer fits -- almost always because the kernel was
// upgraded, which leaves a module that cannot load.
//
// A host with no install record is left alone: it never opted into accelerator
// mode, so it should not pay for an unattended compile at startup. It reports
// whether it rebuilt.
func (i *Installer) EnsureCurrent(ctx context.Context) (bool, error) {
	status, err := Probe(i.Options)
	if err != nil {
		return false, err
	}

	if status.Available() && !status.Stale() {
		return false, nil
	}

	if status.Marker == nil {
		return false, nil
	}

	i.Log.Info("rebuilding the lbd kernel module", "reason", status.Explain())
	if _, err := i.Install(ctx, false); err != nil {
		return false, err
	}
	return true, nil
}

// checkCanBuild refuses the cases where a build would either fail confusingly
// or produce a module that cannot be loaded, and says why.
func (i *Installer) checkCanBuild(status Status) error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("installing a kernel module requires root privileges (use sudo)")
	}

	if i.Builder == nil {
		return fmt.Errorf("no container runtime to run the lbd builder in")
	}

	return i.checkCompilerAndHeaders(status)
}

// checkCompilerAndHeaders covers the host conditions that make a build
// pointless: firmware that will refuse the result, a toolchain we cannot
// match, or no build tree to compile against.
func (i *Installer) checkCompilerAndHeaders(status Status) error {
	if secureBootEnforcing(i.Options.root()) {
		return fmt.Errorf("this host has Secure Boot enabled, which refuses unsigned kernel modules. " +
			"miren cannot sign the module, so accelerator mode needs Secure Boot disabled or a signed module from your distribution")
	}

	if status.Host.Compiler.Name == "clang" {
		return fmt.Errorf("this kernel was built with %s, which the lbd builder does not support",
			status.Host.Compiler)
	}

	if status.Host.HeadersDir == "" && !status.Host.CanFetchHeaders() {
		return fmt.Errorf("no kernel headers for %s on this host: %s",
			status.Host.KernelRelease, status.Host.InstallHint())
	}

	return nil
}

// build runs the builder container and leaves lbd.ko and lbdctl in the build
// directory's out/ subdirectory.
func (i *Installer) build(ctx context.Context, host Host) error {
	dir := i.buildDir(host.KernelRelease)
	srcDir := filepath.Join(dir, "src")
	outDir := filepath.Join(dir, "out")

	// Start from clean source every time. A retry after a failed build must
	// not inherit half-written object files.
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("clearing the build directory %s: %w", dir, err)
	}
	if err := materializeSource(srcDir); err != nil {
		return err
	}
	if err := os.MkdirAll(outDir, 0755); err != nil {
		return fmt.Errorf("creating %s: %w", outDir, err)
	}

	i.Log.Info("building the lbd kernel module",
		"kernel", host.KernelRelease,
		"headers", host.HeadersDir,
		"version", SourceVersion())

	spec := BuildSpec{
		Name:  buildContainerName,
		Image: i.image(),
		Args:  []string{"/usr/local/bin/build-lbd"},
		Env: []string{
			"KERNEL_RELEASE=" + host.KernelRelease,
			"KERNEL_HEADERS=" + host.HeadersDir,
			"HOST_DISTRO_ID=" + host.DistroID,
			"HOST_DISTRO_LIKE=" + strings.Join(host.DistroLike, " "),
		},
		Mounts: []Mount{
			{Destination: "/src", Source: srcDir},
			{Destination: "/out", Source: outDir},
		},
	}

	if host.HeadersDir != "" {
		// Mounted at their real paths, not under a prefix: a kernel build
		// tree is full of absolute symlinks (/lib/modules/<rel>/build usually
		// points into /usr/src) and they only resolve if the paths match the
		// host's.
		spec.Mounts = append(spec.Mounts,
			Mount{Destination: "/lib/modules", Source: "/lib/modules", ReadOnly: true},
			Mount{Destination: "/usr/src", Source: "/usr/src", ReadOnly: true},
		)
	} else {
		// No build tree on the host, so the builder installs one for itself.
		// Those paths are left unmounted precisely so it can write to them,
		// and it needs a network to reach the distro archive.
		spec.Env = append(spec.Env, "FETCH_HEADERS=1")
		spec.HostNetwork = true
	}

	if err := i.Builder.Build(ctx, spec); err != nil {
		return err
	}

	for _, name := range []string{"lbd.ko", "lbdctl"} {
		if _, err := os.Stat(filepath.Join(outDir, name)); err != nil {
			return fmt.Errorf("the build reported success but produced no %s", name)
		}
	}
	return nil
}

// uninstallPaths lists the files an install left on the host, taken from what
// it recorded rather than from the current state.
//
// Both details matter. The module path has to come from the marker, because
// after a kernel upgrade the running kernel is no longer the one the module
// was built for, and deriving the path from it would miss the real artifact
// and orphan it. lbdctl has to come from the marker too, because the lbd
// repo's README tells people to install their own at the same location, and a
// path we never recorded is not ours to delete.
func uninstallPaths(m *Marker) []string {
	if m == nil {
		return nil
	}

	var paths []string
	for _, p := range []string{m.ModulePath, m.LbdctlPath} {
		if p != "" {
			paths = append(paths, p)
		}
	}
	return append(paths, modulesLoadConf)
}

// installedLbdctl reports whether a previous miren install is what put lbdctl
// at this path.
func (i *Installer) installedLbdctl(path string) bool {
	marker, err := readMarker(i.Options.dataPath())
	if err != nil || marker == nil {
		return false
	}
	return marker.LbdctlPath == path
}

// load installs the built artifacts and brings the module up.
func (i *Installer) load(ctx context.Context, host Host) error {
	outDir := filepath.Join(i.buildDir(host.KernelRelease), "out")

	// Unload before touching anything on disk. rmmod refuses while a device is
	// attached, and that is the common case rather than a rare one -- a node
	// with a running app that has a disk. Overwriting lbd.ko and lbdctl first
	// and only then discovering the refusal would leave a userspace lbdctl
	// talking to a kernel module built from different source, with no marker
	// written to say so. Failing here leaves the host exactly as it was.
	if isModuleLoaded(i.Options.root(), ModuleName) {
		i.Log.Info("unloading the previous lbd module")
		if out, err := exec.CommandContext(ctx, "rmmod", ModuleName).CombinedOutput(); err != nil {
			return fmt.Errorf("could not unload the running lbd module, which is usually because a disk is still attached: %w: %s",
				err, strings.TrimSpace(string(out)))
		}
	}

	dest := modulePath(host.KernelRelease)
	if err := installFile(filepath.Join(outDir, "lbd.ko"), dest, 0644); err != nil {
		return err
	}

	// The lbd repo's own README tells people to install lbdctl here by hand,
	// so an existing binary may well be theirs rather than a previous install
	// of ours. Replacing it is still the right move -- lbdctl and the module
	// have to come from the same source -- but it should not happen silently.
	lbdctl := filepath.Join(lbdctlInstallDir, "lbdctl")
	if _, err := os.Stat(lbdctl); err == nil && !i.installedLbdctl(lbdctl) {
		i.Log.Warn("replacing an lbdctl that miren did not install", "path", lbdctl)
	}
	if err := installFile(filepath.Join(outDir, "lbdctl"), lbdctl, 0755); err != nil {
		return err
	}

	// depmod rebuilds the dependency index modprobe consults; without it
	// modprobe cannot find a module that was just dropped into extra/.
	if out, err := exec.CommandContext(ctx, "depmod", "-a", host.KernelRelease).CombinedOutput(); err != nil {
		return fmt.Errorf("depmod failed: %w: %s", err, strings.TrimSpace(string(out)))
	}

	if out, err := exec.CommandContext(ctx, "modprobe", ModuleName).CombinedOutput(); err != nil {
		return fmt.Errorf("modprobe %s failed: %w: %s", ModuleName, err, strings.TrimSpace(string(out)))
	}

	if err := os.MkdirAll(filepath.Dir(modulesLoadConf), 0755); err != nil {
		return fmt.Errorf("creating %s: %w", filepath.Dir(modulesLoadConf), err)
	}
	if err := os.WriteFile(modulesLoadConf, []byte(ModuleName+"\n"), 0644); err != nil {
		return fmt.Errorf("writing %s: %w", modulesLoadConf, err)
	}

	return nil
}

// Uninstall unloads the module and removes everything the install put on the
// host, including the record that would otherwise trigger a rebuild later.
func (i *Installer) Uninstall(ctx context.Context) error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("removing a kernel module requires root privileges (use sudo)")
	}

	status, err := Probe(i.Options)
	if err != nil {
		return err
	}

	if status.Marker == nil {
		return fmt.Errorf("miren did not install lbd on this host, so there is nothing to remove")
	}

	if status.Loaded {
		if out, err := exec.CommandContext(ctx, "rmmod", ModuleName).CombinedOutput(); err != nil {
			return fmt.Errorf("could not unload lbd, which is usually because a disk is still attached: %w: %s",
				err, strings.TrimSpace(string(out)))
		}
	}

	for _, path := range uninstallPaths(status.Marker) {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("removing %s: %w", path, err)
		}
	}

	// depmod has to reindex the kernel the module was installed for, which
	// after an upgrade is not the one running now.
	if out, err := exec.CommandContext(ctx, "depmod", "-a", status.Marker.KernelRelease).CombinedOutput(); err != nil {
		i.Log.Warn("depmod failed after removing lbd", "error", err,
			"kernel", status.Marker.KernelRelease, "output", strings.TrimSpace(string(out)))
	}

	if err := os.RemoveAll(filepath.Join(i.Options.dataPath(), "lbd", "build")); err != nil {
		i.Log.Warn("failed to remove the lbd build directory", "error", err)
	}

	return removeMarker(i.Options.dataPath())
}

// installFile copies src to dest, creating the destination directory. It writes
// to a temporary name and renames, so a reader never sees a half-written module.
func installFile(src, dest string, mode os.FileMode) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("reading %s: %w", src, err)
	}

	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return fmt.Errorf("creating %s: %w", filepath.Dir(dest), err)
	}

	tmp := dest + ".tmp"
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return fmt.Errorf("writing %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, dest); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("installing %s: %w", dest, err)
	}
	return nil
}

// secureBootEnforcing reports whether the firmware will refuse unsigned
// modules. The efivars file carries a five-byte value whose last byte is the
// flag; anything we cannot read is treated as "not enforcing", since guessing
// yes would block hosts that are simply not using EFI.
func secureBootEnforcing(root string) bool {
	matches, err := filepath.Glob(filepath.Join(root, "sys/firmware/efi/efivars/SecureBoot-*"))
	if err != nil || len(matches) == 0 {
		return false
	}

	data, err := os.ReadFile(matches[0])
	if err != nil || len(data) == 0 {
		return false
	}
	return data[len(data)-1] == 1
}
