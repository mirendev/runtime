package addon

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/api/network/network_v1alpha"
	"miren.dev/runtime/api/storage/storage_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/types"
	"miren.dev/runtime/pkg/idgen"
	"miren.dev/runtime/pkg/saga"
)

// ProviderFramework provides common operations that addon providers need
// when creating backing infrastructure (pools, services, etc).
type ProviderFramework struct {
	EC      *entityserver.Client
	EAC     *entityserver_v1alpha.EntityAccessClient
	Storage saga.Storage
	Log     *slog.Logger
}

// NewProviderFramework creates a new provider framework.
func NewProviderFramework(log *slog.Logger, ec *entityserver.Client, eac *entityserver_v1alpha.EntityAccessClient, storage saga.Storage) *ProviderFramework {
	return &ProviderFramework{
		EC:      ec,
		EAC:     eac,
		Storage: storage,
		Log:     log,
	}
}

// CreateSandboxPoolSpec describes the desired sandbox pool configuration.
type CreateSandboxPoolSpec struct {
	Name             string
	Image            string
	Env              []string
	Ports            []compute_v1alpha.SandboxSpecContainerPort
	DesiredInstances int64
	Labels           types.Labels
	SandboxPrefix    string
	Command          string
	Mounts           []compute_v1alpha.SandboxSpecContainerMount
	Volumes          []compute_v1alpha.SandboxSpecVolume
	// PortWaitTimeout overrides the sandbox controller's port-bind health
	// check budget. Parsed via time.ParseDuration (e.g. "60s"). Empty means
	// use the default (15s). Addons with slow cold-init should set this.
	PortWaitTimeout string
}

// CreateSandboxPool creates a fixed-mode SandboxPool entity.
func (fw *ProviderFramework) CreateSandboxPool(ctx context.Context, spec CreateSandboxPoolSpec) (entity.Id, error) {
	// A caller-supplied Name gives the pool a deterministic identity (so a retry
	// can rediscover a pool a prior attempt created); otherwise generate a random
	// one. The id derives from the name either way.
	name := spec.Name
	if name == "" {
		name = idgen.GenNS("pool")
	}
	poolID := entity.Id("pool/" + name)

	pool := &compute_v1alpha.SandboxPool{
		DesiredInstances: spec.DesiredInstances,
		SandboxLabels:    spec.Labels,
		SandboxPrefix:    spec.SandboxPrefix,
		SandboxSpec: compute_v1alpha.SandboxSpec{
			LogAttribute:    spec.Labels,
			PortWaitTimeout: spec.PortWaitTimeout,
		},
	}

	container := compute_v1alpha.SandboxSpecContainer{
		Name:    "addon",
		Image:   spec.Image,
		Command: spec.Command,
		Env:     spec.Env,
		Port:    spec.Ports,
		Mount:   spec.Mounts,
	}

	pool.SandboxSpec.Container = append(pool.SandboxSpec.Container, container)
	pool.SandboxSpec.Volume = spec.Volumes

	_, err := fw.EAC.Create(ctx, entity.New(
		(&core_v1alpha.Metadata{
			Name:   name,
			Labels: spec.Labels,
		}).Encode,
		entity.DBId, poolID,
		pool.Encode,
	).Attrs())
	if err != nil {
		return "", fmt.Errorf("creating sandbox pool: %w", err)
	}

	return poolID, nil
}

// WaitForPool watches a pool entity until it has at least one ready instance
// or the timeout is reached.
func (fw *ProviderFramework) WaitForPool(ctx context.Context, poolID entity.Id, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ch := fw.EC.WatchEntity(ctx, poolID)

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timed out waiting for pool %s to be ready", poolID)
		case e, ok := <-ch:
			if !ok {
				return fmt.Errorf("pool %s watch closed unexpectedly", poolID)
			}

			var pool compute_v1alpha.SandboxPool
			pool.Decode(e)

			if pool.ReadyInstances > 0 {
				fw.Log.Info("pool ready", "pool", poolID, "ready", pool.ReadyInstances)
				return nil
			}
		}
	}
}

