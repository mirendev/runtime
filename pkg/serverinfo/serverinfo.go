// Package serverinfo describes the running server process: build, instance
// identity, and readiness. One source feeds the Version RPC, the health
// endpoint, and the cloud uplink so they cannot disagree.
package serverinfo

import (
	"maps"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"miren.dev/runtime/pkg/containerenv"
	"miren.dev/runtime/pkg/idgen"
	"miren.dev/runtime/version"
)

// InstallKind is how the process is supervised, which decides how an
// upgrade restarts it: through systemd, or by exiting and letting the
// container runtime's restart policy bring a new container up.
type InstallKind string

const (
	InstallKindSystemd   InstallKind = "systemd"
	InstallKindContainer InstallKind = "container"
	InstallKindUnknown   InstallKind = "unknown"
)

// Info is a snapshot of the server process.
type Info struct {
	Version   string    `json:"version"`
	Commit    string    `json:"commit"`
	BuildDate time.Time `json:"build_date"`

	// InstanceID changes on every restart even when Version does not.
	InstanceID string    `json:"runtime_instance_id"`
	StartedAt  time.Time `json:"started_at"`

	// Ready flips once the boot graph has started every component.
	Ready bool `json:"ready"`

	InstallKind InstallKind `json:"install_kind"`

	// Components are the versions of the runtime pieces this process drives
	// (containerd, runc, ...), as observed at boot. An upgrade that swaps
	// them on disk is only known to have taken when this says so.
	Components map[string]string `json:"components,omitempty"`
}

// Source is created once per process at boot and marked ready when the boot
// graph finishes.
type Source struct {
	instanceID  string
	startedAt   time.Time
	installKind InstallKind
	ready       atomic.Bool

	mu         sync.Mutex
	components map[string]string
}

func New() *Source {
	return &Source{
		instanceID:  idgen.ULID(),
		startedAt:   time.Now().UTC(),
		installKind: detectInstallKind(),
	}
}

func (s *Source) MarkReady() {
	s.ready.Store(true)
}

func (s *Source) InstanceID() string {
	return s.instanceID
}

// SetComponent records the version of a runtime component once the boot
// graph has it running. An empty version is not worth recording.
func (s *Source) SetComponent(name, version string) {
	if name == "" || version == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.components == nil {
		s.components = make(map[string]string)
	}
	s.components[name] = version
}

func (s *Source) Info() Info {
	build := version.GetInfo()
	return Info{
		Version:     build.Version,
		Commit:      build.Commit,
		BuildDate:   build.BuildDate,
		InstanceID:  s.instanceID,
		StartedAt:   s.startedAt,
		Ready:       s.ready.Load(),
		InstallKind: s.installKind,
		Components:  s.componentsCopy(),
	}
}

func (s *Source) componentsCopy() map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.components) == 0 {
		return nil
	}
	return maps.Clone(s.components)
}

// systemd wins over the container check: if systemd started this process,
// `systemctl restart` works wherever that systemd lives (including a
// systemd-in-docker test host). A real container install runs miren as the
// container's own PID 1, with no INVOCATION_ID.
func detectInstallKind() InstallKind {
	if os.Getenv("INVOCATION_ID") != "" {
		return InstallKindSystemd
	}
	if containerenv.InContainer() {
		return InstallKindContainer
	}
	return InstallKindUnknown
}
