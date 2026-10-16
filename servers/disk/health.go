package disk

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"github.com/mr-tron/base58"

	"miren.dev/runtime/api/disk/disk_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/api/storage/storage_v1alpha"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/rpc"
)

// HealthServer exposes only lifecycle diagnostics, independently of disk backup
// and its filesystem/cloud dependencies.
type HealthServer struct {
	log *slog.Logger
	eac *entityserver_v1alpha.EntityAccessClient
}

func NewHealthServer(log *slog.Logger, eac *entityserver_v1alpha.EntityAccessClient) *HealthServer {
	return &HealthServer{log: log.With("module", "disk-health"), eac: eac}
}

var _ disk_v1alpha.DiskHealth = (*HealthServer)(nil)

func (s *HealthServer) List(ctx context.Context, call *disk_v1alpha.DiskHealthList) error {
	// The method policy grants cluster readers, not app identities. Keep the
	// boundary here too: no app argument could confine this cluster-wide scan.
	if rpc.BoundApp(ctx) != "" {
		return rpc.ErrUnauthorized
	}
	// Runner certificates share the internal certificate authorizer bypass,
	// but grant no operator access to cluster-wide resource diagnostics.
	if id := rpc.IdentityFromContext(ctx); id != nil && id.Method == rpc.AuthMethodCert && strings.HasPrefix(id.Subject, "runner-") {
		return rpc.ErrUnauthorized
	}
	args := call.Args()
	var kind entity.Id
	switch args.Kind() {
	case "disk":
		kind = storage_v1alpha.KindDisk
	case "disk_volume":
		kind = storage_v1alpha.KindDiskVolume
	default:
		return cond.ValidationFailure("invalid-field", "kind must be disk or disk_volume")
	}
	if args.Limit() < 0 || len(args.Cursor()) > 4096 {
		return cond.ValidationFailure("invalid-field", "invalid health page limit or cursor")
	}
	if args.Cursor() != "" {
		if _, err := base58.Decode(args.Cursor()); err != nil {
			return cond.ValidationFailure("invalid-field", "invalid health cursor")
		}
	}
	limit := args.Limit()
	if limit == 0 || limit > 200 {
		limit = 200
	}
	page, err := s.eac.ListPage(ctx, entity.Ref(entity.EntityKind, kind), args.Cursor(), limit)
	if err != nil {
		// Internal errors can contain paths or data from a corrupt record.
		s.log.Error("resource health scan failed", "kind", args.Kind(), "error", err)
		return errors.New("resource health scan unavailable")
	}
	if page.Incomplete() {
		s.log.Error("resource health scan omitted corrupt records", "kind", args.Kind())
		return errors.New("resource health scan unavailable")
	}
	resources := make([]*disk_v1alpha.ResourceHealth, 0, len(page.Values()))
	for _, entry := range page.Values() {
		info := new(disk_v1alpha.ResourceHealth)
		switch kind {
		case storage_v1alpha.KindDisk:
			var disk storage_v1alpha.Disk
			disk.Decode(entry.Entity())
			info.SetId(disk.ID.String())
			info.SetDiskId(disk.ID.String())
			info.SetState(string(disk.Status))
			if disk.Status == storage_v1alpha.ERROR {
				info.SetDiagnostic("disk lifecycle failed; inspect server logs")
			}
		case storage_v1alpha.KindDiskVolume:
			var volume storage_v1alpha.DiskVolume
			volume.Decode(entry.Entity())
			info.SetId(volume.ID.String())
			info.SetDiskId(volume.DiskId.String())
			info.SetState(string(volume.ActualState))
			if volume.ActualState == storage_v1alpha.DV_ERROR {
				// Controller errors may embed commands, paths and credentials.
				info.SetDiagnostic("volume lifecycle failed; inspect server logs")
			}
		}
		resources = append(resources, info)
	}
	call.Results().SetResources(resources)
	call.Results().SetCursor(page.Cursor())
	return nil
}
