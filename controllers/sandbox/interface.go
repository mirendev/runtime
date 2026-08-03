package sandbox

import (
	"context"
	"net/netip"
	"time"

	containerd "github.com/containerd/containerd/v2/client"

	compute "miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/network"
	"miren.dev/runtime/observability"
	"miren.dev/runtime/pkg/controller"
	"miren.dev/runtime/pkg/entity"
)

// SandboxLifecycle defines the interface for sandbox lifecycle management.
// Both SandboxController and SagaSandboxController implement this interface.
type SandboxLifecycle interface {
	Init(ctx context.Context) error
	Create(ctx context.Context, co *compute.Sandbox, meta *entity.Meta) error
	Delete(ctx context.Context, id entity.Id, sb *compute.Sandbox) error
	Close() error
	Periodic(ctx context.Context, timeHorizon time.Duration) error
	SetWriteTracker(wt controller.WriteTracker)
	SetPortStatus(id string, port observability.BoundPort, status observability.PortStatus)
}

// SandboxEntityStore provides entity read/write operations with write tracking.
type SandboxEntityStore interface {
	GetSandbox(ctx context.Context, id string) (*compute.Sandbox, *entity.Meta, error)
	PatchSandbox(ctx context.Context, attrs []entity.Attr, revision int64) (int64, error)
}

// SandboxNetworking provides network allocation and configuration.
type SandboxNetworking interface {
	AllocateNetwork(ctx context.Context, sb *compute.Sandbox) (*network.EndpointConfig, error)
	ReleaseAddr(addr netip.Addr) error
	RebuildEndpointConfig(addresses []string) (*network.EndpointConfig, error)
	BridgeName() string
}

// SandboxContainerRuntime provides containerd container operations.
type SandboxContainerRuntime interface {
	BuildSpec(ctx context.Context, sb *compute.Sandbox, ep *network.EndpointConfig, meta *entity.Meta) ([]containerd.NewContainerOpts, error)
	CreateContainer(ctx context.Context, id string, opts ...containerd.NewContainerOpts) (string, error)
	LoadContainer(ctx context.Context, id string) (containerd.Container, error)
	CleanupContainer(ctx context.Context, cont containerd.Container)
	BootInitialTask(ctx context.Context, sb *compute.Sandbox, ep *network.EndpointConfig, container containerd.Container, shortID string) (containerd.Task, error)
	ConfigureVolumes(ctx context.Context, sb *compute.Sandbox, meta *entity.Meta) (map[string]string, error)
	BootContainers(ctx context.Context, sb *compute.Sandbox, ep *network.EndpointConfig, sbPid int, cgroups map[string]string, meta *entity.Meta, volumeMounts map[string]string) ([]WaitPort, error)
	DestroySubContainers(ctx context.Context, id entity.Id) error
	ReleaseDiskLeases(ctx context.Context, sandboxID entity.Id) error
	ReleaseTokenState(id entity.Id)
	UnconfigureFirewall(sb *compute.Sandbox)
	WaitForPort(ctx context.Context, id string, port int, timeout time.Duration) error
	// DiagnoseListening reports which ports a container is actually listening
	// on, split into routable (reachable from the host) and loopback-only sets.
	// Used on the port-wait timeout path to detect an app that bound a port
	// other than the one Miren configured. ok is false when the container is no
	// longer monitored and its pid is unknown.
	DiagnoseListening(id string) (routable []int, loopback []int, ok bool)
}

// SandboxObservability provides metrics and service management.
type SandboxObservability interface {
	AddMetrics(logEntity string, cgroups map[string]string, attrs map[string]string) error
	RemoveMetrics(logEntity string)
	UpdateServices(ctx context.Context, co *compute.Sandbox, meta *entity.Meta, ep *network.EndpointConfig) error
	// LogSandboxEvent writes a runtime lifecycle message to the
	// sandbox's normal log stream, so `miren logs sandbox <id>`
	// surfaces it alongside container output. Intended for startup
	// or teardown events where a container never produced logs of
	// its own (e.g. volume mount failures).
	LogSandboxEvent(sb *compute.Sandbox, shortID, line string)
}
