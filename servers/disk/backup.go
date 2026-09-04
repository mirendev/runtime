package disk

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"miren.dev/runtime/api/disk/disk_v1alpha"
	"miren.dev/runtime/components/diskio"
	"miren.dev/runtime/pkg/rpc/stream"
	"miren.dev/runtime/pkg/snapshot"
)

// Backup snapshots a disk, either to miren.cloud or down to the client.
func (s *Server) Backup(ctx context.Context, state *disk_v1alpha.DiskBackupBackup) error {
	args := state.Args()
	prog := s.newProgress(ctx, args.Progress())

	target, err := s.prepareBackup(ctx, args.Disk())
	if err != nil {
		return err
	}

	// Backing up an image while its loop device is still writing gives a read
	// whose head and tail come from different moments — weaker than the
	// power-loss state fsck and Postgres's WAL recovery are built for. Say so
	// and continue: the operator is the one deciding this is safe.
	//
	// Unlike restore, this check is best effort. Backup only reads, so being
	// unable to tell whether the disk is in use is a reason to say less, not a
	// reason to refuse.
	if dev, err := s.liveImageDevice(target.ImagePath); err != nil {
		s.log.Warn("could not tell whether disk image is in use", "disk", target.Name, "error", err)
		prog.Warn("Could not tell whether %q is in use, so this backup may not be a point-in-time copy.", target.Name)
	} else if dev != "" {
		s.log.Info("backing up a disk that is in use", "disk", target.Name, "device", dev)
		prog.Warn("Disk %q is in use (%s) and may be written during the backup.", target.Name, dev)
		prog.Warn("The image is read while in use, so it is not a point-in-time copy and may not mount cleanly.")
		prog.Warn("Detach the disk first for a backup you can rely on.")
	}

	if args.ToCloud() {
		return s.backupToCloud(ctx, state, prog, target)
	}
	return s.backupToClient(ctx, state, prog, target)
}

func (s *Server) backupToCloud(
	ctx context.Context,
	state *disk_v1alpha.DiskBackupBackup,
	prog progressSink,
	target *snapshot.BackupTarget,
) error {
	if s.updates == nil {
		return errNoCloud("backing up to miren.cloud")
	}
	if target.CloudVolumeID == "" {
		return refuse(
			"disk %q is not registered with miren.cloud yet, so there is nowhere to upload to — it registers on its own shortly after the disk is created",
			target.Name,
		)
	}

	prog.Message("Compressing and uploading %s to miren.cloud", target.Name)

	snapper := diskio.NewImageSnapshotter(s.log, s.updates)
	res, err := snapper.Snapshot(ctx, diskio.SnapshotRequest{
		VolumeID:     target.CloudVolumeID,
		ImagePath:    target.ImagePath,
		Name:         target.Name,
		Filesystem:   target.Filesystem,
		SnapshotName: state.Args().Pin(),
		StagingDir:   s.stagingDir(target.ImagePath),
	})
	if err != nil {
		return err
	}

	s.log.Info("backed up disk to cloud",
		"disk", target.Name,
		"restore_point", res.UpdateID,
		"image_size", res.ImageSize,
		"compressed_size", res.CompressedSize,
	)

	out := new(disk_v1alpha.BackupResult)
	out.SetImageSizeBytes(res.ImageSize)
	out.SetCompressedSizeBytes(res.CompressedSize)
	out.SetChecksum(res.Checksum)
	out.SetRestorePointId(res.UpdateID)
	state.Results().SetResult(&out)
	return nil
}

// backupToClient compresses the image and streams it down to the caller, which
// is how a cluster with no miren.cloud still produces a backup.
//
// The snapshot is staged to a file rather than compressed straight into the
// stream for two reasons. snapshot.Backup rewrites the header with the image's
// checksum once it has read the whole image, so it needs somewhere it can seek
// back to. And a staged snapshot is what makes the transfer resumable: a client
// that loses the connection at 90% comes back for the last 10% rather than
// making the server read and compress a multi-gigabyte image all over again.
func (s *Server) backupToClient(
	ctx context.Context,
	state *disk_v1alpha.DiskBackupBackup,
	prog progressSink,
	target *snapshot.BackupTarget,
) error {
	args := state.Args()

	out := args.Data()
	if out == nil {
		return refuse("backup needs either --cloud or somewhere to write the snapshot")
	}

	staged, meta, err := s.stageForClient(args.TransferId(), prog, target)
	if err != nil {
		return err
	}
	defer staged.Close()

	offset := args.Offset()
	if offset < 0 || offset > meta.CompressedSize {
		return refuse(
			"this snapshot is %d bytes and the client offered to continue from %d",
			meta.CompressedSize, offset,
		)
	}
	if _, err := staged.Seek(offset, io.SeekStart); err != nil {
		return fmt.Errorf("seeking staged snapshot: %w", err)
	}

	if offset > 0 {
		prog.Message("Resuming at %d of %d bytes", offset, meta.CompressedSize)
	} else {
		prog.Message("Sending %d bytes", meta.CompressedSize)
	}

	w := stream.ToWriter(ctx, out)
	sent, err := io.Copy(w, s.trackReads(staged, prog, offset, meta.CompressedSize))
	if err != nil {
		// The staged snapshot deliberately survives: the whole point is that
		// the client can come back for the rest of it.
		return fmt.Errorf("sending snapshot: %w", err)
	}
	// The stream belongs to the caller, so closing our writer flushes what we
	// wrote without tearing their client down.
	if err := w.Close(); err != nil {
		return fmt.Errorf("finishing snapshot stream: %w", err)
	}
	if want := meta.CompressedSize - offset; sent != want {
		return fmt.Errorf("sent %d bytes of the %d remaining", sent, want)
	}

	// Delivered in full, so there is nothing left to resume.
	s.discardStaging(args.TransferId())

	s.log.Info("backed up disk to client",
		"disk", target.Name,
		"image_size", meta.ImageSize,
		"compressed_size", meta.CompressedSize,
	)

	res := new(disk_v1alpha.BackupResult)
	res.SetImageSizeBytes(meta.ImageSize)
	res.SetCompressedSizeBytes(meta.CompressedSize)
	res.SetChecksum(meta.Checksum)
	state.Results().SetResult(&res)
	return nil
}

