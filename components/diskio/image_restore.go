package diskio

import (
	"context"
	"fmt"
	"os"

	"miren.dev/runtime/pkg/snapshot"
)

// RestoreImageIfMissing rebuilds a universal-mode volume's backing image from
// the newest loop_image snapshot in the cloud, when the host no longer has one.
//
// This is the universal-mode counterpart to replayMissingSegments. It recovers a
// lost disk; it never touches an image that is already here, because the local
// copy is by definition newer than anything the cloud holds.
func (c *DiskMountController) RestoreImageIfMissing(ctx context.Context, volState *VolumeState, imagePath string) error {
	if c.updatesClient == nil {
		return nil
	}

	if _, err := os.Stat(imagePath); err == nil {
		// The image is present; leave it alone
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat image: %w", err)
	}

	updates, err := c.updatesClient.List(ctx, volState.VolumeId, ListOptions{Kind: KindLoopImage})
	if err != nil {
		return fmt.Errorf("listing image snapshots: %w", err)
	}
	if len(updates) == 0 {
		// Nothing was ever uploaded; this is a fresh volume, not a lost one
		c.log.Info("no image snapshot to restore", "volume_id", volState.VolumeId)
		return nil
	}

	// The cloud returns them in ordering-key order, so the last is the newest
	newest := updates[len(updates)-1]

	c.log.Info("restoring volume image from cloud snapshot",
		"volume_id", volState.VolumeId,
		"update_id", newest.UpdateID,
		"ordering_key", newest.OrderingKey,
	)

	body, err := c.updatesClient.Download(ctx, volState.VolumeId, newest.UpdateID)
	if err != nil {
		return fmt.Errorf("downloading image snapshot %s: %w", newest.UpdateID, err)
	}
	defer body.Close()

	meta, err := snapshot.ReadHeader(body)
	if err != nil {
		return fmt.Errorf("reading snapshot header: %w", err)
	}

	// Build into a temp file and rename, so an interrupted restore cannot leave
	// a half-written image that looks complete on the next mount.
	tmpPath := imagePath + ".restoring"
	dst, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("creating image file: %w", err)
	}

	if err := snapshot.RestoreImage(dst, body, meta); err != nil {
		dst.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("restoring image: %w", err)
	}
	if err := dst.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("closing restored image: %w", err)
	}

	if err := os.Rename(tmpPath, imagePath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("installing restored image: %w", err)
	}

	c.log.Info("restored volume image",
		"volume_id", volState.VolumeId,
		"update_id", newest.UpdateID,
		"size", meta.SizeBytes,
		"checksum", meta.Checksum,
	)
	return nil
}
