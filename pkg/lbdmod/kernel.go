package lbdmod

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Host describes the machine we are about to build a module for.
type Host struct {
	// KernelRelease is `uname -r`, e.g. "6.8.0-51-generic". A module built
	// here only loads on this exact release.
	KernelRelease string

	// HeadersDir is the kernel build tree the module must compile against,
	// or empty if the host does not have one installed.
	HeadersDir string

	// Compiler is the toolchain the running kernel was built with, as
	// reported by /proc/version.
	Compiler Compiler

	// DistroID and DistroLike come from /etc/os-release: ID is the specific
	// distribution ("ubuntu"), and DistroLike is its family ("debian"), which
	// is what decides how to name a header package. Either may be empty.
	DistroID   string
	DistroLike []string
}

// Compiler identifies what built the running kernel.
type Compiler struct {
	// Name is "gcc" or "clang", or empty when /proc/version says something
	// we do not recognize.
	Name string

	// Major is the compiler's major version, or 0 if it could not be read.
	Major int
}

// String renders the compiler for logs and error messages.
func (c Compiler) String() string {
	switch {
	case c.Name == "":
		return "unknown"
	case c.Major == 0:
		return c.Name
	default:
		return fmt.Sprintf("%s-%d", c.Name, c.Major)
	}
}

// HeaderPackage names the distro package that provides the build tree for this
// kernel, so an error message can tell the operator exactly what to install.
// Returns an empty string when the distribution is not one we recognize.
func (h Host) HeaderPackage() string {
	for _, id := range append([]string{h.DistroID}, h.DistroLike...) {
		switch id {
		case "debian", "ubuntu":
			return "linux-headers-" + h.KernelRelease
		case "fedora", "rhel", "centos":
			return "kernel-devel-" + h.KernelRelease
		case "arch":
			return "linux-headers"
		case "alpine":
			return "linux-headers"
		case "suse", "opensuse", "opensuse-leap", "opensuse-tumbleweed", "sles":
			return "kernel-devel"
		}
	}
	return ""
}

// CanFetchHeaders reports whether the builder can install kernel headers for
// itself rather than borrowing the host's.
//
// The builder image is Debian-based, so it can only reach a Debian-family
// archive. On any other distribution the operator has to install the headers,
// which is what InstallHint tells them to do. Even on a Debian-family host the
// fetch can still come up empty -- a kernel that has aged out of the archive,
// or a Debian host whose package is not in the builder's Ubuntu sources -- and
// the builder reports that itself, naming the package it could not find.
func (h Host) CanFetchHeaders() bool {
	return h.hasFamily("debian") || h.hasFamily("ubuntu")
}

// InstallHint is the sentence to show an operator whose host has no kernel
// build tree.
func (h Host) InstallHint() string {
	pkg := h.HeaderPackage()
	if pkg == "" {
		return fmt.Sprintf("install the kernel headers for %s and try again", h.KernelRelease)
	}
	switch {
	case h.hasFamily("debian"):
		return "run: apt-get install " + pkg
	case h.hasFamily("fedora"), h.hasFamily("rhel"), h.hasFamily("centos"):
		return "run: dnf install " + pkg
	case h.hasFamily("arch"):
		return "run: pacman -S " + pkg
	case h.hasFamily("alpine"):
		return "run: apk add " + pkg
	case h.hasFamily("suse"), h.hasFamily("sles"):
		return "run: zypper install " + pkg
	}
	return "install " + pkg + " and try again"
}

func (h Host) hasFamily(id string) bool {
	if h.DistroID == id {
		return true
	}
	for _, like := range h.DistroLike {
		if like == id {
			return true
		}
	}
	return false
}

// headerCandidates lists where a kernel build tree may live, in the order to
// try. Debian and Ubuntu populate /lib/modules/<rel>/build; Fedora ships the
// tree under /usr/src/kernels and does not always leave that symlink behind.
func headerCandidates(release string) []string {
	return []string{
		filepath.Join("/lib/modules", release, "build"),
		filepath.Join("/usr/src/kernels", release),
		filepath.Join("/usr/src", "linux-headers-"+release),
	}
}

// findHeaders returns the first candidate that looks like a usable kernel build
// tree, or an empty string. Makefile is the file the module's own build invokes,
// so its absence means the tree is unusable however complete it otherwise looks.
func findHeaders(root, release string) string {
	for _, dir := range headerCandidates(release) {
		if _, err := os.Stat(filepath.Join(root, dir, "Makefile")); err == nil {
			return dir
		}
	}
	return ""
}

// compilerPattern matches the toolchain stanza /proc/version carries, e.g.
// "(gcc-13 (Ubuntu 13.3.0-6ubuntu2~24.04) 13.3.0, ...)" or "(clang version 18.1.3".
var compilerPattern = regexp.MustCompile(`\b(gcc|clang)\b[^0-9]*([0-9]+)`)

// parseCompiler pulls the building toolchain out of a /proc/version line.
func parseCompiler(procVersion string) Compiler {
	m := compilerPattern.FindStringSubmatch(procVersion)
	if m == nil {
		return Compiler{}
	}
	major, err := strconv.Atoi(m[2])
	if err != nil {
		return Compiler{Name: m[1]}
	}
	return Compiler{Name: m[1], Major: major}
}

// parseOSRelease reads the ID and ID_LIKE fields of an os-release file. Values
// may be quoted, and ID_LIKE is a space-separated list ordered most-specific
// first.
func parseOSRelease(content string) (id string, like []string) {
	for _, line := range strings.Split(content, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		value = strings.Trim(value, `"'`)
		switch key {
		case "ID":
			id = value
		case "ID_LIKE":
			like = strings.Fields(value)
		}
	}
	return id, like
}

// kernelRelease reports `uname -r`. It reads the procfs file rather than
// shelling out, and falls back to uname(1) only for a real host, since a
// fixture root has no process to ask.
func kernelRelease(root string) (string, error) {
	path := filepath.Join(root, "proc/sys/kernel/osrelease")
	data, readErr := os.ReadFile(path)
	if readErr == nil {
		if release := strings.TrimSpace(string(data)); release != "" {
			return release, nil
		}
		// The file is there but says nothing. Name that, so the error below
		// never wraps a nil.
		readErr = fmt.Errorf("%s is empty", path)
	}

	if root != "/" {
		return "", fmt.Errorf("no kernel release under %s: %w", root, readErr)
	}

	out, unameErr := exec.Command("uname", "-r").Output()
	if unameErr != nil {
		return "", fmt.Errorf("could not determine the kernel release: %w", unameErr)
	}
	release := strings.TrimSpace(string(out))
	if release == "" {
		return "", errors.New("could not determine the kernel release: uname -r said nothing")
	}
	return release, nil
}

// DetectHost inspects the machine miren is running on. root is the filesystem
// to read from, normally "/"; tests pass a fixture directory.
//
// Missing kernel headers are not an error here: whether they can be fetched
// instead is the caller's decision.
func DetectHost(root string) (Host, error) {
	release, err := kernelRelease(root)
	if err != nil {
		return Host{}, err
	}

	h := Host{
		KernelRelease: release,
		HeadersDir:    findHeaders(root, release),
	}

	if data, err := os.ReadFile(filepath.Join(root, "proc/version")); err == nil {
		h.Compiler = parseCompiler(string(data))
	}

	for _, path := range []string{"etc/os-release", "usr/lib/os-release"} {
		data, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			continue
		}
		h.DistroID, h.DistroLike = parseOSRelease(string(data))
		break
	}

	return h, nil
}
