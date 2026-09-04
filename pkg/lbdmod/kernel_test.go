package lbdmod

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeFile creates path under root, making its parents.
func writeFile(t *testing.T, root, path, content string) {
	t.Helper()
	full := filepath.Join(root, path)
	require.NoError(t, os.MkdirAll(filepath.Dir(full), 0755))
	require.NoError(t, os.WriteFile(full, []byte(content), 0644))
}

func TestParseCompiler(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  Compiler
	}{
		{
			name:  "ubuntu gcc",
			input: "Linux version 6.8.0-51-generic (buildd@lcy02) (x86_64-linux-gnu-gcc-13 (Ubuntu 13.3.0-6ubuntu2~24.04) 13.3.0, GNU ld (GNU Binutils) 2.42) #52-Ubuntu SMP\n",
			want:  Compiler{Name: "gcc", Major: 13},
		},
		{
			name:  "fedora gcc",
			input: "Linux version 6.11.4-301.fc41.x86_64 (mockbuild@) (gcc (GCC) 14.2.1 20240912, GNU ld version 2.43.1) #1 SMP\n",
			want:  Compiler{Name: "gcc", Major: 14},
		},
		{
			name:  "clang built",
			input: "Linux version 6.6.30-android14 (build@) (Android (11368139) clang version 17.0.4, LLD 17.0.4) #1 SMP\n",
			want:  Compiler{Name: "clang", Major: 17},
		},
		{
			name:  "unrecognized",
			input: "Linux version 5.10.0 (someone@somewhere) #1 SMP\n",
			want:  Compiler{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, parseCompiler(tt.input))
		})
	}
}

func TestCompilerString(t *testing.T) {
	assert.Equal(t, "gcc-13", Compiler{Name: "gcc", Major: 13}.String())
	assert.Equal(t, "clang", Compiler{Name: "clang"}.String())
	assert.Equal(t, "unknown", Compiler{}.String())
}

func TestParseOSRelease(t *testing.T) {
	id, like := parseOSRelease(`NAME="Ubuntu"
ID=ubuntu
ID_LIKE=debian
VERSION_ID="24.04"
`)
	assert.Equal(t, "ubuntu", id)
	assert.Equal(t, []string{"debian"}, like)

	id, like = parseOSRelease("ID=fedora\n")
	assert.Equal(t, "fedora", id)
	assert.Empty(t, like)

	id, like = parseOSRelease(`ID="rocky"
ID_LIKE="rhel centos fedora"
`)
	assert.Equal(t, "rocky", id)
	assert.Equal(t, []string{"rhel", "centos", "fedora"}, like)
}

func TestDetectHostReadsFixtureRoot(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "proc/sys/kernel/osrelease", "6.8.0-51-generic\n")
	writeFile(t, root, "proc/version",
		"Linux version 6.8.0-51-generic (buildd@lcy02) (x86_64-linux-gnu-gcc-13 (Ubuntu 13.3.0) 13.3.0) #52-Ubuntu SMP\n")
	writeFile(t, root, "etc/os-release", "ID=ubuntu\nID_LIKE=debian\n")
	writeFile(t, root, "lib/modules/6.8.0-51-generic/build/Makefile", "# kernel build tree\n")

	host, err := DetectHost(root)
	require.NoError(t, err)

	assert.Equal(t, "6.8.0-51-generic", host.KernelRelease)
	assert.Equal(t, "/lib/modules/6.8.0-51-generic/build", host.HeadersDir)
	assert.Equal(t, Compiler{Name: "gcc", Major: 13}, host.Compiler)
	assert.Equal(t, "ubuntu", host.DistroID)
	assert.Equal(t, []string{"debian"}, host.DistroLike)
}

func TestDetectHostWithoutHeaders(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "proc/sys/kernel/osrelease", "6.11.4-301.fc41.x86_64\n")
	writeFile(t, root, "etc/os-release", "ID=fedora\n")

	host, err := DetectHost(root)
	require.NoError(t, err)

	// Missing headers are reported, not an error: the builder may be able to
	// fetch them instead.
	assert.Empty(t, host.HeadersDir)
	assert.Equal(t, "kernel-devel-6.11.4-301.fc41.x86_64", host.HeaderPackage())
	assert.Equal(t, "run: dnf install kernel-devel-6.11.4-301.fc41.x86_64", host.InstallHint())
}

func TestDetectHostFindsFedoraStyleHeaders(t *testing.T) {
	root := t.TempDir()
	release := "6.11.4-301.fc41.x86_64"
	writeFile(t, root, "proc/sys/kernel/osrelease", release+"\n")
	writeFile(t, root, "etc/os-release", "ID=fedora\n")
	// Fedora ships the build tree here and does not always leave the
	// /lib/modules/<rel>/build symlink behind.
	writeFile(t, root, "usr/src/kernels/"+release+"/Makefile", "# kernel build tree\n")

	host, err := DetectHost(root)
	require.NoError(t, err)
	assert.Equal(t, "/usr/src/kernels/"+release, host.HeadersDir)
}

func TestDetectHostFallsBackToUsrLibOSRelease(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "proc/sys/kernel/osrelease", "6.8.0-51-generic\n")
	writeFile(t, root, "usr/lib/os-release", "ID=debian\n")

	host, err := DetectHost(root)
	require.NoError(t, err)
	assert.Equal(t, "debian", host.DistroID)
}

func TestDetectHostNeedsAKernelRelease(t *testing.T) {
	_, err := DetectHost(t.TempDir())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no kernel release")
}

func TestHeaderPackageUsesTheDistroFamily(t *testing.T) {
	// A derivative distro we do not name explicitly still gets the right
	// package via ID_LIKE.
	h := Host{KernelRelease: "6.8.0-51", DistroID: "pop", DistroLike: []string{"ubuntu", "debian"}}
	assert.Equal(t, "linux-headers-6.8.0-51", h.HeaderPackage())
	assert.Equal(t, "run: apt-get install linux-headers-6.8.0-51", h.InstallHint())
}

func TestHeaderPackageUnknownDistro(t *testing.T) {
	h := Host{KernelRelease: "6.8.0-51", DistroID: "somethingelse"}
	assert.Empty(t, h.HeaderPackage())
	assert.Equal(t, "install the kernel headers for 6.8.0-51 and try again", h.InstallHint())
}

func TestKernelReleaseErrorNamesTheRealCause(t *testing.T) {
	// A fixture root with no procfs file at all: the error has to carry the
	// read failure, not a nil wrapped by %w.
	_, err := kernelRelease(t.TempDir())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no kernel release")
	assert.NotContains(t, err.Error(), "%!w", "an error was wrapped that was nil")
}

func TestKernelReleaseRejectsAnEmptyOsrelease(t *testing.T) {
	// The file exists but says nothing. Before, err was nil here and %w
	// rendered as %!w(<nil>), telling the operator nothing.
	root := t.TempDir()
	writeFile(t, root, "proc/sys/kernel/osrelease", "\n")

	_, err := kernelRelease(root)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "is empty")
	assert.NotContains(t, err.Error(), "%!w")
}
