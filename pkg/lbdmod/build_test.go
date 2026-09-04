package lbdmod

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testInstaller(t *testing.T, root, dataPath string) *Installer {
	t.Helper()
	return &Installer{
		Log:     slog.New(slog.DiscardHandler),
		Options: Options{Root: root, DataPath: dataPath},
	}
}

func TestCheckCanBuildRefusesClangKernels(t *testing.T) {
	root := ubuntuRoot(t)
	writeFile(t, root, "proc/version",
		"Linux version "+testRelease+" (build@) (Android clang version 17.0.4, LLD 17.0.4) #1 SMP\n")

	status, err := Probe(Options{Root: root, DataPath: t.TempDir()})
	require.NoError(t, err)

	// Root and containerd are checked first, so exercise the compiler rule
	// directly rather than depending on how the test runner is invoked.
	err = testInstaller(t, root, t.TempDir()).checkCompilerAndHeaders(status)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "built with clang-17")
}

func TestCheckCanBuildNeedsHeadersOffTheDebianFamily(t *testing.T) {
	// The builder image is Debian-based, so it can only fetch headers from a
	// Debian-family archive. Anywhere else the operator has to install them,
	// and the message has to name the package or they have to go looking.
	root := t.TempDir()
	writeFile(t, root, "proc/sys/kernel/osrelease", "6.11.4-301.fc41.x86_64\n")
	writeFile(t, root, "etc/os-release", "ID=fedora\n")

	status, err := Probe(Options{Root: root, DataPath: t.TempDir()})
	require.NoError(t, err)

	err = testInstaller(t, root, t.TempDir()).checkCompilerAndHeaders(status)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no kernel headers")
	assert.Contains(t, err.Error(), "dnf install kernel-devel-6.11.4-301.fc41.x86_64")
}

func TestCheckCanBuildLetsDebianFamilyHostsFetchHeaders(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "proc/sys/kernel/osrelease", testRelease+"\n")
	writeFile(t, root, "etc/os-release", "ID=ubuntu\nID_LIKE=debian\n")

	status, err := Probe(Options{Root: root, DataPath: t.TempDir()})
	require.NoError(t, err)
	require.Empty(t, status.Host.HeadersDir)

	// Missing headers are not fatal here: the builder installs them itself.
	require.NoError(t, testInstaller(t, root, t.TempDir()).checkCompilerAndHeaders(status))
}

func TestBuildAsksTheBuilderToFetchHeadersWhenTheHostHasNone(t *testing.T) {
	i := testInstaller(t, ubuntuRoot(t), t.TempDir())
	builder := &fakeBuilder{onBuild: produceArtifacts(t)}
	i.Builder = builder

	host := Host{KernelRelease: testRelease, DistroID: "ubuntu", DistroLike: []string{"debian"}}
	require.NoError(t, i.build(t.Context(), host))

	assert.Contains(t, builder.spec.Env, "FETCH_HEADERS=1")
	assert.True(t, builder.spec.HostNetwork, "fetching headers needs to reach the archive")

	// /lib/modules and /usr/src are deliberately left unmounted so the
	// builder can install headers into its own filesystem.
	for _, m := range builder.spec.Mounts {
		assert.NotEqual(t, "/lib/modules", m.Destination)
		assert.NotEqual(t, "/usr/src", m.Destination)
	}
}

func TestBuildAgainstHostHeadersNeedsNoNetwork(t *testing.T) {
	i := testInstaller(t, ubuntuRoot(t), t.TempDir())
	builder := &fakeBuilder{onBuild: produceArtifacts(t)}
	i.Builder = builder

	host := Host{KernelRelease: testRelease, HeadersDir: "/lib/modules/" + testRelease + "/build"}
	require.NoError(t, i.build(t.Context(), host))

	assert.False(t, builder.spec.HostNetwork, "a build against host headers should reach nothing")
	assert.NotContains(t, builder.spec.Env, "FETCH_HEADERS=1")
}

func TestCheckCanBuildAcceptsAGoodHost(t *testing.T) {
	root := ubuntuRoot(t)
	status, err := Probe(Options{Root: root, DataPath: t.TempDir()})
	require.NoError(t, err)
	require.NoError(t, testInstaller(t, root, t.TempDir()).checkCompilerAndHeaders(status))
}