// stageForClient returns the compressed snapshot to send, compressing it only
// if this transfer has not already produced one.
//
// Reusing it is not just an optimization. A second compression pass would read
// the image as it is now, which for a live disk is not the image the client has
// already half-downloaded, and stitching the two together would produce a file
// that is the right length and not a valid snapshot of anything.
func (s *Server) stageForClient(
	transferID string,
	prog progressSink,
	target *snapshot.BackupTarget,
) (*os.File, *stagedSnapshot, error) {
	if meta, err := s.loadStaging(transferID); err != nil {
		return nil, nil, err
	} else if meta != nil {
		path, _ := s.transferPath(transferID)
		f, oerr := os.Open(path)
		if oerr != nil {
			return nil, nil, fmt.Errorf("reopening staged snapshot: %w", oerr)
		}
		return f, meta, nil
	}

	s.sweepTransfers()

	img, err := os.Open(target.ImagePath)
	if err != nil {
		return nil, nil, fmt.Errorf("opening disk image: %w", err)
	}
	defer img.Close()

	info, err := img.Stat()
	if err != nil {
		return nil, nil, fmt.Errorf("stat disk image: %w", err)
	}

	prog.Message("Compressing %s (%d bytes)", target.Name, info.Size())

	path, err := s.transferPath(transferID)
	if err != nil {
		return nil, nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, nil, fmt.Errorf("creating transfer directory: %w", err)
	}

	staged, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0600)
	if err != nil {
		return nil, nil, fmt.Errorf("creating staged snapshot: %w", err)
	}

	checksum, err := snapshot.Backup(staged, img, target.Name, info.Size(), target.Filesystem)
	if err != nil {
		staged.Close()
		os.Remove(path)
		return nil, nil, err
	}

	stagedInfo, err := staged.Stat()
	if err != nil {
		staged.Close()
		os.Remove(path)
		return nil, nil, fmt.Errorf("stat staged snapshot: %w", err)
	}

	meta := &stagedSnapshot{
		Disk:           target.Name,
		ImageSize:      info.Size(),
		CompressedSize: stagedInfo.Size(),
		Checksum:       checksum,
	}
	// Written only now, so a staging file left behind by a server that died
	// mid-compression has no marker and is never mistaken for a finished one.
	if err := s.saveStaging(transferID, meta); err != nil {
		staged.Close()
		os.Remove(path)
		return nil, nil, err
	}

	// Compression left the file at its end. Rewind so a freshly staged snapshot
	// and one reopened for a resume are handed back the same way, rather than
	// leaving every caller to remember which it got.
	if _, err := staged.Seek(0, io.SeekStart); err != nil {
		staged.Close()
		return nil, nil, fmt.Errorf("rewinding staged snapshot: %w", err)
	}

	return staged, meta, nil
}

// trackReads reports progress as bytes are pulled out of r, counting from what
// the client already has so a resumed transfer shows a bar that keeps going up
// rather than restarting.
func (s *Server) trackReads(r io.Reader, prog progressSink, done, total int64) io.Reader {
	start := time.Now()
	sentThisCall := int64(0)
	var lastReport time.Time

	return readerFunc(func(p []byte) (int, error) {
		n, err := r.Read(p)
		done += int64(n)
		sentThisCall += int64(n)

		// One event per 250ms. The client renders a bar from these, and a
		// report per read would be thousands of RPCs for one backup.
		if now := time.Now(); now.Sub(lastReport) >= 250*time.Millisecond || err == io.EOF {
			lastReport = now
			// Rate is measured over this attempt only. Counting bytes a
			// previous attempt moved against this attempt's clock would
			// report a throughput nothing achieved.
			elapsed := now.Sub(start).Seconds()
			var perSecond, eta int64
			if elapsed > 0 {
				perSecond = int64(float64(sentThisCall) / elapsed)
			}
			if perSecond > 0 && total > done {
				eta = (total - done) / perSecond
			}
			prog.Transfer(done, total, perSecond, eta)
		}
		return n, err
	})
}

type readerFunc func(p []byte) (int, error)

func (f readerFunc) Read(p []byte) (int, error) { return f(p) }

// ListBackups returns the restore points a disk can be restored to, newest
// first.
func (s *Server) ListBackups(ctx context.Context, state *disk_v1alpha.DiskBackupListBackups) error {
	args := state.Args()

	target, err := s.prepareBackup(ctx, args.Disk())
	if err != nil {
		return err
	}

	if s.updates == nil {
		return errNoCloud("listing restore points")
	}
	if target.CloudVolumeID == "" {
		// Not an error: a disk that has never been registered simply has no
		// restore points, which is a different thing from a broken lookup.
		state.Results().SetPoints(nil)
		return nil
	}

	updates, err := s.updates.List(ctx, target.CloudVolumeID, diskio.ListOptions{
		Kind:       diskio.KindLoopImage,
		Descending: true,
	})
	if err != nil {
		return fmt.Errorf("listing restore points for %s: %w", target.Name, err)
	}

	points := make([]*disk_v1alpha.RestorePoint, 0, len(updates))
	for _, u := range updates {
		points = append(points, restorePointFromUpdate(u))
	}

	state.Results().SetPoints(points)
	return nil
}
