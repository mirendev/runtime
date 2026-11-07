package diskio

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
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

// checkpointBasisReplayed tells the cloud the image was rebuilt from its own
// committed history, so its boundary may lie below the moving head.
const checkpointBasisReplayed = "replayed"

// RunAttachedCheckpoints checkpoints attached accelerator disks until ctx
// ends. A live device's image is always ahead of its uploaded log, so the
// checkpoint is never read from it: each one is rebuilt from the latest cloud
// checkpoint plus the committed tail, the same protected selection a fresh
// node restores from, and published with that tail's last label as its
// boundary. A volume that never had a checkpoint is rebuilt from its whole
// log and starts its stream. The device is never paused.
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
		return c.publishFirstAttachedCheckpoint(ctx, client, v, nonce)
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

// publishFirstAttachedCheckpoint covers a volume that has never had a
// checkpoint, such as a new disk that has stayed attached since it was
// created. Nothing of its history was ever retired, so its image is rebuilt
// from an empty one plus its whole committed log, and the checkpoint starts
// its stream. A stream already proposed by a detach that never published is
// reused, so the two never disagree.
func (c *DiskMountController) publishFirstAttachedCheckpoint(ctx context.Context, client protectedUpdatesClient, v *VolumeState, nonce string) error {
	// Without a stream the cloud retains the whole log ("legacy").
	m, err := client.BeginRecovery(ctx, v.CloudVolumeId, "", "")
	switch {
	case errors.Is(err, errNeverBackedUp):
		return nil // nothing written yet
	case errors.Is(err, errRecoveryUnsupported):
	case err != nil:
		return fmt.Errorf("select committed history: %w", err)
	default:
		return fmt.Errorf("cloud has stream %s for a volume with no local stream", m.StreamID)
	}
	var stream string
	if data, err := os.ReadFile(filepath.Join(v.DiskPath, checkpointIntentFile)); err == nil {
		var staged checkpointIntent
		if err := json.Unmarshal(data, &staged); err != nil {
			return err
		}
		stream = staged.Publication.StreamID
	} else if !os.IsNotExist(err) {
		return err
	}
	if stream == "" {
		id := make([]byte, 16)
		if _, err := rand.Read(id); err != nil {
			return err
		}
		stream = hex.EncodeToString(id)
	}
	segments, err := c.cloudClient.ListLogSegments(ctx, v.CloudVolumeId, "")
	if err != nil {
		return err
	}
	if len(segments) == 0 {
		return nil
	}
	sort.Slice(segments, func(i, j int) bool { return segments[i].Label < segments[j].Label })
	start := time.Now()
	image, err := os.CreateTemp(v.DiskPath, ".accelerator-image-*")
	if err != nil {
		return err
	}
	defer os.Remove(image.Name())
	err = image.Truncate(v.SizeBytes)
	if closeErr := image.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	img, err := openDiskImage(image.Name())
	if err != nil {
		return err
	}
	for _, seg := range segments {
		if err = c.replayOneSegment(ctx, v.CloudVolumeId, seg.SegmentID, img); err != nil {
			err = fmt.Errorf("replay %s: %w", seg.Label, err)
			break
		}
	}
	if err == nil {
		err = img.Sync()
	}
	if closeErr := img.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("rebuild image: %w", err)
	}
	head := segments[len(segments)-1].Label
	payload, intent, err := c.compressCheckpoint(ctx, v, image.Name(), stream, head)
	if payload != "" {
		defer os.Remove(payload)
	}
	if err != nil {
		return err
	}
	if _, err := c.uploadAttachedCheckpoint(ctx, v, nonce, intent, payload, len(segments), start); err != nil {
		return err
	}
	// Install the acknowledged stream, as detach does after its first
	// checkpoint. A staged detach intent proposed this same stream.
	return writeRecoveryFile(v.DiskPath, acceleratorStreamFile, []byte(stream))
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