func TestSecureBootDetection(t *testing.T) {
	// No EFI at all: not enforcing, rather than guessing yes and blocking a
	// host that simply is not using EFI.
	assert.False(t, secureBootEnforcing(t.TempDir()))

	off := t.TempDir()
	writeFile(t, off, "sys/firmware/efi/efivars/SecureBoot-8be4df61-93ca-11d2-aa0d-00e098032b8c",
		string([]byte{6, 0, 0, 0, 0}))
	assert.False(t, secureBootEnforcing(off))

	on := t.TempDir()
	writeFile(t, on, "sys/firmware/efi/efivars/SecureBoot-8be4df61-93ca-11d2-aa0d-00e098032b8c",
		string([]byte{6, 0, 0, 0, 1}))
	assert.True(t, secureBootEnforcing(on))
}

func TestSecureBootBlocksTheBuild(t *testing.T) {
	root := ubuntuRoot(t)
	writeFile(t, root, "sys/firmware/efi/efivars/SecureBoot-8be4df61-93ca-11d2-aa0d-00e098032b8c",
		string([]byte{6, 0, 0, 0, 1}))

	status, err := Probe(Options{Root: root, DataPath: t.TempDir()})
	require.NoError(t, err)

	err = testInstaller(t, root, t.TempDir()).checkCompilerAndHeaders(status)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Secure Boot")
}

func TestBuildDirIsKeyedByKernelAndVersion(t *testing.T) {
	i := testInstaller(t, "/", "/var/lib/miren")

	// A rebuild after a kernel upgrade must not reuse the old kernel's object
	// files, so the two kernels get separate directories.
	assert.NotEqual(t, i.buildDir("6.8.0-51-generic"), i.buildDir("6.8.0-52-generic"))
	assert.Contains(t, i.buildDir("6.8.0-51-generic"), SourceVersion())
	assert.Contains(t, i.buildDir("6.8.0-51-generic"), "6.8.0-51-generic")
}

func TestInstallFileIsAtomicAndCreatesParents(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "lbd.ko")
	require.NoError(t, os.WriteFile(src, []byte("module"), 0644))

	dest := filepath.Join(dir, "lib", "modules", testRelease, "extra", "lbd.ko")
	require.NoError(t, installFile(src, dest, 0644))

	data, err := os.ReadFile(dest)
	require.NoError(t, err)
	assert.Equal(t, "module", string(data))

	info, err := os.Stat(dest)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0644), info.Mode().Perm())

	// No temporary file is left behind.
	_, err = os.Stat(dest + ".tmp")
	assert.True(t, os.IsNotExist(err))

	// Overwriting an existing module works, which is the kernel-upgrade path.
	require.NoError(t, os.WriteFile(src, []byte("newer"), 0644))
	require.NoError(t, installFile(src, dest, 0644))
	data, err = os.ReadFile(dest)
	require.NoError(t, err)
	assert.Equal(t, "newer", string(data))
}

func TestInstallFileReportsAMissingSource(t *testing.T) {
	dir := t.TempDir()
	err := installFile(filepath.Join(dir, "absent"), filepath.Join(dir, "dest"), 0644)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reading")
}

// fakeBuilder stands in for the container runtime. onBuild may write into the
// spec's /out mount to imitate a successful compile.
type fakeBuilder struct {
	spec    BuildSpec
	called  bool
	err     error
	onBuild func(spec BuildSpec) error
}

func (f *fakeBuilder) Build(_ context.Context, spec BuildSpec) error {
	f.called = true
	f.spec = spec
	if f.err != nil {
		return f.err
	}
	if f.onBuild != nil {
		return f.onBuild(spec)
	}
	return nil
}

// hostMount finds a mount by its path inside the container.
func hostMount(t *testing.T, spec BuildSpec, dest string) Mount {
	t.Helper()
	for _, m := range spec.Mounts {
		if m.Destination == dest {
			return m
		}
	}
	t.Fatalf("no mount at %s", dest)
	return Mount{}
}

// produceArtifacts imitates a builder that compiled successfully.
func produceArtifacts(t *testing.T) func(BuildSpec) error {
	t.Helper()
	return func(spec BuildSpec) error {
		out := hostMount(t, spec, "/out").Source
		for _, name := range []string{"lbd.ko", "lbdctl"} {
			if err := os.WriteFile(filepath.Join(out, name), []byte(name), 0644); err != nil {
				return err
			}
		}
		return nil
	}
}

