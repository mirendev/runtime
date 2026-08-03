package sandbox

import (
	"context"
	"fmt"
	"net/netip"
	"time"

	"github.com/containerd/containerd/namespaces"
	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/errdefs"

	compute "miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/network"
	"miren.dev/runtime/pkg/entity"
)

// sandboxOps adapts a *SandboxController to the saga domain interfaces.
// It is the bridge between the saga actions and the controller's resources.
type sandboxOps struct {
	ctrl *SandboxController
}

// --- SandboxEntityStore ---

func (o *sandboxOps) GetSandbox(ctx context.Context, id string) (*compute.Sandbox, *entity.Meta, error) {
	resp, err := o.ctrl.EAC.Get(ctx, id)
	if err != nil {
		return nil, nil, fmt.Errorf("fetching sandbox %s: %w", id, err)
	}
	var co compute.Sandbox
	co.Decode(resp.Entity().Entity())
	meta := &entity.Meta{
		Entity:   resp.Entity().Entity(),
		Revision: resp.Entity().Revision(),
	}
	return &co, meta, nil
}

func (o *sandboxOps) PatchSandbox(ctx context.Context, attrs []entity.Attr, revision int64) (int64, error) {
	result, err := o.ctrl.EAC.Patch(ctx, attrs, revision)
	if err != nil {
		return 0, err
	}
	if o.ctrl.writeTracker != nil && result.HasRevision() {
		o.ctrl.writeTracker.RecordWrite(result.Revision())
	}
	return result.Revision(), nil
}

// --- SandboxNetworking ---

func (o *sandboxOps) AllocateNetwork(ctx context.Context, sb *compute.Sandbox) (*network.EndpointConfig, error) {
	return o.ctrl.AllocateNetwork(ctx, sb)
}

func (o *sandboxOps) ReleaseAddr(addr netip.Addr) error {
	return o.ctrl.Subnet.ReleaseAddr(addr)
}

func (o *sandboxOps) RebuildEndpointConfig(addresses []string) (*network.EndpointConfig, error) {
	ep := &network.EndpointConfig{
		Bridge: &network.BridgeConfig{
			Name:      o.ctrl.Bridge,
			Addresses: []netip.Prefix{o.ctrl.Subnet.Router()},
		},
	}

	for _, addrStr := range addresses {
		prefix, err := netip.ParsePrefix(addrStr)
		if err != nil {
			return nil, fmt.Errorf("parsing address %q: %w", addrStr, err)
		}
		ep.Addresses = append(ep.Addresses, prefix)
	}

	if err := ep.DeriveDefaultGateway(); err != nil {
		return nil, fmt.Errorf("deriving default gateway: %w", err)
	}

	return ep, nil
}

func (o *sandboxOps) BridgeName() string {
	return o.ctrl.Bridge
}

// --- SandboxContainerRuntime ---

func (o *sandboxOps) BuildSpec(ctx context.Context, sb *compute.Sandbox, ep *network.EndpointConfig, meta *entity.Meta) ([]containerd.NewContainerOpts, error) {
	ctx = namespaces.WithNamespace(ctx, o.ctrl.Namespace)
	return o.ctrl.BuildSpec(ctx, sb, ep, meta)
}

func (o *sandboxOps) CreateContainer(ctx context.Context, id string, opts ...containerd.NewContainerOpts) (string, error) {
	ctx = namespaces.WithNamespace(ctx, o.ctrl.Namespace)
	cid := pauseContainerId(entity.Id(id))
	_, err := o.ctrl.CC.NewContainer(ctx, cid, opts...)
	if err != nil && !errdefs.IsAlreadyExists(err) {
		return "", fmt.Errorf("creating container %s: %w", id, err)
	}
	return cid, nil
}

func (o *sandboxOps) LoadContainer(ctx context.Context, id string) (containerd.Container, error) {
	ctx = namespaces.WithNamespace(ctx, o.ctrl.Namespace)
	return o.ctrl.CC.LoadContainer(ctx, id)
}

func (o *sandboxOps) CleanupContainer(ctx context.Context, cont containerd.Container) {
	ctx = namespaces.WithNamespace(ctx, o.ctrl.Namespace)
	o.ctrl.CleanupContainer(ctx, cont)
}

func (o *sandboxOps) BootInitialTask(ctx context.Context, sb *compute.Sandbox, ep *network.EndpointConfig, container containerd.Container, shortID string) (containerd.Task, error) {
	ctx = namespaces.WithNamespace(ctx, o.ctrl.Namespace)
	return o.ctrl.BootInitialTask(ctx, sb, ep, container, shortID)
}

func (o *sandboxOps) ConfigureVolumes(ctx context.Context, sb *compute.Sandbox, meta *entity.Meta) (map[string]string, error) {
	return o.ctrl.ConfigureVolumes(ctx, sb, meta)
}

func (o *sandboxOps) BootContainers(ctx context.Context, sb *compute.Sandbox, ep *network.EndpointConfig, sbPid int, cgroups map[string]string, meta *entity.Meta, volumeMounts map[string]string) ([]WaitPort, error) {
	return o.ctrl.BootContainers(ctx, sb, ep, sbPid, cgroups, meta, volumeMounts)
}

func (o *sandboxOps) DestroySubContainers(ctx context.Context, id entity.Id) error {
	ctx = namespaces.WithNamespace(ctx, o.ctrl.Namespace)
	return o.ctrl.DestroySubContainers(ctx, id)
}

func (o *sandboxOps) ReleaseDiskLeases(ctx context.Context, sandboxID entity.Id) error {
	return o.ctrl.ReleaseDiskLeases(ctx, sandboxID)
}

func (o *sandboxOps) ReleaseTokenState(id entity.Id) {
	o.ctrl.ReleaseTokenState(id)
}

func (o *sandboxOps) UnconfigureFirewall(sb *compute.Sandbox) {
	o.ctrl.UnconfigureFirewall(sb)
}

func (o *sandboxOps) WaitForPort(ctx context.Context, id string, port int, timeout time.Duration) error {
	return o.ctrl.WaitForPort(ctx, id, port, timeout)
}

// DiagnoseListening reports the ports a container is actually listening on,
// split into routable and loopback-only sets, by inspecting its netns via the
// port monitor. Returns ok=false when monitoring is unavailable or the
// container's pid is unknown.
func (o *sandboxOps) DiagnoseListening(id string) (routable []int, loopback []int, ok bool) {
	if o.ctrl.portMonitor == nil {
		return nil, nil, false
	}
	return o.ctrl.portMonitor.DiagnoseListening(id)
}

// --- SandboxObservability ---

func (o *sandboxOps) AddMetrics(logEntity string, cgroups map[string]string, attrs map[string]string) error {
	return o.ctrl.Metrics.Add(logEntity, cgroups, attrs)
}

func (o *sandboxOps) RemoveMetrics(logEntity string) {
	o.ctrl.Metrics.Remove(logEntity)
}

func (o *sandboxOps) UpdateServices(ctx context.Context, co *compute.Sandbox, meta *entity.Meta, ep *network.EndpointConfig) error {
	return o.ctrl.UpdateServices(ctx, co, meta, ep)
}

func (o *sandboxOps) LogSandboxEvent(sb *compute.Sandbox, shortID, line string) {
	o.ctrl.EmitSandboxEvent(sb, shortID, line)
}
