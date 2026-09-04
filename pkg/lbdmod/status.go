package lbdmod

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// ModuleName is what the module is called once loaded, and the name
	// modprobe takes.
	ModuleName = "lbd"

	// ControlDevice is the misc device the module registers. Its presence is
	// the only trustworthy proof that the module is loaded and working --
	// lbdctl being on PATH proves nothing.
	ControlDevice = "/dev/lbd-control"

	// markerName records what was built and for which kernel, so a later boot
	// can tell a stale module from a missing one.
	markerName = "installed.json"
)

// Marker is the record left behind by a successful install. Its presence means
// this host has opted into accelerator mode, which is what licenses miren to
// rebuild the module unattended after a kernel upgrade.
type Marker struct {
	// LbdVersion is the miren.dev/lbd version the source came from.
	LbdVersion string `json:"lbd_version"`

	// KernelRelease is the kernel the module was built for. A module built
	// for one release will not load on another.
	KernelRelease string `json:"kernel_release"`

	// ModulePath is where the built module was installed.
	ModulePath string `json:"module_path"`

	// LbdctlPath is where the built lbdctl was installed.
	LbdctlPath string `json:"lbdctl_path"`

	// BuiltAt is when the build finished.
	BuiltAt time.Time `json:"built_at"`
}

// Status is what miren knows about lbd on this host.
type Status struct {
	// Host is the machine as detected.
	Host Host

	// Loaded is true when the module is in /proc/modules.
	Loaded bool

	// ControlDevicePresent is true when /dev/lbd-control exists. Together
	// with Loaded this is the real availability test.
	ControlDevicePresent bool

	// ModuleInstalled is true when a built module exists for the running
	// kernel, whether or not it is currently loaded.
	ModuleInstalled bool

	// LbdctlPath is where lbdctl was found, or empty.
	LbdctlPath string

	// Marker is the record of the last successful install, or nil if this
	// host has never installed the module.
	Marker *Marker

	// EmbeddedVersion is the miren.dev/lbd version this binary carries.
	EmbeddedVersion string
}

// Available reports whether accelerator mode can actually run right now: the
// module is loaded, its control device is there, and lbdctl exists to drive it.
func (s Status) Available() bool {
	return s.Loaded && s.ControlDevicePresent && s.LbdctlPath != ""
}

// Stale reports whether this host installed the module before but what is on
// disk no longer fits -- almost always because the kernel was upgraded, but
// also when miren itself now carries a newer lbd. Callers use this to decide
// whether to rebuild without being asked.
func (s Status) Stale() bool {
	return s.staleReason() != ""
}

// staleReason names what stopped fitting, phrased to follow "lbd is loaded
// but ...". It is empty when nothing is stale.
func (s Status) staleReason() string {
	if s.Marker == nil {
		return ""
	}
	switch {
	case s.Marker.KernelRelease != s.Host.KernelRelease:
		return fmt.Sprintf("it was built for kernel %s, not the %s this host is running",
			s.Marker.KernelRelease, s.Host.KernelRelease)
	case s.EmbeddedVersion != "" && s.Marker.LbdVersion != s.EmbeddedVersion:
		return fmt.Sprintf("miren now bundles lbd %s and the installed module is %s",
			s.EmbeddedVersion, s.Marker.LbdVersion)
	case !s.ModuleInstalled:
		return fmt.Sprintf("its module file %s is gone", s.Marker.ModulePath)
	}
	return ""
}

// Explain renders the status as a sentence for logs and CLI output.
func (s Status) Explain() string {
	// Concrete faults come first. A module can be loaded without miren having
	// installed it -- by hand, or by a distro package -- and in that case the
	// specific problem is more useful than "not installed".
	stale := s.staleReason()

	switch {
	// A loaded module can still be the wrong one -- most often after miren
	// was upgraded to a build carrying a newer lbd. Saying only that it is
	// loaded would read as healthy while a rebuild is pending.
	case s.Available() && stale != "":
		return "lbd is loaded but " + stale
	case s.Available():
		return fmt.Sprintf("lbd %s is loaded for kernel %s", s.markerVersion(), s.Host.KernelRelease)
	case s.Loaded && !s.ControlDevicePresent:
		return fmt.Sprintf("lbd is loaded but %s is missing", ControlDevice)
	case s.Loaded && s.LbdctlPath == "":
		return "lbd is loaded but lbdctl is missing"
	case s.Marker == nil:
		return "lbd is not installed"
	case stale != "":
		return "lbd is not usable: " + stale
	case !s.Loaded:
		return "lbd is installed but not loaded"
	default:
		return "lbd is installed but lbdctl is missing"
	}
}

func (s Status) markerVersion() string {
	if s.Marker != nil && s.Marker.LbdVersion != "" {
		return s.Marker.LbdVersion
	}
	return s.EmbeddedVersion
}

// isModuleLoaded reports whether the named module appears in /proc/modules.
func isModuleLoaded(root, name string) bool {
	content, err := os.ReadFile(filepath.Join(root, "proc/modules"))
	if err != nil {
		return false
	}
	for line := range strings.SplitSeq(string(content), "\n") {
		fields := strings.Fields(line)
		if len(fields) > 0 && fields[0] == name {
			return true
		}
	}
	return false
}

// modulePath is where a built module for the given kernel lives. "extra" is the
// conventional home for out-of-tree modules and is on depmod's search path.
func modulePath(release string) string {
	return filepath.Join("/lib/modules", release, "extra", ModuleName+".ko")
}

// markerPath is where the install record lives, under miren's data directory.
func markerPath(dataPath string) string {
	return filepath.Join(dataPath, "lbd", markerName)
}

// readMarker loads the install record, returning nil when the host has never
// installed the module. A corrupt marker is reported as an error rather than
// silently treated as absent, since discarding it would strand a module that is
// actually installed.
func readMarker(dataPath string) (*Marker, error) {
	data, err := os.ReadFile(markerPath(dataPath))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading the lbd install record: %w", err)
	}

	var m Marker
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("the lbd install record at %s is corrupt: %w", markerPath(dataPath), err)
	}
	return &m, nil
}

// writeMarker records a successful install.
func writeMarker(dataPath string, m Marker) error {
	path := markerPath(dataPath)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("creating %s: %w", filepath.Dir(path), err)
	}

	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding the lbd install record: %w", err)
	}

	// Written through a rename so a crash mid-write cannot leave a truncated
	// record. readMarker reports a corrupt one as an error rather than
	// treating it as absent, which would otherwise wedge every later probe.
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0644); err != nil {
		return fmt.Errorf("writing %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("installing %s: %w", path, err)
	}
	return nil
}

// removeMarker forgets that lbd was ever installed, so later boots stop
// rebuilding it.
func removeMarker(dataPath string) error {
	err := os.Remove(markerPath(dataPath))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