// ScalePool sets the desired instances on a pool.
func (fw *ProviderFramework) ScalePool(ctx context.Context, poolID entity.Id, desired int64) error {
	return fw.EC.Patch(ctx, poolID, 0,
		entity.Int64(compute_v1alpha.SandboxPoolDesiredInstancesId, desired),
	)
}

// DeleteSandboxPool scales a pool to zero and then deletes it.
func (fw *ProviderFramework) DeleteSandboxPool(ctx context.Context, poolID entity.Id) error {
	// Scale to zero first so sandboxes are cleaned up
	if err := fw.ScalePool(ctx, poolID, 0); err != nil {
		fw.Log.Warn("failed to scale pool to zero before delete", "pool", poolID, "error", err)
	}

	return fw.EC.Delete(ctx, poolID)
}

// CreateService creates a network Service entity that routes to pods
// matching the given labels.
func (fw *ProviderFramework) CreateService(ctx context.Context, name string, matchLabels types.Labels, port int64) (entity.Id, error) {
	svc := &network_v1alpha.Service{
		Match: matchLabels,
		Port: []network_v1alpha.Port{
			{
				Port:     port,
				Name:     "default",
				Protocol: network_v1alpha.TCP,
			},
		},
	}

	id, err := fw.EC.Create(ctx, name, svc)
	if err != nil {
		return "", fmt.Errorf("creating service: %w", err)
	}

	return id, nil
}

// DeleteDiskByName finds a disk entity by its Name field and deletes it.
// Returns nil if no disk with that name exists.
func (fw *ProviderFramework) DeleteDiskByName(ctx context.Context, diskName string) error {
	listResp, err := fw.EAC.List(ctx, entity.String(storage_v1alpha.DiskNameId, diskName))
	if err != nil {
		return fmt.Errorf("querying disks by name %q: %w", diskName, err)
	}

	for _, e := range listResp.Values() {
		id := entity.Id(e.Id())
		if err := fw.EC.Delete(ctx, id); err != nil {
			return fmt.Errorf("deleting disk %s: %w", id, err)
		}
		fw.Log.Info("deleted disk", "disk", id, "name", diskName)
	}

	return nil
}

// DeleteService deletes a network Service entity.
func (fw *ProviderFramework) DeleteService(ctx context.Context, serviceID entity.Id) error {
	return fw.EC.Delete(ctx, serviceID)
}

// GetServiceAddress reads the Service entity and returns its first allocated IP.
func (fw *ProviderFramework) GetServiceAddress(ctx context.Context, serviceID entity.Id) (string, error) {
	var svc network_v1alpha.Service
	if err := fw.EC.GetById(ctx, serviceID, &svc); err != nil {
		return "", fmt.Errorf("getting service %s: %w", serviceID, err)
	}

	if len(svc.Ip) == 0 {
		return "", fmt.Errorf("service %s has no IP address assigned", serviceID)
	}

	return svc.Ip[0], nil
}

// WaitForServiceAddress watches a Service entity until it has an IP address
// or the timeout is reached.
func (fw *ProviderFramework) WaitForServiceAddress(ctx context.Context, serviceID entity.Id, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ch := fw.EC.WatchEntity(ctx, serviceID)

	for {
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("timed out waiting for service %s to get an IP", serviceID)
		case e, ok := <-ch:
			if !ok {
				return "", fmt.Errorf("service %s watch closed unexpectedly", serviceID)
			}

			var svc network_v1alpha.Service
			svc.Decode(e)

			if len(svc.Ip) > 0 {
				fw.Log.Info("service address assigned", "service", serviceID, "ip", svc.Ip[0])
				return svc.Ip[0], nil
			}
		}
	}
}
