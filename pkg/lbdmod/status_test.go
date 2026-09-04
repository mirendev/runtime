package lbdmod

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testRelease = "6.8.0-51-generic"

// ubuntuRoot builds a fixture filesystem for a plausible Ubuntu host with
// kernel headers installed and no lbd anywhere.
func ubuntuRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeFile(t, root, "proc/sys/kernel/osrelease", testRelease+"\n")
	writeFile(t, root, "proc/version",
		"Linux version "+testRelease+" (buildd@lcy02) (x86_64-linux-gnu-gcc-13 (Ubuntu 13.3.0) 13.3.0) #52-Ubuntu SMP\n")
	writeFile(t, root, "etc/os-release", "ID=ubuntu\nID_LIKE=debian\n")
	writeFile(t, root, "lib/modules/"+testRelease+"/build/Makefile", "# kernel build tree\n")
	writeFile(t, root, "proc/modules", "loop 69632 0 - Live 0x0000000000000000\n")
	return root
}

func TestIsModuleLoaded(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "proc/modules", `nf_tables 356352 0 - Live 0x0000000000000000
lbd 65536 1 - Live 0x0000000000000000
loop 69632 0 - Live 0x0000000000000000
`)
	assert.True(t, isModuleLoaded(root, "lbd"))
	assert.True(t, isModuleLoaded(root, "loop"))
	assert.False(t, isModuleLoaded(root, "lbdctl"))
	assert.False(t, isModuleLoaded(t.TempDir(), "lbd"))
}

func TestProbeOnAHostWithoutLbd(t *testing.T) {
	root := ubuntuRoot(t)

	status, err := Probe(Options{Root: root, DataPath: t.TempDir()})
	require.NoError(t, err)

	assert.False(t, status.Available())
	assert.False(t, status.Loaded)
	assert.False(t, status.ModuleInstalled)
	assert.Nil(t, status.Marker)
	assert.False(t, status.Stale(), "a host that never installed lbd has nothing to rebuild")
	assert.Equal(t, "lbd is not installed", status.Explain())
	assert.NotEmpty(t, status.EmbeddedVersion, "the binary should know which lbd it carries")
}

func TestProbeOnAHealthyHost(t *testing.T) {
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
		BuiltAt:       time.Now(),
	}))

	status, err := Probe(Options{
		Root:       root,
		DataPath:   dataPath,
		SearchPath: []string{"/usr/local/bin"},
	})
	require.NoError(t, err)

	assert.True(t, status.Available())
	assert.False(t, status.Stale())
	assert.Equal(t, "/usr/local/bin/lbdctl", status.LbdctlPath)
	assert.Contains(t, status.Explain(), "is loaded for kernel "+testRelease)
}

func TestLbdctlPresenceAloneIsNotAvailability(t *testing.T) {
	// The bug this guards: shipping lbdctl in the release bundle used to be
	// enough to select accelerator mode, even with no module loaded.
	root := ubuntuRoot(t)
	writeFile(t, root, "usr/local/bin/lbdctl", "")

	status, err := Probe(Options{Root: root, DataPath: t.TempDir(), SearchPath: []string{"/usr/local/bin"}})
	require.NoError(t, err)

	assert.NotEmpty(t, status.LbdctlPath)
	assert.False(t, status.Available())
}

func TestLoadedWithoutControlDeviceIsNotAvailable(t *testing.T) {
	root := ubuntuRoot(t)
	writeFile(t, root, "proc/modules", "lbd 65536 1 - Live 0x0000000000000000\n")
	writeFile(t, root, "usr/local/bin/lbdctl", "")

	status, err := Probe(Options{Root: root, DataPath: t.TempDir(), SearchPath: []string{"/usr/local/bin"}})
	require.NoError(t, err)

	assert.True(t, status.Loaded)
	assert.False(t, status.ControlDevicePresent)
	assert.False(t, status.Available())
	assert.Contains(t, status.Explain(), ControlDevice+" is missing")
}

func TestStaleAfterAKernelUpgrade(t *testing.T) {
	root := ubuntuRoot(t)
	dataPath := t.TempDir()

	// The marker remembers the kernel the module was built for; the host is
	// now running a different one.
	require.NoError(t, writeMarker(dataPath, Marker{
		LbdVersion:    SourceVersion(),
		KernelRelease: "6.8.0-45-generic",
		ModulePath:    modulePath("6.8.0-45-generic"),
		BuiltAt:       time.Now(),
	}))

	status, err := Probe(Options{Root: root, DataPath: dataPath})
	require.NoError(t, err)

	assert.True(t, status.Stale())
	assert.Contains(t, status.Explain(), "built for kernel 6.8.0-45-generic")
	assert.Contains(t, status.Explain(), testRelease+" this host is running")
}

