package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/components/coordinate"
	"miren.dev/runtime/components/diskio"
	"miren.dev/runtime/pkg/cloudauth"
	"miren.dev/runtime/pkg/registration"
	"miren.dev/runtime/pkg/snapshot"
)

// DiskBackup backs up a disk to a compressed snapshot file.
func DiskBackup(ctx *Context, opts struct {
	ConfigCentric
	Name     string `short:"n" long:"name" description:"Disk name to backup" required:"true"`
	Output   string `short:"o" long:"output" description:"Output snapshot path (default: DISK-YYYYMMDD-HHMMSS.miren.zst)"`
	DataPath string `long:"data-path" description:"Path to miren data directory" default:"/var/lib/miren"`
	Cloud    bool   `long:"cloud" description:"Also upload the snapshot to miren.cloud as a restore point"`
	Pin      string `long:"pin" description:"Name the uploaded restore point, pinning it against cleanup"`
}) (retErr error) {
	if _, err := os.Stat(opts.DataPath); err != nil {
		return fmt.Errorf("data path %s not found — disk backup must be run on the server", opts.DataPath)
	}

	client, err := ctx.RPCClient("entities")
	if err != nil {
		return err
	}

	eac := entityserver_v1alpha.NewEntityAccessClient(client)
	resolver := newEntityDiskResolver(eac, nil)

	target, err := snapshot.PrepareBackup(ctx, resolver, opts.Name, opts.DataPath)
	if err != nil {
		return err
	}

	imgInfo, err := os.Stat(target.ImagePath)
	if err != nil {
		return fmt.Errorf("disk image not found at %s: %w", target.ImagePath, err)
	}

	if target.IsAttached {
		// Nothing here freezes the filesystem or takes a copy-on-write clone, so
		// this is a sequential read of a file the loop device is still writing.
		// The head and tail of the image come from different moments, which is
		// weaker than the power-loss state fsck and Postgres recovery are built
		// for. Say so plainly: the operator is the one deciding this is safe.
		ctx.Warn("Disk is attached and may be written during the backup.")
		ctx.Warn("The image is read while in use, so it is not a point-in-time copy and may not mount cleanly.")
		ctx.Warn("Detach the disk first for a backup you can rely on.")
	}

	outputPath := opts.Output
	if outputPath == "" {
		outputPath = fmt.Sprintf("%s-%s.miren.zst", opts.Name, time.Now().Format("20060102-150405"))
	}

	ctx.Info("Backing up disk %q (%s)", opts.Name, formatBytes(imgInfo.Size()))
	ctx.Info("Image: %s", target.ImagePath)
	ctx.Info("Output: %s", outputPath)

	start := time.Now()

	imgFile, err := os.Open(target.ImagePath)
	if err != nil {
		return fmt.Errorf("opening image file: %w", err)
	}
	defer imgFile.Close()

	outFile, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("creating output file: %w", err)
	}
	// Set once the snapshot itself is written. A later step failing (the cloud
	// upload) must not take a finished local backup down with it.
	snapshotComplete := false

	defer func() {
		closeErr := outFile.Close()
		if closeErr != nil && retErr == nil {
			retErr = fmt.Errorf("closing output file: %w", closeErr)
		}
		// Only discard a snapshot that never finished, or one whose close
		// failed and may therefore be truncated.
		if retErr != nil && (!snapshotComplete || closeErr != nil) {
			os.Remove(outputPath)
		}
	}()

	ctx.Info("Computing checksum and compressing...")

	checksum, err := snapshot.Backup(outFile, imgFile, opts.Name, imgInfo.Size(), target.Filesystem)
	if err != nil {
		return err
	}

	outInfo, err := outFile.Stat()
	if err != nil {
		return fmt.Errorf("stat output file: %w", err)
	}

	snapshotComplete = true

	duration := time.Since(start)
	ratio := float64(outInfo.Size()) / float64(imgInfo.Size()) * 100

	ctx.Info("Backup complete")
	ctx.Info("  Original size:   %s", formatBytes(imgInfo.Size()))
	ctx.Info("  Compressed size: %s (%.1f%%)", formatBytes(outInfo.Size()), ratio)
	ctx.Info("  Checksum:        %s", checksum)
	ctx.Info("  Duration:        %s", duration.Truncate(time.Millisecond))
	ctx.Info("  Snapshot:        %s", outputPath)

	if opts.Cloud {
		updateID, err := uploadSnapshotToCloud(ctx, opts.DataPath, target, outputPath, cloudSnapshotDetails{
			Pin:            opts.Pin,
			ImageSize:      imgInfo.Size(),
			ImageChecksum:  checksum,
			CompressedSize: outInfo.Size(),
		})
		if err != nil {
			// The local snapshot is finished and valid; say where it is rather
			// than leaving the operator thinking they got nothing.
			ctx.Warn("Upload failed, but the local snapshot is intact at %s", outputPath)
			return fmt.Errorf("uploading snapshot to miren.cloud: %w", err)
		}
		ctx.Info("  Uploaded:        %s", updateID)
	}

	return nil
}

