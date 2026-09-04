package commands

import (
	"fmt"
	"io"
	"os"

	"time"

	"miren.dev/runtime/api/disk/disk_v1alpha"
	"miren.dev/runtime/pkg/progress/upload"
	"miren.dev/runtime/pkg/rpc/stream"
	"miren.dev/runtime/pkg/snapshot"
	"miren.dev/runtime/pkg/ui"
)

// DiskRestore restores a disk, driving the server over RPC.
//
// Either from a local snapshot file, which is streamed up, or from a restore
// point in miren.cloud, which the server downloads itself. Restoring a disk this
// cluster has never seen creates it, so this works on a freshly built host with
// nothing on it — which is the whole point, since disaster recovery means the
// original host is gone.
func DiskRestore(ctx *Context, opts struct {
	ConfigCentric
	Snapshot     string `short:"s" long:"snapshot" description:"Path to a snapshot file to restore from"`
	Name         string `short:"n" long:"name" description:"Disk name to restore to (default: the name recorded in the snapshot)"`
	FromCloud    bool   `long:"from-cloud" description:"Restore from a miren.cloud restore point"`
	RestorePoint string `long:"restore-point" description:"Restore point to use (implies --from-cloud; default: the newest)"`
	Force        bool   `short:"f" long:"force" description:"Overwrite an existing disk image"`
}) error {
	client, err := ctx.RPCClient(diskBackupService)
	if err != nil {
		return err
	}
	dc := disk_v1alpha.NewDiskBackupClient(client)

	fromCloud := opts.FromCloud || opts.RestorePoint != ""
	if opts.Snapshot == "" && !fromCloud {
		return fmt.Errorf("restore needs either --snapshot to read a local file or --from-cloud to fetch a restore point")
	}
	if opts.Snapshot != "" && fromCloud {
		return fmt.Errorf("pass either --snapshot or --from-cloud, not both")
	}

	start := time.Now()
	progress := diskProgress(ctx)

	if fromCloud {
		if opts.Name == "" {
			return fmt.Errorf("--name says which disk's restore points to look at, so it is required with --from-cloud")
		}

		point := opts.RestorePoint
		if point == "" {
			point, err = pickRestorePoint(ctx, dc, opts.Name)
			if err != nil {
				return err
			}
		}

		ctx.Info("Restoring disk %q from restore point %s", opts.Name, point)

		// No resume here: the server fetches from the cloud over its own
		// connection, so nothing rides on this one but the request.
		res, err := dc.Restore(ctx, opts.Name, point, nil, opts.Force, progress, "", 0)
		if err != nil {
			return err
		}
		reportRestore(ctx, res.Result(), time.Since(start))
		return nil
	}

	snapFile, err := os.Open(opts.Snapshot)
	if err != nil {
		return fmt.Errorf("opening snapshot: %w", err)
	}
	defer snapFile.Close()

	name := opts.Name
	if name == "" {
		// The snapshot records the disk it came from, so restoring one back
		// where it belongs needs no --name. The server reads the header again
		// for the size and filesystem; this read is only to answer "which
		// disk", which the client has to know before it can ask.
		meta, err := snapshot.ReadHeader(snapFile)
		if err != nil {
			return fmt.Errorf("reading snapshot header: %w", err)
		}
		name = meta.Name
		if name == "" {
			return fmt.Errorf("%s records no disk name, so pass --name", opts.Snapshot)
		}
		if _, err := snapFile.Seek(0, io.SeekStart); err != nil {
			return fmt.Errorf("rewinding snapshot: %w", err)
		}
	}

	ctx.Info("Restoring disk %q from %s", name, opts.Snapshot)

	transferID, err := newTransferID()
	if err != nil {
		return err
	}

	var result *disk_v1alpha.RestoreResult

	err = runTransfer(ctx, "Restore", func(try int) error {
		// The server is the only end that knows how much of the upload it
		// durably has, so ask rather than assume. An id it has never seen
		// reports zero, which makes the first attempt and a retry identical.
		off, oerr := dc.TransferOffset(ctx, transferID)
		if oerr != nil {
			return oerr
		}
		offset := off.ReceivedBytes()

		if _, serr := snapFile.Seek(offset, io.SeekStart); serr != nil {
			return fmt.Errorf("seeking snapshot to %d: %w", offset, serr)
		}

		res, rerr := dc.Restore(ctx, name, "",
			stream.ServeReader(ctx, snapFile, stream.WithBulkBatching()),
			opts.Force, progress, transferID, offset)
		if rerr != nil {
			return rerr
		}
		result = res.Result()
		return nil
	})
	if err != nil {
		return err
	}

	reportRestore(ctx, result, time.Since(start))
	return nil
}

// restorePointItem adapts a restore point to the shared picker.
type restorePointItem struct {
	point *disk_v1alpha.RestorePoint
}

func (r restorePointItem) ID() string { return r.point.Id() }

func (r restorePointItem) Row() []string {
	when := "unknown"
	if t := goTime(r.point.HasCreatedAt(), r.point.CreatedAt()); !t.IsZero() {
		when = t.Format("2006-01-02 15:04:05")
	}
	return []string{
		when,
		upload.FormatBytes(r.point.SizeBytes()),
		r.point.Name(),
		r.point.Id(),
	}
}

// pickRestorePoint lists what is available and asks, so an operator recovering a
// disk does not have to already know a restore point id.
func pickRestorePoint(ctx *Context, dc *disk_v1alpha.DiskBackupClient, name string) (string, error) {
	res, err := dc.ListBackups(ctx, name)
	if err != nil {
		return "", err
	}

	points := res.Points()
	if len(points) == 0 {
		return "", fmt.Errorf("disk %q has no restore points in miren.cloud", name)
	}

	// The server returns newest first. On a non-interactive run take that one:
	// it is what a recovery almost always wants, and there is nobody to ask.
	if !ui.IsInteractive() {
		ctx.Info("Using the newest restore point (%s of %d)", points[0].Id(), len(points))
		return points[0].Id(), nil
	}

	items := make([]ui.PickerItem, 0, len(points))
	for _, p := range points {
		items = append(items, restorePointItem{point: p})
	}

	chosen, err := ui.RunPicker(items,
		ui.WithTitle(fmt.Sprintf("Restore point for %q", name)),
		ui.WithHeaders([]string{"Taken", "Size", "Pinned as", "ID"}),
	)
	if err != nil {
		return "", err
	}
	if chosen == nil {
		return "", fmt.Errorf("no restore point selected")
	}
	return chosen.ID(), nil
}

func reportRestore(ctx *Context, res *disk_v1alpha.RestoreResult, took time.Duration) {
	ctx.Info("Restore complete")
	if res == nil {
		return
	}
	if res.Created() {
		ctx.Info("  Created disk:    %s", res.Disk())
	}
	ctx.Info("  Restored size:   %s", formatBytes(res.ImageSizeBytes()))
	ctx.Info("  Duration:        %s", took.Truncate(time.Millisecond))
}