func TestBuildHandsTheBuilderSourceAndHeaders(t *testing.T) {
	dataPath := t.TempDir()
	builder := &fakeBuilder{onBuild: produceArtifacts(t)}

	i := testInstaller(t, ubuntuRoot(t), dataPath)
	i.Builder = builder

	host := Host{
		KernelRelease: testRelease,
		HeadersDir:    "/lib/modules/" + testRelease + "/build",
		DistroID:      "ubuntu",
		DistroLike:    []string{"debian"},
	}
	require.NoError(t, i.build(t.Context(), host))
	require.True(t, builder.called)

	// The source is materialized into a writable directory, because the
	// kernel build writes its object files next to the source.
	src := hostMount(t, builder.spec, "/src")
	assert.False(t, src.ReadOnly)
	_, err := os.Stat(filepath.Join(src.Source, "lbd_main.c"))
	require.NoError(t, err)

	// The host's kernel tree is mounted at its real path, so the absolute
	// symlinks inside it resolve, and read-only so a build cannot damage it.
	modules := hostMount(t, builder.spec, "/lib/modules")
	assert.Equal(t, "/lib/modules", modules.Source)
	assert.True(t, modules.ReadOnly)
	assert.True(t, hostMount(t, builder.spec, "/usr/src").ReadOnly)

	assert.Contains(t, builder.spec.Env, "KERNEL_RELEASE="+testRelease)
	assert.Contains(t, builder.spec.Env, "KERNEL_HEADERS=/lib/modules/"+testRelease+"/build")
	assert.Contains(t, builder.spec.Env, "HOST_DISTRO_ID=ubuntu")
	assert.Contains(t, builder.spec.Env, "HOST_DISTRO_LIKE=debian")
}

