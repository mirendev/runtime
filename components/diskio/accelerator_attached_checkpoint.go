package diskio

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"miren.dev/runtime/api/storage/storage_v1alpha"
)

// DefaultAttachedCheckpointInterval is how often a disk that stays attached
// gets a checkpoint, so its replay and retained log stay about an hour deep
// rather than growing for as long as the disk is attached.
const DefaultAttachedCheckpointInterval = time.Hour

// attachedCheckpointPoll is how often attached mounts are checked for a due
// checkpoint. It only bounds how late one starts.
const attachedCheckpointPoll = 5 * time.Minute

// attachedCheckpointMinMargin is the least free space an attached checkpoint
// leaves beyond its rebuilt image, for the compressed payload and other writers.
const attachedCheckpointMinMargin = 256 << 20

// availableBytes reports the free space for unprivileged writes at path.
var availableBytes = func(path string) (uint64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, err
	}
	return uint64(st.Bavail) * uint64(st.Bsize), nil
}

// checkpointBasisReplayed tells the cloud the image was rebuilt from its own
// committed history, so its boundary may lie below the moving head.
const checkpointBasisReplayed = "replayed"

// RunAttachedCheckpoints checkpoints attached accelerator disks until ctx
// ends. A live device's image is always ahead of its uploaded log, so the
// checkpoint is never read from it: each one is rebuilt from the latest cloud
// checkpoint plus the committed tail, the same protected selection a fresh
// node restores from, and published with that tail's last label as its
// boundary. A volume without a stream is skipped: its legacy log cannot be
// verified, so its first checkpoint comes from detach. The device is never
// paused.
func (c *DiskMountController) RunAttachedCheckpoints(ctx context.Context) {
	ticker := time.NewTicker(attachedCheckpointPoll)
	defer ticker.Stop()
	due := map[string]time.Time{}
	for {
		c.checkpointAttachedMounts(ctx, due, time.Now())
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// checkpointAttachedMounts publishes a checkpoint for each attached
// accelerator mount whose interval has passed. A mount first seen here is due
// one interval later: it attached from a checkpoint and its tail is short.
// Checkpoints run one at a time, since each rebuilds a full image.
func (c *DiskMountController) checkpointAttachedMounts(ctx context.Context, due map[string]time.Time, now time.Time) {
	client, ok := c.updatesClient.(protectedUpdatesClient)
	if !ok || c.checkpointsUnsupported.Load() {
		return
	}
	attached := map[string]bool{}
	for _, mount := range c.state.ListMounts() {
		if mount.Mode != storage_v1alpha.VM_ACCELERATOR || mount.LeaseNonce == "" || mount.DevicePath == "" {
			continue
		}
		attached[mount.VolumeId] = true
		next, seen := due[mount.VolumeId]
		if !seen {
			due[mount.VolumeId] = now.Add(c.attachedCheckpointInterval)
			continue
		}
		if now.Before(next) || ctx.Err() != nil {
			continue
		}
		// A failure waits a full interval too: the next try rebuilds the
		// whole image, and the log already holds everything meanwhile.
		due[mount.VolumeId] = now.Add(c.attachedCheckpointInterval)
		v := c.state.GetVolume(mount.VolumeId)
		if v == nil || v.CloudVolumeId == "" {
			continue
		}
		if err := c.publishAttachedCheckpoint(ctx, client, v, mount.LeaseNonce); err != nil {
			c.log.Warn("attached accelerator checkpoint failed; will retry next interval",
				"volume_id", v.CloudVolumeId, "error", err)
		}
	}
	for id := range due {
		if !attached[id] {
			delete(due, id)
		}
	}
}

// publishAttachedCheckpoint rebuilds v's image through the cloud's committed
// head and publishes it as a replayed checkpoint.
func (c *DiskMountController) publishAttachedCheckpoint(ctx context.Context, client protectedUpdatesClient, v *VolumeState, nonce string) error {
	stream, err := os.ReadFile(filepath.Join(v.DiskPath, acceleratorStreamFile))
	if os.IsNotExist(err) {
		// A volume without a stream has only the legacy listing, which carries
		// no digests, labels or sequences to verify a rebuild against. Its
		// first checkpoint would start the stream and make that log
		// retirable, so it comes from detach, which checkpoints the real image.
		return nil
	} else if err != nil {
		return err
	}
	m, err := client.BeginRecovery(ctx, v.CloudVolumeId, "", "")
	if err != nil {
		return fmt.Errorf("select committed history: %w", err)
	}
	if m.Status != "ready" || m.StreamID != string(stream) {
		return fmt.Errorf("cloud history is %q for stream %s, local stream %s", m.Status, m.StreamID, stream)
	}
	if m.Head == "" || (m.Checkpoint != nil && m.Checkpoint.ThroughLabel == m.Head) {
		return nil // nothing committed since the newest checkpoint
	}
	// The rebuild writes a whole image next to the live one, then its
	// compressed payload. Running the volume's own filesystem out of space
	// would cost far more than a skipped checkpoint, which loses nothing.
	need := m.DeviceSize + max(m.DeviceSize/10, attachedCheckpointMinMargin)
	if free, err := availableBytes(v.DiskPath); err != nil {
		return fmt.Errorf("check free space: %w", err)
	} else if free < need {
		c.log.Warn("skipping attached accelerator checkpoint: not enough free disk space to rebuild the image",
			"volume_id", v.CloudVolumeId, "path", v.DiskPath, "free_bytes", free, "needed_bytes", need)
		c.metrics.inc(ctx, "miren_disk_accelerator_checkpoint_skipped_total",
			map[string]string{"volume_id": v.CloudVolumeId, "reason": "low_disk_space"})
		return nil
	}
	start := time.Now()
	var payload string
	var intent checkpointIntent
	defer func() {
		if payload != "" {
			os.Remove(payload)
		}
	}()
	err = c.protectRecovery(ctx, client, m, func(ctx context.Context) error {
		image, err := c.buildRecoveryImage(ctx, v, m, 0)
		if image != "" {
			defer os.Remove(image)
		}
		if err != nil {
			return fmt.Errorf("rebuild image: %w", err)
		}
		payload, intent, err = c.compressCheckpoint(ctx, v, image, m.StreamID, m.Head)
		return err
	})
	if err != nil {
		return err
	}
	_, err = c.uploadAttachedCheckpoint(ctx, v, nonce, intent, payload, len(m.Segments), start)
	return err
}

func (c *DiskMountController) uploadAttachedCheckpoint(ctx context.Context, v *VolumeState, nonce string, intent checkpointIntent, payload string, segments int, start time.Time) (string, error) {
	intent.Publication.Basis = checkpointBasisReplayed
	id, err := c.uploadCheckpoint(ctx, v, nonce, intent, payload)
	if errors.Is(err, errCheckpointUnsupported) {
		c.checkpointsUnsupported.Store(true)
	}
	if err != nil {
		return "", err
	}
	c.log.Info("published attached accelerator checkpoint", "volume_id", v.CloudVolumeId, "checkpoint", id,
		"stream", intent.Publication.StreamID, "through_label", intent.Publication.Base.ThroughLabel, "segments", segments,
		"image_bytes", intent.Publication.Base.ImageSize, "payload_bytes", intent.Publication.Base.Size,
		"took", time.Since(start).Round(time.Millisecond))
	return id, nil
}
