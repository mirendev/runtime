package commands

import (
	"context"
	"fmt"
	"time"

	"miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/api/storage/storage_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/idgen"
)

// DebugDiskLease creates a disk lease for testing
func DebugDiskLease(ctx *Context, opts struct {
	ConfigCentric
	DiskID    string `short:"d" long:"disk" description:"Disk ID to lease" required:"true"`
	SandboxID string `short:"s" long:"sandbox" description:"Sandbox ID for the lease"`
	AppID     string `short:"a" long:"app" description:"App ID for the lease"`
	Path      string `short:"p" long:"path" description:"Mount path in sandbox" default:"/data"`
	ReadOnly  bool   `short:"r" long:"readonly" description:"Mount as read-only"`
	Hours     int    `short:"H" long:"hours" description:"Lease duration in hours" default:"2"`
}) error {
	// Use the context's RPC client
	client, err := ctx.RPCClient("entities")
	if err != nil {
		return err
	}

	// Create entity access client
	eac := entityserver_v1alpha.NewEntityAccessClient(client)
	ec := entityserver.NewClient(ctx.Log, eac)

	// Validate disk ID format
	diskId := entity.Id(opts.DiskID)

	// Generate lease ID (format: disk-lease-<base58>)
	leaseId := idgen.GenNS("disk-lease")

	// Create disk lease entity
	lease := &storage_v1alpha.DiskLease{
		DiskId: diskId,
		Status: storage_v1alpha.PENDING,
		Mount: storage_v1alpha.Mount{
			Path:     opts.Path,
			ReadOnly: opts.ReadOnly,
		},
	}

	// Set app ID if provided
	if opts.AppID != "" {
		appId := entity.Id(opts.AppID)
		if len(opts.AppID) < 4 || string(appId)[:4] != "app/" {
			appId = entity.Id("app/" + opts.AppID)
		}
		lease.AppId = appId
	}

	// Create the lease entity
	ctx.Info("Creating disk lease...")
	result, err := ec.Create(ctx, leaseId, lease)
	if err != nil {
		return fmt.Errorf("failed to create disk lease: %w", err)
	}

	ctx.Info("Disk lease created successfully")
	ctx.Info("Lease ID: %s", result)
	ctx.Info("Disk ID: %s", diskId)
	ctx.Info("Mount Path: %s", opts.Path)
	ctx.Info("Read-Only: %v", opts.ReadOnly)

	return nil
}

// DebugDiskLeaseList lists all disk lease entities
func DebugDiskLeaseList(ctx *Context, opts struct {
	ConfigCentric
	DiskID    string `short:"d" long:"disk" description:"Filter by disk ID"`
	SandboxID string `short:"s" long:"sandbox" description:"Filter by sandbox ID"`
	Status    string `long:"status" description:"Filter by status (pending, bound, released, failed)"`
}) error {
	// Use the context's RPC client
	client, err := ctx.RPCClient("entities")
	if err != nil {
		return err
	}

	// Create entity access client
	eac := entityserver_v1alpha.NewEntityAccessClient(client)

	// List disk lease entities
	ref := entity.Ref(entity.EntityKind, storage_v1alpha.KindDiskLease)
	results, err := eac.List(ctx, ref)
	if err != nil {
		return fmt.Errorf("failed to list disk lease entities: %w", err)
	}

	entities := results.Values()
	if len(entities) == 0 {
		ctx.Info("No disk lease entities found")
		return nil
	}

	// Filter if needed
	filtered := entities
	if opts.DiskID != "" || opts.SandboxID != "" || opts.Status != "" {
		filtered = nil
		for _, e := range entities {
			var lease storage_v1alpha.DiskLease
			lease.Decode(e.Entity())

			// Apply filters
			if opts.DiskID != "" {
				if lease.DiskId != entity.Id(opts.DiskID) {
					continue
				}
			}

			if opts.SandboxID != "" {
				if lease.SandboxId != entity.Id(opts.SandboxID) {
					continue
				}
			}

			if opts.Status != "" {
				var statusId storage_v1alpha.DiskLeaseStatus
				switch opts.Status {
				case "pending":
					statusId = storage_v1alpha.PENDING
				case "bound":
					statusId = storage_v1alpha.BOUND
				case "released":
					statusId = storage_v1alpha.RELEASED
				case "failed":
					statusId = storage_v1alpha.FAILED
				default:
					ctx.Warn("Unknown status filter: %s", opts.Status)
					continue
				}
				if lease.Status != statusId {
					continue
				}
			}

			filtered = append(filtered, e)
		}
	}

	if len(filtered) == 0 {
		ctx.Info("No disk leases match the filters")
		return nil
	}

	ctx.Info("Disk leases:")
	ctx.Info("")

	for _, e := range filtered {
		var lease storage_v1alpha.DiskLease
		lease.Decode(e.Entity())

		ctx.Info("ID: %s", lease.ID)
		ctx.Info("  Disk: %s", lease.DiskId)
		ctx.Info("  Sandbox: %s", lease.SandboxId)
		if lease.AppId != "" {
			ctx.Info("  App: %s", lease.AppId)
		}
		ctx.Info("  Status: %s", lease.Status)
		ctx.Info("  Mount Path: %s", lease.Mount.Path)
		ctx.Info("  Read-Only: %v", lease.Mount.ReadOnly)
		if lease.Mount.Options != "" {
			ctx.Info("  Mount Options: %s", lease.Mount.Options)
		}
		ctx.Info("  Acquired: %s", lease.AcquiredAt.Format(time.RFC3339))
		ctx.Info("")
	}

	return nil
}

