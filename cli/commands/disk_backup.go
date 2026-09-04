package commands

import (
	"fmt"
	"io"
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

	transferID, err := newTransferID()
	if err != nil {
		return err
	}

	if opts.Cloud {
		ctx.Info("Backing up disk %q to miren.cloud", opts.Name)

		// No resume here: the bytes go from the server straight to the cloud,
		// so nothing is riding on this connection but the request itself.
		res, err := dc.Backup(ctx, opts.Name, true, opts.Pin, nil, progress, transferID, 0)
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

	var result *disk_v1alpha.BackupResult

	err = runTransfer(ctx, "Backup", func(try int) error {
		// What is durably in the file is the resume point, and it is the
		// client's to know: it is the only end that can see how much of the
		// stream actually reached disk.
		offset, oerr := syncedSize(outFile)
		if oerr != nil {
			return oerr
		}

		res, berr := dc.Backup(ctx, opts.Name, false, "", stream.ServeWriter(ctx, outFile), progress, transferID, offset)
		if berr != nil {
			return berr
		}
		result = res.Result()
		return nil
	})
	if err != nil {
		return err
	}
	complete = true

	reportBackup(ctx, result, time.Since(start))
	ctx.Info("  Snapshot:        %s", outputPath)
	return nil
}

// syncedSize flushes what has been written and reports how much of it is
// durably on disk.
//
// This is the number the server is told to continue from, so it has to be what
// survived rather than what was handed to the kernel: asking to resume past
// bytes that a crash could still take back would leave a hole in the snapshot.
func syncedSize(f *os.File) (int64, error) {
	if err := f.Sync(); err != nil {
		return 0, fmt.Errorf("flushing snapshot: %w", err)
	}
	info, err := f.Stat()
	if err != nil {
		return 0, fmt.Errorf("stat snapshot: %w", err)
	}
	// Writes go on at the end, so the offset has to follow the length: a retry
	// that left the file position short would overwrite good bytes.
	if _, err := f.Seek(info.Size(), io.SeekStart); err != nil {
		return 0, fmt.Errorf("seeking snapshot: %w", err)
	}
	return info.Size(), nil
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
