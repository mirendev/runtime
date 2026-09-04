package disk

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"miren.dev/runtime/api/disk/disk_v1alpha"
	"miren.dev/runtime/api/storage/storage_v1alpha"
	"miren.dev/runtime/components/diskio"
	"miren.dev/runtime/pkg/diskresolve"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/idgen"
	"miren.dev/runtime/pkg/snapshot"
)

// diskDataPath is where volume directories and the soft-delete holding area
// live under the server's data directory.
func (s *Server) diskDataPath() string {
	return filepath.Join(s.dataPath, "disk-data")
}

// ListDeleted reports the disks still recoverable from the soft-delete holding
// area.
func (s *Server) ListDeleted(ctx context.Context, state *disk_v1alpha.DiskBackupListDeleted) error {
	disks, retention, err := s.listDeleted()
	if err != nil {
		return err
	}
	state.Results().SetDisks(disks)
	state.Results().SetRetentionDays(int32(retention))
	return nil
}

func (s *Server) listDeleted() ([]*disk_v1alpha.DeletedDisk, int, error) {
	entries, err := diskio.ListDeletedVolumes(s.diskDataPath())
	if err != nil {
		return nil, 0, fmt.Errorf("listing deleted volumes: %w", err)
	}

	// Newest deletion first: the disk someone wants back is almost always the
	// one they just lost.
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Metadata.DeletedAt.After(entries[j].Metadata.DeletedAt)
	})

	retention := diskio.DefaultDeletedVolumeGCConfig().RetentionDays

	disks := make([]*disk_v1alpha.DeletedDisk, 0, len(entries))
	for _, e := range entries {
		meta := e.Metadata

		d := new(disk_v1alpha.DeletedDisk)
		d.SetDiskName(meta.DiskName)
		d.SetVolumeId(meta.VolumeID)
		d.SetSizeGb(meta.SizeGb)
		d.SetFilesystem(meta.Filesystem)
		d.SetVolumeMode(meta.VolumeMode)
		if ts := timestamp(meta.DeletedAt); ts != nil {
			d.SetDeletedAt(ts)
		}
		expiry := meta.DeletedAt.Add(time.Duration(retention) * 24 * time.Hour)
		if ts := timestamp(expiry); ts != nil {
			d.SetExpiresAt(ts)
		}
		disks = append(disks, d)
	}

	return disks, retention, nil
}

// Undelete moves a deleted volume's data back into place and recreates the
// entities that point at it.
func (s *Server) Undelete(ctx context.Context, state *disk_v1alpha.DiskBackupUndelete) error {
	args := state.Args()

	out, err := s.undelete(ctx, args.Disk(), args.VolumeId())
	if err != nil {
		return err
	}

	res := new(disk_v1alpha.UndeleteResult)
	res.SetDisk(out.name)
	res.SetDiskId(string(out.diskID))
	res.SetVolumeId(out.volumeID)
	res.SetImagePath(out.imagePath)
	state.Results().SetResult(&res)
	return nil
}

// undeleteResult is what a recovery produced, for the caller to report.
type undeleteResult struct {
	name      string
	diskID    entity.Id
	volumeID  string
	imagePath string
}