func TestALoadedButStaleModuleDoesNotReadAsHealthy(t *testing.T) {
	// Upgrading miren to a build carrying a newer lbd leaves the old module
	// loaded and working. Reporting only "is loaded" would read as healthy
	// while a rebuild is pending.
	root := ubuntuRoot(t)
	dataPath := t.TempDir()
	writeFile(t, root, "proc/modules", "lbd 65536 1 - Live 0x0000000000000000\n")
	writeFile(t, root, ControlDevice, "")
	writeFile(t, root, modulePath(testRelease), "")
	writeFile(t, root, "usr/local/bin/lbdctl", "")
	require.NoError(t, writeMarker(dataPath, Marker{
		LbdVersion:    "v0.0.0-20250101000000-000000000000",
		KernelRelease: testRelease,
		ModulePath:    modulePath(testRelease),
	}))

	status, err := Probe(Options{Root: root, DataPath: dataPath, SearchPath: []string{"/usr/local/bin"}})
	require.NoError(t, err)

	require.True(t, status.Available())
	require.True(t, status.Stale())
	assert.Contains(t, status.Explain(), "miren now bundles lbd")
	assert.Contains(t, status.Explain(), "v0.0.0-20250101000000-000000000000")
}

func TestStaleWhenMirenCarriesANewerLbd(t *testing.T) {
	root := ubuntuRoot(t)
	dataPath := t.TempDir()
	writeFile(t, root, modulePath(testRelease), "")

	require.NoError(t, writeMarker(dataPath, Marker{
		LbdVersion:    "v0.0.0-20250101000000-000000000000",
		KernelRelease: testRelease,
		ModulePath:    modulePath(testRelease),
		BuiltAt:       time.Now(),
	}))

	status, err := Probe(Options{Root: root, DataPath: dataPath})
	require.NoError(t, err)
	assert.True(t, status.Stale())
}

func TestStaleWhenTheModuleFileWentAway(t *testing.T) {
	root := ubuntuRoot(t)
	dataPath := t.TempDir()

	require.NoError(t, writeMarker(dataPath, Marker{
		LbdVersion:    SourceVersion(),
		KernelRelease: testRelease,
		ModulePath:    modulePath(testRelease),
		BuiltAt:       time.Now(),
	}))

	status, err := Probe(Options{Root: root, DataPath: dataPath})
	require.NoError(t, err)
	assert.True(t, status.Stale())
	assert.Contains(t, status.Explain(), "is gone")
}

func TestMarkerRoundTrip(t *testing.T) {
	dataPath := t.TempDir()

	marker, err := readMarker(dataPath)
	require.NoError(t, err)
	assert.Nil(t, marker, "no record means the host never installed lbd")

	want := Marker{
		LbdVersion:    "v0.0.0-20260824210626-be4cec661034",
		KernelRelease: testRelease,
		ModulePath:    modulePath(testRelease),
		LbdctlPath:    "/usr/local/bin/lbdctl",
		BuiltAt:       time.Now().UTC().Truncate(time.Second),
	}
	require.NoError(t, writeMarker(dataPath, want))

	got, err := readMarker(dataPath)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, want, *got)

	require.NoError(t, removeMarker(dataPath))
	got, err = readMarker(dataPath)
	require.NoError(t, err)
	assert.Nil(t, got)

	// Removing an absent record is not an error.
	require.NoError(t, removeMarker(dataPath))
}

func TestCorruptMarkerIsAnError(t *testing.T) {
	dataPath := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dataPath, "lbd"), 0755))
	require.NoError(t, os.WriteFile(markerPath(dataPath), []byte("{not json"), 0644))

	// Reported rather than treated as absent: silently discarding it would
	// strand a module that really is installed.
	_, err := readMarker(dataPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "corrupt")

	_, err = Probe(Options{Root: ubuntuRoot(t), DataPath: dataPath})
	require.Error(t, err)
}

func TestFindLbdctlPrefersTheSearchPath(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "var/lib/miren/release/lbdctl", "")

	assert.Equal(t, "/var/lib/miren/release/lbdctl",
		findLbdctl(root, []string{"", "/nowhere", "/var/lib/miren/release"}))
	assert.Empty(t, findLbdctl(root, []string{"/nowhere"}))
}

func TestSearchPathDoesNotWriteIntoTheCallersSlice(t *testing.T) {
	// A slice with spare capacity is what makes append dangerous: it writes
	// into the caller's backing array instead of allocating.
	caller := make([]string, 1, 4)
	caller[0] = "/opt/bin"

	opts := Options{SearchPath: caller}
	got := opts.searchPath()

	assert.Equal(t, []string{"/opt/bin", systemReleasePath}, got)
	assert.Equal(t, []string{"/opt/bin"}, caller, "the caller's slice must be untouched")

	// Extending the caller's slice writes into its spare capacity, which is
	// the array searchPath would have appended into. What it returned must not
	// move.
	extended := append(caller, "/clobbered")
	require.Equal(t, "/clobbered", extended[1])
	assert.Equal(t, []string{"/opt/bin", systemReleasePath}, got,
		"the returned slice must not alias the caller's array")
}

func TestMarkerSurvivesATruncatedWrite(t *testing.T) {
	// The record is written through a rename, so a crash mid-write cannot
	// leave a half-file behind. readMarker treats a corrupt record as an
	// error rather than as absent, so a torn write would wedge every probe.
	dataPath := t.TempDir()
	require.NoError(t, writeMarker(dataPath, Marker{
		LbdVersion:    SourceVersion(),
		KernelRelease: testRelease,
		ModulePath:    modulePath(testRelease),
	}))

	// No temporary file is left behind.
	_, err := os.Stat(markerPath(dataPath) + ".tmp")
	assert.True(t, os.IsNotExist(err))

	got, err := readMarker(dataPath)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, testRelease, got.KernelRelease)
}
