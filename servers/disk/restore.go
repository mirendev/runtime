package disk

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"miren.dev/runtime/api/disk/disk_v1alpha"
	"miren.dev/runtime/components/diskio"
	"miren.dev/runtime/pkg/cond"
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

	src, total, closeSrc, err := s.restoreSource(ctx, name, args, prog)
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
		if retErr == nil {
			return
		}
		if target.Created && target.Cleanup != nil {
			if cerr := target.Cleanup(ctx); cerr != nil {
				s.log.Warn("failed to clean up after restore", "disk", name, "error", cerr)
			}
		}
		// A refusal is the one failure the client will not come back from, so
		// the uploaded bytes are already dead. Drop them now rather than leave
		// a copy of the snapshot sitting there until the sweep, which a
		// repeatedly-refused restore would do once per attempt.
		s.discardRefusedUpload(args, retErr)
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

	if err := s.writeImage(target.ImagePath, src, total, meta, prog); err != nil {
		return err
	}

	if target.Finalize != nil {
		if err := target.Finalize(ctx); err != nil {
			return err
		}
	}

	// Installed, so the uploaded copy has served its purpose. Only now: until
	// the image is in place, those bytes are the only copy on this host and a
	// retry would have to send them again.
	if id := args.TransferId(); id != "" && args.RestorePoint() == "" {
		s.discardTransfer(id)
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

// discardRefusedUpload drops an uploaded snapshot the server has decided it
// will not use.
//
// The distinction that matters is refusal versus interruption. An interrupted
// upload is exactly what the transfer file is for and must survive; a refused
// one will never be picked up, because the client does not retry a refusal.
func (s *Server) discardRefusedUpload(args *disk_v1alpha.DiskBackupRestoreArgs, err error) {
	if shouldDiscardUpload(args.TransferId(), args.RestorePoint(), err) {
		s.discardTransfer(args.TransferId())
	}
}

func shouldDiscardUpload(transferID, restorePoint string, err error) bool {
	if transferID == "" || restorePoint != "" {
		return false
	}
	var refusal cond.ErrValidationFailure
	return errors.As(err, &refusal)
}

// restoreSource resolves the snapshot to restore from and hands back a reader
// positioned at its start.
//
// The second return is the compressed size when it is known, and 0 when it is
// not, for progress reporting.
func (s *Server) restoreSource(
	ctx context.Context,
	name string,
	args *disk_v1alpha.DiskBackupRestoreArgs,
	prog progressSink,
) (io.Reader, int64, func(), error) {
	if point := args.RestorePoint(); point != "" {
		return s.downloadRestorePoint(ctx, name, point, prog)
	}

	in := args.Data()
	if in == nil {
		return nil, 0, nil, refuse("restore needs either a restore point or a snapshot to read")
	}

	// Land the whole snapshot on disk before touching the image. It is what
	// makes an interrupted upload resumable, and it means a transfer that dies
	// halfway cannot leave a half-decompressed image behind.
	path, err := s.receiveSnapshot(ctx, args.TransferId(), args.Offset(), in, prog)
	if err != nil {
		return nil, 0, nil, err
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, 0, nil, fmt.Errorf("reopening uploaded snapshot: %w", err)
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, 0, nil, fmt.Errorf("stat uploaded snapshot: %w", err)
	}
	return f, info.Size(), func() { f.Close() }, nil
}

// receiveSnapshot appends the client's bytes to this transfer's file and
// returns where it landed.
//
// The file is the checkpoint. Every byte that reaches it is a byte the client
// never has to send again, and fsync before returning is what makes that true
// across a server restart rather than only across a dropped connection.
func (s *Server) receiveSnapshot(
	ctx context.Context,
	transferID string,
	offset int64,
	in *stream.RecvStreamClient[[]byte],
	prog progressSink,
) (string, error) {
	if transferID == "" {
		return "", refuse("uploading a snapshot needs a transfer id, so an interrupted upload can be resumed")
	}

	s.sweepTransfers()

	f, err := s.openTransfer(transferID, offset)
	if err != nil {
		return "", err
	}
	defer f.Close()

	if offset > 0 {
		prog.Message("Resuming upload at %d bytes", offset)
	} else {
		prog.Message("Reading snapshot from client")
	}

	// The stream belongs to the caller; closing our reader would tear their
	// client down, so leave it to them.
	r := stream.ToReader(ctx, in)

	// Total is unknown — the client has not said how big its snapshot is — so
	// progress shows bytes moved rather than a percentage.
	written, err := io.Copy(f, s.trackReads(r, prog, offset, 0))

	// Flush whatever did arrive before reporting either way. On the failure
	// path this is the entire point: unsynced bytes would be re-sent for no
	// reason, and worse, a length the client trusts might not survive a crash.
	if serr := f.Sync(); serr != nil && err == nil {
		err = fmt.Errorf("flushing uploaded snapshot: %w", serr)
	}
	if err != nil {
		return "", fmt.Errorf("receiving snapshot after %d bytes: %w", written, err)
	}

	return f.Name(), nil
}

// downloadRestorePoint opens a restore point from miren.cloud.
//
// This one is not resumable here: the bytes come over the cloud's own HTTPS
// connection rather than the client's RPC session, so an interruption is
// between the server and the cloud, and re-fetching costs the operator nothing
// but time.
func (s *Server) downloadRestorePoint(
	ctx context.Context,
	name, point string,
	prog progressSink,
) (io.Reader, int64, func(), error) {
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

	if err := snapshot.RestoreImage(out, s.trackReads(src, prog, 0, compressedSize), meta); err != nil {
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
