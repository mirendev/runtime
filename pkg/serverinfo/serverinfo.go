// Package serverinfo describes the running server process: build, instance
// identity, and readiness. One source feeds the Version RPC, the health
// endpoint, and the cloud uplink so they cannot disagree.
package serverinfo

import (
	"crypto/rand"
	"os"
	"sync/atomic"
	"time"

	"github.com/oklog/ulid/v2"
	"miren.dev/runtime/pkg/containerenv"
	"miren.dev/runtime/version"
)

// InstallKind is how the process is supervised, which decides whether an
// upgrade can restart it. Container installs are not upgradable yet (MIR-882).
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
}

// Source is created once per process at boot and marked ready when the boot
// graph finishes.
type Source struct {
	instanceID  string
	startedAt   time.Time
	installKind InstallKind
	ready       atomic.Bool
}

func New() *Source {
	return &Source{
		instanceID:  ulid.MustNew(ulid.Now(), rand.Reader).String(),
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
	}
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
