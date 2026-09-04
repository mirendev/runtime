package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/diskresolve"
	"miren.dev/runtime/pkg/snapshot"
)

// DiskRestore restores a disk from a compressed snapshot file.
func DiskRestore(ctx *Context, opts struct {
	ConfigCentric
	Snapshot string `short:"s" long:"snapshot" description:"Path to snapshot file" required:"true"`
	Name     string `short:"n" long:"name" description:"Disk name to restore to (default: original name from snapshot)"`
	DataPath string `long:"data-path" description:"Path to miren data directory" default:"/var/lib/miren"`
	Force    bool   `short:"f" long:"force" description:"Overwrite existing disk image without confirmation"`
}) (retErr error) {
	if _, err := os.Stat(opts.DataPath); err != nil {
		return fmt.Errorf("data path %s not found — disk restore must be run on the server", opts.DataPath)
	}

	snapFile, err := os.Open(opts.Snapshot)
	if err != nil {
		return fmt.Errorf("opening snapshot file: %w", err)
	}
	defer snapFile.Close()

	meta, err := snapshot.ReadHeader(snapFile)
	if err != nil {
		return fmt.Errorf("reading snapshot header: %w", err)
	}

	diskName := opts.Name
	if diskName == "" {
		diskName = meta.Name
	}

	ctx.Info("Restoring from snapshot: %s", opts.Snapshot)
	ctx.Info("  Original disk:  %s", meta.Name)
	ctx.Info("  Size:           %s", formatBytes(meta.SizeBytes))
	ctx.Info("  Filesystem:     %s", meta.Filesystem)
	ctx.Info("  Created:        %s", meta.Timestamp.Format(time.RFC3339))
	ctx.Info("  Checksum:       %s", meta.Checksum)
	ctx.Info("  Target disk:    %s", diskName)

	client, err := ctx.RPCClient("entities")
	if err != nil {
		return err
	}

	eac := entityserver_v1alpha.NewEntityAccessClient(client)
	ec := entityserver.NewClient(ctx.Log, eac)
	resolver := diskresolve.New(eac, ec)

	target, err := snapshot.PrepareRestore(ctx, resolver, diskName, opts.DataPath,
		snapshot.WithCreator(resolver, meta.SizeBytes, meta.Filesystem),
	)
	if err != nil {
		return err
	}

	// If the disk was freshly created and anything fails, clean up the
	// RESTORING disk entity so it doesn't become a zombie.
	if target.Created && target.Cleanup != nil {
		defer func() {
			if retErr != nil {
				ctx.Warn("Cleaning up disk entity after failed restore")
				if cerr := target.Cleanup(ctx); cerr != nil {
					ctx.Warn("Failed to clean up disk entity: %v", cerr)
				}
			}
		}()
	}

	if !target.Created {
		if _, err := os.Stat(target.ImagePath); err == nil {
			if !opts.Force {
				return fmt.Errorf("disk image already exists at %s — use --force to overwrite", target.ImagePath)
			}
			ctx.Warn("Overwriting existing disk image at %s", target.ImagePath)
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("checking existing image at %s: %w", target.ImagePath, err)
		}
	}

	ctx.Info("Restoring to: %s", target.ImagePath)

	start := time.Now()

	if err := os.MkdirAll(filepath.Dir(target.ImagePath), 0o755); err != nil {
		return fmt.Errorf("creating image directory: %w", err)
	}

	tmpPath := target.ImagePath + ".restore.tmp"
	outFile, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("creating temp image file: %w", err)
	}
	defer func() {
		outFile.Close()
		if retErr != nil {
			os.Remove(tmpPath)
		}
	}()

	if err := outFile.Truncate(meta.SizeBytes); err != nil {
		return fmt.Errorf("truncating image file: %w", err)
	}

	ctx.Info("Decompressing...")

	if err := snapshot.RestoreImage(outFile, snapFile, meta); err != nil {
		return err
	}

	if err := outFile.Close(); err != nil {
		return fmt.Errorf("closing restored image: %w", err)
	}

	if err := os.Rename(tmpPath, target.ImagePath); err != nil {
		return fmt.Errorf("moving restored image into place: %w", err)
	}

	if target.Finalize != nil {
		ctx.Info("Finalizing disk entities...")
		if err := target.Finalize(ctx); err != nil {
			return fmt.Errorf("finalizing restore: %w", err)
		}
	}

	duration := time.Since(start)

	ctx.Info("Restore complete")
	ctx.Info("  Disk:      %s", diskName)
	ctx.Info("  Image:     %s", target.ImagePath)
	ctx.Info("  Size:      %s", formatBytes(meta.SizeBytes))
	ctx.Info("  Checksum:  verified")
	ctx.Info("  Duration:  %s", duration.Truncate(time.Millisecond))

	return nil
}