// cloudSnapshotDetails carries what the sidecar records about the image the
// snapshot was taken from. It mirrors what diskio.ImageSnapshotter writes, so a
// restore sees the same fields whichever path produced the update.
type cloudSnapshotDetails struct {
	Pin            string
	ImageSize      int64
	ImageChecksum  string
	CompressedSize int64
}

// uploadSnapshotToCloud sends a finished snapshot file to miren.cloud as a
// loop_image update for the disk's volume.
//
// The cluster's service account key lives in the registration this server wrote
// at enrolment, which is why this has to run on the server alongside the data
// directory.
func uploadSnapshotToCloud(ctx *Context, dataPath string, target *snapshot.BackupTarget, snapshotPath string, details cloudSnapshotDetails) (string, error) {
	if target.VolumeID == "" {
		return "", fmt.Errorf("disk %q has no cloud volume; it may predate cloud registration", target.Name)
	}

	reg, err := registration.LoadRegistration(filepath.Join(dataPath, "server"))
	if err != nil {
		return "", fmt.Errorf("loading cluster registration: %w", err)
	}
	if reg == nil || reg.Status != "approved" || reg.PrivateKey == "" {
		return "", fmt.Errorf("this cluster is not registered with miren.cloud")
	}

	keyPair, err := cloudauth.LoadKeyPairFromPEM(reg.PrivateKey)
	if err != nil {
		return "", fmt.Errorf("parsing cluster key: %w", err)
	}

	cloudURL := reg.CloudURL
	if cloudURL == "" {
		cloudURL = coordinate.DefaultCloudURL
	}

	authClient, err := cloudauth.NewAuthClient(cloudURL, keyPair)
	if err != nil {
		return "", fmt.Errorf("creating cloud auth client: %w", err)
	}

	file, err := os.Open(snapshotPath)
	if err != nil {
		return "", fmt.Errorf("opening snapshot: %w", err)
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return "", fmt.Errorf("stat snapshot: %w", err)
	}

	ctx.Info("Uploading to %s...", cloudURL)

	updates := diskio.NewCloudUpdatesClient(ctx.Log, cloudURL, authClient)
	// ctx is the command context, so Ctrl-C interrupts the upload rather than
	// leaving it running to the end of a multi-gigabyte image.
	return updates.Upload(ctx, target.VolumeID, diskio.UploadRequest{
		Kind:         diskio.KindLoopImage,
		OrderingKey:  fmt.Sprintf("%016x", time.Now().UnixNano()),
		SnapshotName: details.Pin,
		Metadata: map[string]any{
			"compression":     "zstd",
			"format":          "miren-snapshot",
			"filesystem":      target.Filesystem,
			"image_size":      details.ImageSize,
			"image_sha256":    details.ImageChecksum,
			"compressed_size": details.CompressedSize,
			// Records that this was taken from a live disk, so the image is a
			// smear across the read rather than a point-in-time copy.
			"was_attached": target.IsAttached,
		},
	}, file, info.Size())
}
