package lbdmod

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"

	lbdsrc "miren.dev/runtime/third_party/lbd"
)

// DefaultDataPath is where miren keeps the lbd install record and build
// scratch space.
const DefaultDataPath = "/var/lib/miren"

// Options say where to look. The zero value probes the real host with miren's
// default data directory.
type Options struct {
	// Root is the filesystem to read, normally "" for "/". Tests point this
	// at a fixture directory.
	Root string

	// DataPath is miren's data directory, holding the install record.
	// Defaults to DefaultDataPath.
	DataPath string

	// SearchPath holds extra directories to look for lbdctl in, ahead of
	// PATH. The server's release directory belongs here: it is prepended to
	// containerd's PATH but not to miren's own.
	SearchPath []string
}

// systemReleasePath is searched for lbdctl by default. It is prepended to
// containerd's PATH but not to miren's own, so without this every caller would
// have to remember to add it.
const systemReleasePath = "/var/lib/miren/release"

// HostOptions builds the options for inspecting this host. dataPath is where
// miren keeps its data; empty means DefaultDataPath.
//
// Everything that decides whether a disk gets accelerator mode goes through
// here, because they all have to reach the same answer or a node picks a mode
// it cannot serve. They disagreed before: the CLI searched the release
// directory it resolved through $HOME while the disk controller searched only
// the system one, so a host with lbdctl under ~/.miren/release would have the
// CLI choose accelerator and the controller choose universal.
//
// The rule is now the system release directory and PATH, for every caller.
// Nothing resolves a per-user location, since the CLI and the server run as
// different users and would resolve it differently.
func HostOptions(dataPath string) Options {
	return Options{DataPath: dataPath}
}

func (o Options) root() string {
	if o.Root == "" {
		return "/"
	}
	return o.Root
}

// searchPath returns the directories to look for lbdctl in. Callers that name
// none still get the release directory, so every caller agrees on whether
// lbdctl is present.
//
// This builds a new slice rather than appending to o.SearchPath, which would
// write into the caller's backing array whenever it has spare capacity.
func (o Options) searchPath() []string {
	return slices.Concat(o.SearchPath, []string{systemReleasePath})
}

func (o Options) dataPath() string {
	if o.DataPath == "" {
		return DefaultDataPath
	}
	return o.DataPath
}

// findLbdctl locates the lbdctl binary, checking the caller's directories
// before falling back to PATH.
func findLbdctl(root string, searchPath []string) string {
	for _, dir := range searchPath {
		if dir == "" {
			continue
		}
		path := filepath.Join(dir, "lbdctl")
		if info, err := os.Stat(filepath.Join(root, path)); err == nil && !info.IsDir() {
			return path
		}
	}

	// PATH only makes sense against the real filesystem.
	if root != "/" {
		return ""
	}
	if path, err := exec.LookPath("lbdctl"); err == nil {
		return path
	}
	return ""
}

// Probe reports what miren knows about lbd on this host. It never modifies
// anything, so it is safe to call without root.
func Probe(opts Options) (Status, error) {
	root := opts.root()

	host, err := DetectHost(root)
	if err != nil {
		return Status{}, err
	}

	marker, err := readMarker(opts.dataPath())
	if err != nil {
		return Status{}, err
	}

	installed := false
	if _, err := os.Stat(filepath.Join(root, modulePath(host.KernelRelease))); err == nil {
		installed = true
	}

	_, ctlErr := os.Stat(filepath.Join(root, ControlDevice))

	return Status{
		Host:                 host,
		Loaded:               isModuleLoaded(root, ModuleName),
		ControlDevicePresent: ctlErr == nil,
		ModuleInstalled:      installed,
		LbdctlPath:           findLbdctl(root, opts.searchPath()),
		Marker:               marker,
		EmbeddedVersion:      lbdsrc.Version(),
	}, nil
}

// Available reports whether accelerator mode can run right now: the module is
// loaded, its control device exists, and lbdctl is there to drive it.
//
// This is the check that decides a disk's mode, so it deliberately does not
// read the install record and cannot fail -- unlike Probe, which is for
// explaining the situation to a person. lbdctl being on PATH is not enough on
// its own: miren installs lbdctl alongside the module, so a host that has the
// binary but no loaded module would otherwise be sent down the accelerator
// path and fail at attach time.
func Available(opts Options) bool {
	root := opts.root()

	if !isModuleLoaded(root, ModuleName) {
		return false
	}
	if _, err := os.Stat(filepath.Join(root, ControlDevice)); err != nil {
		return false
	}
	return findLbdctl(root, opts.searchPath()) != ""
}