// DebugDiskLeaseRelease releases a disk lease
func DebugDiskLeaseRelease(ctx *Context, opts struct {
	ConfigCentric
	ID string `short:"i" long:"id" description:"Lease ID to release" required:"true"`
}) error {
	// Use the context's RPC client
	client, err := ctx.RPCClient("entities")
	if err != nil {
		return err
	}

	// Create entity access client
	eac := entityserver_v1alpha.NewEntityAccessClient(client)
	ec := entityserver.NewClient(ctx.Log, eac)

	// Validate lease ID format
	leaseId := entity.Id(opts.ID)

	// Update the lease status to RELEASED
	ctx.Info("Releasing disk lease...")
	lease := &storage_v1alpha.DiskLease{
		Status: storage_v1alpha.RELEASED,
	}

	err = ec.UpdateAttrs(ctx, leaseId, lease.Encode)
	if err != nil {
		return fmt.Errorf("failed to release disk lease: %w", err)
	}

	ctx.Info("Disk lease released: %s", leaseId)
	ctx.Info("The disk lease controller will handle cleanup")

	return nil
}

// DebugDiskLeaseDelete deletes a disk lease entity
func DebugDiskLeaseDelete(ctx *Context, opts struct {
	ConfigCentric
	ID    string `short:"i" long:"id" description:"Lease ID to delete" required:"true"`
	Force bool   `short:"f" long:"force" description:"Force deletion without releasing"`
}) error {
	// Use the context's RPC client
	client, err := ctx.RPCClient("entities")
	if err != nil {
		return err
	}

	// Create entity access client
	eac := entityserver_v1alpha.NewEntityAccessClient(client)
	ec := entityserver.NewClient(ctx.Log, eac)

	// Validate lease ID format
	leaseId := entity.Id(opts.ID)

	if !opts.Force {
		// First release the lease
		ctx.Info("Releasing disk lease before deletion...")
		lease := &storage_v1alpha.DiskLease{
			Status: storage_v1alpha.RELEASED,
		}

		err = ec.UpdateAttrs(context.Background(), leaseId, lease.Encode)
		if err != nil {
			ctx.Warn("Failed to release lease, attempting deletion anyway: %v", err)
		}
	}

	// Delete the lease entity
	ctx.Info("Deleting disk lease entity...")
	err = ec.Delete(context.Background(), leaseId)
	if err != nil {
		return fmt.Errorf("failed to delete disk lease: %w", err)
	}

	ctx.Info("Disk lease deleted: %s", leaseId)

	return nil
}

// DebugDiskLeaseStatus shows detailed status of a specific disk lease
func DebugDiskLeaseStatus(ctx *Context, opts struct {
	ConfigCentric
	ID string `short:"i" long:"id" description:"Lease ID to check" required:"true"`
}) error {
	// Use the context's RPC client
	client, err := ctx.RPCClient("entities")
	if err != nil {
		return err
	}

	// Create entity access client
	eac := entityserver_v1alpha.NewEntityAccessClient(client)

	leaseId := entity.Id(opts.ID)

	// Get the lease entity
	result, err := eac.Get(context.Background(), string(leaseId))
	if err != nil {
		return fmt.Errorf("failed to get disk lease entity: %w", err)
	}

	// Decode lease entity
	var lease storage_v1alpha.DiskLease
	if result.Entity() != nil {
		lease.Decode(result.Entity().Entity())
	} else {
		return fmt.Errorf("disk lease entity not found: %s", leaseId)
	}

	// Display lease information
	ctx.Info("Disk Lease Details:")
	ctx.Info("  ID: %s", lease.ID)
	ctx.Info("  Disk ID: %s", lease.DiskId)
	ctx.Info("  Sandbox ID: %s", lease.SandboxId)
	if lease.AppId != "" {
		ctx.Info("  App ID: %s", lease.AppId)
	}
	ctx.Info("  Status: %s", lease.Status)
	ctx.Info("")
	ctx.Info("Mount Configuration:")
	ctx.Info("  Path: %s", lease.Mount.Path)
	ctx.Info("  Read-Only: %v", lease.Mount.ReadOnly)
	if lease.Mount.Options != "" {
		ctx.Info("  Options: %s", lease.Mount.Options)
	}
	ctx.Info("")
	ctx.Info("Timing:")
	ctx.Info("  Acquired: %s", lease.AcquiredAt.Format(time.RFC3339))

	if lease.NodeId != "" {
		ctx.Info("")
		ctx.Info("Node: %s", lease.NodeId)
	}

	// Check error message if failed
	if lease.Status == storage_v1alpha.FAILED {
		// Note: Error message would be in attributes - this would need entity attribute access
		ctx.Info("")
		ctx.Info("Status: FAILED - Check entity attributes for error details")
	}

	return nil
}
