package commands

import (
	"fmt"
	"os"
	"time"

	"miren.dev/runtime/api/disk/disk_v1alpha"
	"miren.dev/runtime/pkg/rpc/stream"
)

// DiskBackup backs up a disk, driving the server over RPC.
//
// There is one path here whether you run this on the server host or on your
// laptop, so there is no local-versus-remote detection and no failure mode where
// that detection guesses wrong. The image bytes and the cluster's miren.cloud
// key both stay on the server; see RFD-108.
func DiskBackup(ctx *Context, opts struct {
	ConfigCentric
	Name   string `short:"n" long:"name" description:"Disk name to backup" required:"true"`
	Output string `short:"o" long:"output" description:"Output snapshot path (default: DISK-YYYYMMDD-HHMMSS.miren.zst)"`
	Cloud  bool   `long:"cloud" description:"Upload the snapshot to miren.cloud as a restore point instead of writing a local file"`
	Pin    string `long:"pin" description:"Name the uploaded restore point, pinning it against cleanup"`
}) (retErr error) {
	client, err := ctx.RPCClient(diskBackupService)
	if err != nil {
		return err
	}
	dc := disk_v1alpha.NewDiskBackupClient(client)

	if opts.Pin != "" && !opts.Cloud {
		return fmt.Errorf("--pin names a restore point in miren.cloud, so it only applies with --cloud")
	}

	start := time.Now()
	progress := diskProgress(ctx)

	if opts.Cloud {
		ctx.Info("Backing up disk %q to miren.cloud", opts.Name)

		res, err := dc.Backup(ctx, opts.Name, true, opts.Pin, nil, progress)
		if err != nil {
			return err
		}
		reportBackup(ctx, res.Result(), time.Since(start))
		ctx.Info("  Restore point:   %s", res.Result().RestorePointId())
		return nil
	}

	outputPath := opts.Output
	if outputPath == "" {
		outputPath = fmt.Sprintf("%s-%s.miren.zst", opts.Name, time.Now().Format("20060102-150405"))
	}

	outFile, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("creating output file: %w", err)
	}

	// Discard a snapshot that never finished. A truncated .miren.zst that looks
	// like a backup is worse than no file at all.
	complete := false
	defer func() {
		closeErr := outFile.Close()
		if closeErr != nil && retErr == nil {
			retErr = fmt.Errorf("closing output file: %w", closeErr)
		}
		if retErr != nil || !complete {
			os.Remove(outputPath)
		}
	}()

	ctx.Info("Backing up disk %q", opts.Name)
	ctx.Info("Output: %s", outputPath)

	res, err := dc.Backup(ctx, opts.Name, false, "", stream.ServeWriter(ctx, outFile), progress)
	if err != nil {
		return err
	}
	complete = true

	reportBackup(ctx, res.Result(), time.Since(start))
	ctx.Info("  Snapshot:        %s", outputPath)
	return nil
}

func reportBackup(ctx *Context, res *disk_v1alpha.BackupResult, took time.Duration) {
	ctx.Info("Backup complete")
	if res == nil {
		return
	}
	ctx.Info("  Original size:   %s", formatBytes(res.ImageSizeBytes()))
	if res.ImageSizeBytes() > 0 {
		ratio := float64(res.CompressedSizeBytes()) / float64(res.ImageSizeBytes()) * 100
		ctx.Info("  Compressed size: %s (%.1f%%)", formatBytes(res.CompressedSizeBytes()), ratio)
	} else {
		ctx.Info("  Compressed size: %s", formatBytes(res.CompressedSizeBytes()))
	}
	ctx.Info("  Checksum:        %s", res.Checksum())
	ctx.Info("  Duration:        %s", took.Truncate(time.Millisecond))
}