func (s *Server) undelete(ctx context.Context, name, volumeID string) (_ *undeleteResult, retErr error) {
	if name == "" {
		return nil, refuse("disk name is required")
	}

	entry, err := s.findDeleted(name, volumeID)
	if err != nil {
		return nil, err
	}
	meta := entry.Metadata

	// A live disk already owning this name would end up with two entities
	// answering to it, and every lookup by name is ambiguous from then on.
	//
	// Only an actual absence clears the way. A lookup that merely failed says
	// nothing about whether the name is taken, and treating it as free is how
	// a moment of entity-store trouble becomes the duplicate this check exists
	// to prevent.
	switch _, err := s.disks.FindDisk(ctx, name); {
	case err == nil:
		return nil, refuse("a disk named %q already exists — rename or delete it before recovering this one", name)
	case !errors.As(err, &snapshot.DiskNotFoundError{}):
		return nil, fmt.Errorf("checking whether %q is already taken: %w", name, err)
	}

	filesystem := strings.TrimPrefix(strings.ToLower(meta.Filesystem), "filesystem.")

	diskEntityId, err := s.createRestoringDisk(ctx, meta, filesystem)
	if err != nil {
		return nil, err
	}

	// From here on there are entities to unwind, and unwinding has to survive
	// the thing that made it necessary. A client that disconnects mid-recovery
	// cancels ctx, which is exactly when the cleanup below must still run.
	cleanupCtx := context.WithoutCancel(ctx)

	volID := meta.VolumeID
	destPath := filepath.Join(s.diskDataPath(), "volumes", volID)

	// The runner creates this on startup, but a recovery can be the first thing
	// that happens on a rebuilt host, and rename will not create it.
	if err := os.MkdirAll(filepath.Dir(destPath), 0700); err != nil {
		s.deleteEntity(cleanupCtx, string(diskEntityId), "disk")
		return nil, fmt.Errorf("creating volumes directory: %w", err)
	}

	if err := os.Rename(entry.Path, destPath); err != nil {
		s.deleteEntity(cleanupCtx, string(diskEntityId), "disk")
		return nil, fmt.Errorf("moving volume back to %s: %w", destPath, err)
	}

	// Until the last entity write lands, put everything back: the data returns
	// to the holding area with its metadata intact, so a failed recovery can be
	// retried rather than leaving a volume directory nothing references.
	committed := false
	defer func() {
		if committed {
			return
		}
		if rerr := os.Rename(destPath, entry.Path); rerr != nil {
			s.log.Warn("failed to move volume back to deleted-volumes", "volume_id", volID, "error", rerr)
		}
		s.deleteEntity(cleanupCtx, string(diskEntityId), "disk")
	}()

	imagePath := filepath.Join(destPath, "disk.img")
	if _, err := os.Stat(imagePath); err != nil {
		return nil, refuse("disk image not found at %s — the deleted volume may be corrupted", imagePath)
	}

	nodeId, err := s.disks.FindNodeId(ctx)
	if err != nil {
		s.log.Warn("could not find node, using the one recorded at deletion",
			"disk", name, "node_id", meta.NodeID, "error", err)
		nodeId = meta.NodeID.Id()
	}

	volEntityId := entity.Id("disk_volume/" + volID)
	vol := &storage_v1alpha.DiskVolume{
		Name:         meta.DiskName,
		DiskId:       diskEntityId,
		VolumeId:     volID,
		SizeGb:       meta.SizeGb,
		Filesystem:   filesystem,
		VolumeMode:   storage_v1alpha.DiskVolumeVolumeMode(meta.VolumeMode),
		DesiredState: storage_v1alpha.DV_PRESENT,
		// Start PENDING, not READY. The runner's DiskVolumeController drives
		// the recovered volume through the same mount-then-READY ordering as a
		// fresh create (skipping the reimage, since disk.img is already in
		// place). Advertising DV_READY here would let a lease bind against a
		// volume that isn't registered in the runner's in-memory state yet,
		// which lands the lease in a terminal FAILED (MIR-1469).
		ActualState: storage_v1alpha.DV_PENDING,
		ImagePath:   imagePath,
		NodeId:      nodeId,
	}

	if _, err := s.eac.Create(ctx, entity.New(entity.DBId, volEntityId, vol.Encode).Attrs()); err != nil {
		return nil, fmt.Errorf("creating disk_volume entity: %w", err)
	}

	// PROVISIONING, not PROVISIONED. The DiskController promotes it only once
	// the disk_volume actually reaches DV_READY — after the volume is
	// registered and mounted on the node — so the disk never reports itself
	// leasable ahead of a real mount.
	_, err = s.eac.Patch(ctx, []entity.Attr{
		entity.Ref(entity.DBId, diskEntityId),
		entity.Ref(storage_v1alpha.DiskStatusId, storage_v1alpha.DiskStatusProvisioningId),
		entity.String(storage_v1alpha.DiskVolumeIdId, volID),
	}, 0)
	if err != nil {
		s.deleteEntity(cleanupCtx, string(volEntityId), "disk_volume")
		return nil, fmt.Errorf("updating disk to provisioning: %w", err)
	}

	// Committed. The metadata file only exists to describe a volume while it
	// sits in the holding area, so it goes now that the volume is live again.
	if rerr := os.Remove(filepath.Join(destPath, "metadata.json")); rerr != nil && !os.IsNotExist(rerr) {
		s.log.Warn("failed to remove deleted-volume metadata", "volume_id", volID, "error", rerr)
	}
	committed = true

	s.log.Info("recovered deleted disk",
		"disk", name, "disk_id", diskEntityId, "volume_id", volID)

	return &undeleteResult{
		name:      name,
		diskID:    diskEntityId,
		volumeID:  volID,
		imagePath: imagePath,
	}, nil
}

// findDeleted picks the one deleted volume the caller meant.
func (s *Server) findDeleted(name, volumeID string) (*diskio.DeletedVolumeEntry, error) {
	entries, err := diskio.ListDeletedVolumes(s.diskDataPath())
	if err != nil {
		return nil, fmt.Errorf("listing deleted volumes: %w", err)
	}

	var matches []diskio.DeletedVolumeEntry
	for _, e := range entries {
		if e.Metadata.DiskName != name {
			continue
		}
		if volumeID != "" && e.Metadata.VolumeID != volumeID {
			continue
		}
		matches = append(matches, e)
	}

	switch len(matches) {
	case 0:
		if volumeID != "" {
			return nil, refuse("no deleted disk named %q with volume %s", name, volumeID)
		}
		return nil, refuse("no deleted disk found named %q", name)
	case 1:
		return &matches[0], nil
	}

	ids := make([]string, 0, len(matches))
	for _, m := range matches {
		ids = append(ids, m.Metadata.VolumeID)
	}
	return nil, refuse(
		"several deleted disks are named %q — pass one of these volume ids to say which: %s",
		name, strings.Join(ids, ", "),
	)
}

// createRestoringDisk makes the disk entity in RESTORING so the disk controller
// leaves it alone while the volume is moved back into place.
func (s *Server) createRestoringDisk(
	ctx context.Context,
	meta *diskio.DeletedVolumeMetadata,
	filesystem string,
) (entity.Id, error) {
	disk := &storage_v1alpha.Disk{
		Name:       meta.DiskName,
		SizeGb:     meta.SizeGb,
		Filesystem: diskresolve.ParseFilesystem(filesystem),
		Status:     storage_v1alpha.RESTORING,
	}

	// A fresh id, not the one the disk had before it was deleted. Anything that
	// still holds a reference to the old id is holding a reference to something
	// the operator deleted, and reusing the id would silently reconnect it.
	id, err := s.ec.Create(ctx, idgen.GenNS("disk"), disk)
	if err != nil {
		return "", fmt.Errorf("creating disk entity: %w", err)
	}
	return id, nil
}

// deleteEntity is best-effort cleanup on a path that is already failing, so it
// reports rather than returns: the error that got us here is the one to keep.
func (s *Server) deleteEntity(ctx context.Context, id, kind string) {
	if _, err := s.eac.Delete(ctx, id); err != nil {
		s.log.Warn("failed to clean up entity after a failed recovery",
			"kind", kind, "id", id, "error", err)
	}
}
