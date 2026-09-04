package disk

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"miren.dev/runtime/api/disk/disk_v1alpha"
	"miren.dev/runtime/components/diskio"
	"miren.dev/runtime/pkg/rpc/stream"
	"miren.dev/runtime/pkg/snapshot"
)

// Restore rebuilds a disk's image, either from a restore point in miren.cloud
// or from a snapshot the client streams up.
//
// Restoring a disk this cluster has never seen creates it, which is the case
// that matters for disaster recovery: the host is gone and the whole task is to
// rebuild onto a new one.
func (s *Server) Restore(ctx context.Context, state *disk_v1alpha.DiskBackupRestore) (retErr error) {
	args := state.Args()
	prog := s.newProgress(ctx, args.Progress())

	name := args.Disk()
	if name == "" {
		return refuse("disk name is required")
	}

	src, compressedSize, closeSrc, err := s.restoreSource(ctx, name, args, prog)
	if err != nil {
		return err
	}
	defer closeSrc()

	meta, err := snapshot.ReadHeader(src)
	if err != nil {
		return fmt.Errorf("reading snapshot header: %w", err)
	}

	target, err := snapshot.PrepareRestore(ctx, s.disks, name, s.dataPath,
		snapshot.WithCreator(s.disks, meta.SizeBytes, meta.Filesystem))
	if err != nil {
		return err
	}

	// Roll the entities back if anything below fails, but only when this
	// restore is what created them.
	defer func() {
		if retErr != nil && target.Created && target.Cleanup != nil {
			if cerr := target.Cleanup(ctx); cerr != nil {
				s.log.Warn("failed to clean up after restore", "disk", name, "error", cerr)
			}
		}
	}()

	if err := s.refuseLiveImage(target, name); err != nil {
		return err
	}

	if !target.Created {
		if _, err := os.Stat(target.ImagePath); err == nil && !args.Force() {
			return refuse(
				"disk %q already has an image at %s — pass --force to overwrite it",
				name, target.ImagePath,
			)
		}
	}

	prog.Message("Restoring %s (%d bytes)", name, meta.SizeBytes)

	if err := s.writeImage(target.ImagePath, src, compressedSize, meta, prog); err != nil {
		return err
	}

	if target.Finalize != nil {
		if err := target.Finalize(ctx); err != nil {
			return err
		}
	}

	s.log.Info("restored disk",
		"disk", name,
		"image_size", meta.SizeBytes,
		"created", target.Created,
	)

	res := new(disk_v1alpha.RestoreResult)
	res.SetDisk(name)
	res.SetImageSizeBytes(meta.SizeBytes)
	res.SetCreated(target.Created)
	state.Results().SetResult(&res)
	return nil
}

// restoreSource opens the snapshot to restore from, whichever end it came from.
//
// The second return is the compressed size when it is known, and 0 when it is
// not. Progress is measured in compressed bytes because that is what actually
// moves; a client streaming a snapshot up has not told us how big it is, so
// there is nothing honest to show a percentage against.
func (s *Server) restoreSource(
	ctx context.Context,
	name string,
	args *disk_v1alpha.DiskBackupRestoreArgs,
	prog progressSink,
) (io.Reader, int64, func(), error) {
	point := args.RestorePoint()
	if point == "" {
		in := args.Data()
		if in == nil {
			return nil, 0, nil, refuse("restore needs either a restore point or a snapshot to read")
		}
		prog.Message("Reading snapshot from client")
		r := stream.ToReader(ctx, in)
		// The stream belongs to the caller; closing our reader would tear
		// their client down, so leave it to them.
		return r, 0, func() {}, nil
	}

	if s.updates == nil {
		return nil, 0, nil, errNoCloud("restoring from a restore point")
	}

	// The restore point lives against the disk's cloud volume, so the disk has
	// to already exist for this path. Creating a disk from a cloud restore
	// point it has no record of is not something this can resolve.
	target, err := s.prepareBackup(ctx, name)
	if err != nil {
		return nil, 0, nil, err
	}
	if target.CloudVolumeID == "" {
		return nil, 0, nil, refuse("disk %q is not registered with miren.cloud, so it has no restore points", name)
	}

	size := s.restorePointSize(ctx, target.CloudVolumeID, point)

	prog.Message("Downloading restore point %s", point)

	body, err := s.updates.Download(ctx, target.CloudVolumeID, point)
	if err != nil {
		return nil, 0, nil, fmt.Errorf("downloading restore point %s: %w", point, err)
	}
	return body, size, func() { body.Close() }, nil
}

// restorePointSize looks up how big a restore point is, so the download can
// show a percentage. Best effort: a failure here costs a progress bar, not a
// restore, so it is logged rather than returned.
func (s *Server) restorePointSize(ctx context.Context, cloudVolumeID, point string) int64 {
	updates, err := s.updates.List(ctx, cloudVolumeID, diskio.ListOptions{
		Kind:       diskio.KindLoopImage,
		Descending: true,
	})
	if err != nil {
		s.log.Debug("could not size restore point", "restore_point", point, "error", err)
		return 0
	}
	for _, u := range updates {
		if u.UpdateID == point {
			return u.Size
		}
	}
	return 0
}

// refuseLiveImage stops a restore that would silently do nothing.
//
// See liveImageDevice: a loop device holds the image's inode, not its path, so
// writing a new image and renaming it into place leaves the running system on
// the old one. There is deliberately no --force for this. --force means
// "overwrite an existing image", and letting it also mean "write over a live
// loop device" would let an operator ask for a no-op and be told it worked.
func (s *Server) refuseLiveImage(target *snapshot.RestoreTarget, name string) error {
	dev, err := s.liveImageDevice(target.ImagePath)
	if err != nil {
		return err
	}
	if dev == "" {
		return nil
	}
	return refuse(
		"disk %q is in use (%s is backing %s), and restoring it now would write an image nothing reads — "+
			"stop everything using the disk first, or restore into a new disk instead",
		name, dev, target.ImagePath,
	)
}

// writeImage decompresses a snapshot into the image path.
//
// It writes to a temp file and renames, so an interrupted restore cannot leave
// a half-written image that looks complete.
func (s *Server) writeImage(imagePath string, src io.Reader, compressedSize int64, meta *snapshot.Meta, prog progressSink) error {
	if err := os.MkdirAll(filepath.Dir(imagePath), 0700); err != nil {
		return fmt.Errorf("creating volume directory: %w", err)
	}

	tmpPath := imagePath + ".restore.tmp"
	out, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("creating image file: %w", err)
	}

	closed := false
	cleanup := true
	defer func() {
		if !closed {
			out.Close()
		}
		if cleanup {
			os.Remove(tmpPath)
		}
	}()

	// Preallocate sparsely so the sparse-aware writer can seek over zero runs.
	if err := out.Truncate(meta.SizeBytes); err != nil {
		return fmt.Errorf("preallocating image: %w", err)
	}

	if err := snapshot.RestoreImage(out, s.trackReads(src, prog, compressedSize), meta); err != nil {
		return err
	}

	if err := out.Sync(); err != nil {
		return fmt.Errorf("flushing image: %w", err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("closing image: %w", err)
	}
	closed = true
	if err := os.Rename(tmpPath, imagePath); err != nil {
		return fmt.Errorf("moving image into place: %w", err)
	}
	cleanup = false
	return nil
}