func TestBuildRejectsABuilderThatProducedNothing(t *testing.T) {
	// The module Makefile downgrades unresolved symbols to warnings, so a
	// zero exit does not prove there is a module to install.
	i := testInstaller(t, ubuntuRoot(t), t.TempDir())
	i.Builder = &fakeBuilder{}

	err := i.build(t.Context(), Host{KernelRelease: testRelease, HeadersDir: "/lib/modules/" + testRelease + "/build"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "produced no lbd.ko")
}

func TestBuildSurfacesTheBuilderError(t *testing.T) {
	i := testInstaller(t, ubuntuRoot(t), t.TempDir())
	i.Builder = &fakeBuilder{err: &BuildFailedError{ExitCode: 2, Output: "error: no kernel headers"}}

	err := i.build(t.Context(), Host{KernelRelease: testRelease})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no kernel headers")
}

func TestBuildStartsFromCleanSource(t *testing.T) {
	dataPath := t.TempDir()
	i := testInstaller(t, ubuntuRoot(t), dataPath)
	host := Host{KernelRelease: testRelease, HeadersDir: "/lib/modules/" + testRelease + "/build"}

	// Leave debris from a build that failed partway through.
	stale := filepath.Join(i.buildDir(testRelease), "src", "lbd_main.o")
	require.NoError(t, os.MkdirAll(filepath.Dir(stale), 0755))
	require.NoError(t, os.WriteFile(stale, []byte("stale object"), 0644))

	i.Builder = &fakeBuilder{onBuild: produceArtifacts(t)}
	require.NoError(t, i.build(t.Context(), host))

	_, err := os.Stat(stale)
	assert.True(t, os.IsNotExist(err), "object files from a failed build must not survive into the retry")
}

func TestBuildUsesTheConfiguredImage(t *testing.T) {
	i := testInstaller(t, ubuntuRoot(t), t.TempDir())
	builder := &fakeBuilder{onBuild: produceArtifacts(t)}
	i.Builder = builder
	i.Image = "example.test/lbd-builder:local"

	require.NoError(t, i.build(t.Context(), Host{KernelRelease: testRelease}))
	assert.Equal(t, "example.test/lbd-builder:local", builder.spec.Image)
}

func TestEnsureCurrentLeavesAHostThatNeverOptedInAlone(t *testing.T) {
	// No install record means accelerator mode was never enabled here, so
	// startup must not pay for an unattended compile.
	i := testInstaller(t, ubuntuRoot(t), t.TempDir())
	builder := &fakeBuilder{}
	i.Builder = builder

	rebuilt, err := i.EnsureCurrent(t.Context())
	require.NoError(t, err)
	assert.False(t, rebuilt)
	assert.False(t, builder.called, "a host with no install record must not be built for")
}

func TestEnsureCurrentSkipsAHealthyHost(t *testing.T) {
	root := ubuntuRoot(t)
	dataPath := t.TempDir()
	writeFile(t, root, "proc/modules", "lbd 65536 1 - Live 0x0000000000000000\n")
	writeFile(t, root, ControlDevice, "")
	writeFile(t, root, modulePath(testRelease), "")
	writeFile(t, root, "usr/local/bin/lbdctl", "")
	require.NoError(t, writeMarker(dataPath, Marker{
		LbdVersion:    SourceVersion(),
		KernelRelease: testRelease,
		ModulePath:    modulePath(testRelease),
	}))

	i := testInstaller(t, root, dataPath)
	i.Options.SearchPath = []string{"/usr/local/bin"}
	builder := &fakeBuilder{}
	i.Builder = builder

	rebuilt, err := i.EnsureCurrent(t.Context())
	require.NoError(t, err)
	assert.False(t, rebuilt)
	assert.False(t, builder.called)
}

func TestEnsureCurrentRebuildsAfterAKernelUpgrade(t *testing.T) {
	root := ubuntuRoot(t)
	dataPath := t.TempDir()

	// The host installed lbd for a kernel it is no longer running.
	require.NoError(t, writeMarker(dataPath, Marker{
		LbdVersion:    SourceVersion(),
		KernelRelease: "6.8.0-45-generic",
		ModulePath:    modulePath("6.8.0-45-generic"),
	}))

	i := testInstaller(t, root, dataPath)
	builder := &fakeBuilder{}
	i.Builder = builder

	// It decides to act, which is the point. Whether it then gets past the
	// root check depends on how the tests were invoked, and the install needs
	// a real kernel and depmod either way -- so what is asserted is that it
	// did not quietly do nothing.
	rebuilt, err := i.EnsureCurrent(t.Context())
	assert.False(t, rebuilt && err != nil, "a rebuild cannot both succeed and fail")
	assert.True(t, err != nil || builder.called,
		"a stale module must trigger a rebuild attempt, not silence")
}

func TestBuildFailedErrorQuotesTheOutput(t *testing.T) {
	err := &BuildFailedError{ExitCode: 2, Output: "error: no kernel headers for 6.8.0-51-generic"}
	assert.Contains(t, err.Error(), "exit 2")
	assert.Contains(t, err.Error(), "no kernel headers")

	bare := &BuildFailedError{ExitCode: 1}
	assert.Equal(t, "the lbd build failed (exit 1)", bare.Error())
	assert.False(t, strings.HasSuffix(bare.Error(), ":\n"))
}

func TestUninstallRemovesWhatWasInstalledNotWhatIsRunning(t *testing.T) {
	// The host has moved on to a newer kernel since the install. Deriving the
	// module path from the running kernel would miss the real artifact and
	// leave it on disk forever.
	installedKernel := "6.8.0-45-generic"
	m := &Marker{
		LbdVersion:    SourceVersion(),
		KernelRelease: installedKernel,
		ModulePath:    modulePath(installedKernel),
		LbdctlPath:    "/usr/local/bin/lbdctl",
	}

	paths := uninstallPaths(m)

	assert.Contains(t, paths, modulePath(installedKernel))
	assert.NotContains(t, paths, modulePath(testRelease),
		"the running kernel's path was never installed")
	assert.Contains(t, paths, "/usr/local/bin/lbdctl")
	assert.Contains(t, paths, modulesLoadConf)
}

func TestUninstallLeavesAnLbdctlItDidNotInstall(t *testing.T) {
	// An operator who followed the lbd repo's README has their own lbdctl at
	// the same path. A marker that never recorded one must not license
	// deleting it.
	m := &Marker{
		LbdVersion:    SourceVersion(),
		KernelRelease: testRelease,
		ModulePath:    modulePath(testRelease),
	}

	paths := uninstallPaths(m)

	assert.Contains(t, paths, modulePath(testRelease))
	for _, p := range paths {
		assert.NotContains(t, p, "lbdctl", "an unrecorded lbdctl is not ours to remove")
	}
}

func TestUninstallPathsWithNoMarker(t *testing.T) {
	// No install record means miren put nothing on this host.
	assert.Empty(t, uninstallPaths(nil))
}
