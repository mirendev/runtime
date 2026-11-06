package diskio

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"miren.dev/runtime/api/storage/storage_v1alpha"
)

const checkpointIntentFile = "accelerator-checkpoint.json"
const checkpointPayloadFile = "accelerator-checkpoint.snapshot"

// checkpointPublishedFile records the last boundary the cloud acknowledged, so
// a detach with no new writes does not upload the same image again.
const checkpointPublishedFile = "accelerator-checkpoint-published"

// checkpointMinBudget is the least remaining deadline worth spending on a
// full-image checkpoint before releasing the lease.
const checkpointMinBudget = 5 * time.Minute

type checkpointIntent struct {
	Publication CheckpointPublication `json:"publication"`
	OrderingKey string                `json:"ordering_key"`
}

// checkpointDetachedMount runs under the same volume lock and writer lease as
// final-log drain, after detach, so no checkpoint comes from an attached image.
// It provides filesystem crash consistency, not application quiescence.
//
// It is best-effort and never blocks lease release: the drained tail is already
// durable, so a missed checkpoint loses nothing while the cloud retains logs.
func (c *DiskMountController) checkpointDetachedMount(ctx context.Context, mount *MountState) {
	if mount.Mode != storage_v1alpha.VM_ACCELERATOR || mount.LeaseNonce == "" || mount.BackingPath == "" ||
		mount.DevicePath != "" || mount.Mounted || c.updatesClient == nil || c.checkpointsUnsupported.Load() {
		return
	}
	v := c.state.GetVolume(mount.VolumeId)
	if v == nil || v.CloudVolumeId == "" {
		return
	}
	// Compression and upload can outlast a bounded caller such as Shutdown,
	// whose lease release must still fit in the same context.
	if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < checkpointMinBudget {
		c.log.Info("skipping accelerator checkpoint under a short deadline", "volume_id", v.CloudVolumeId)
		return
	}
	err := c.publishAcceleratorCheckpoint(ctx, v, mount.LeaseNonce)
	if errors.Is(err, errCheckpointUnsupported) {
		c.checkpointsUnsupported.Store(true)
		c.log.Info("cloud does not support accelerator checkpoints; keeping full log history", "volume_id", v.CloudVolumeId)
	} else if err != nil {
		c.log.Warn("accelerator checkpoint failed; will retry at next detach", "volume_id", v.CloudVolumeId, "error", err)
	}
}

// publishAcceleratorCheckpoint uploads the detached image as lbd_checkpoint.
// A legacy volume has no stream yet; its first acknowledged checkpoint starts
// one, which is how existing volumes migrate without a separate step.
func (c *DiskMountController) publishAcceleratorCheckpoint(ctx context.Context, v *VolumeState, nonce string) error {
	if c.updatesClient == nil || nonce == "" {
		return fmt.Errorf("checkpoint requires an updates client and an owned lease")
	}
	// An interrupted install leaves an image ahead of its recorded horizon; the
	// next mount rebuilds it.
	if _, err := os.Stat(filepath.Join(v.DiskPath, acceleratorInstallFile)); err == nil {
		return fmt.Errorf("interrupted accelerator install is not a checkpoint")
	} else if !os.IsNotExist(err) {
		return err
	}
	intentPath := filepath.Join(v.DiskPath, checkpointIntentFile)
	payloadPath := filepath.Join(v.DiskPath, checkpointPayloadFile)
	horizon, err := readLogHorizon(v.DiskPath)
	if err != nil {
		return err
	}
	stream, err := os.ReadFile(filepath.Join(v.DiskPath, acceleratorStreamFile))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if len(stream) > 0 {
		published, err := os.ReadFile(filepath.Join(v.DiskPath, checkpointPublishedFile))
		if err == nil && strings.TrimSpace(string(published)) == horizon {
			return nil
		} else if err != nil && !os.IsNotExist(err) {
			return err
		}
	}

	var intent checkpointIntent
	data, err := os.ReadFile(intentPath)
	if err == nil {
		if err := json.Unmarshal(data, &intent); err != nil {
			return err
		}
		if intent.Publication.VolumeID != v.CloudVolumeId || (len(stream) > 0 && intent.Publication.StreamID != string(stream)) {
			return fmt.Errorf("staged checkpoint belongs to another stream/volume")
		}
		// Once the log has moved past its boundary, the boundary can no longer
		// equal the cloud's head; restage, but keep the proposed stream.
		if intent.Publication.Base.ThroughLabel != horizon {
			if len(stream) == 0 {
				stream = []byte(intent.Publication.StreamID)
			}
			if err := os.Remove(intentPath); err != nil {
				return err
			}
			if err := syncRecoveryDir(v.DiskPath); err != nil {
				return err
			}
			err = os.ErrNotExist
		}
	}
	if err != nil && !os.IsNotExist(err) {
		return err
	} else if err != nil {
		if len(stream) == 0 {
			// The proposal becomes durable only when the cloud acknowledges it.
			id := make([]byte, 16)
			if _, err := rand.Read(id); err != nil {
				return err
			}
			stream = []byte(hex.EncodeToString(id))
		}
		if intent, err = c.stageCheckpoint(v, string(stream), horizon, payloadPath); err != nil {
			return err
		}
	}

	f, err := os.Open(payloadPath)
	if err != nil {
		return err
	}
	defer f.Close()
	hash := sha256.New()
	n, err := io.Copy(hash, f)
	if err != nil {
		return err
	}
	if n != intent.Publication.Base.Size || hex.EncodeToString(hash.Sum(nil)) != intent.Publication.Base.SHA256 {
		return fmt.Errorf("staged checkpoint payload changed")
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	id, err := c.updatesClient.Upload(ctx, v.CloudVolumeId, UploadRequest{
		Kind: KindLBDCheckpoint, OrderingKey: intent.OrderingKey, LeaseNonce: nonce,
		Checkpoint: &intent.Publication,
		Metadata: map[string]any{"accelerator_recovery_version": AcceleratorRecoveryVersion,
			"stream_id": intent.Publication.StreamID, "through_label": intent.Publication.Base.ThroughLabel},
	}, f, n)
	if errors.Is(err, errCheckpointUnsupported) {
		// Nothing was sent; staged bytes would only be refused again.
		_ = os.Remove(intentPath)
		_ = os.Remove(payloadPath)
		return err
	}
	if err != nil {
		return err
	}
	c.log.Info("published accelerator checkpoint", "volume_id", v.CloudVolumeId,
		"checkpoint", id, "through_label", intent.Publication.Base.ThroughLabel)
	// The stream is installed before the intent is retired, so a crash in
	// between resumes with the same stream and boundary.
	if err := writeRecoveryFile(v.DiskPath, acceleratorStreamFile, []byte(intent.Publication.StreamID)); err != nil {
		return err
	}
	if err := writeRecoveryFile(v.DiskPath, checkpointPublishedFile, []byte(intent.Publication.Base.ThroughLabel)); err != nil {
		return err
	}
	if err := os.Remove(intentPath); err != nil {
		return err
	}
	if err := syncRecoveryDir(v.DiskPath); err != nil {
		return err
	}
	return os.Remove(payloadPath)
}

// stageCheckpoint durably writes the compressed image and its intent before
// any remote call, so retries reuse the same checksum-bound claim.
func (c *DiskMountController) stageCheckpoint(v *VolumeState, stream, horizon, payloadPath string) (checkpointIntent, error) {
	req := SnapshotRequest{VolumeID: v.CloudVolumeId, ImagePath: filepath.Join(v.DiskPath, "disk.img"),
		Name: v.Name, Filesystem: v.Filesystem}
	info, err := os.Stat(req.ImagePath)
	if err != nil {
		return checkpointIntent{}, err
	}
	staged, checksum, err := NewImageSnapshotter(c.log, c.updatesClient).stage(req, info)
	if err != nil {
		return checkpointIntent{}, err
	}
	defer staged.Close()
	defer os.Remove(staged.Name())
	if err := staged.Sync(); err != nil {
		return checkpointIntent{}, err
	}
	if _, err := staged.Seek(0, io.SeekStart); err != nil {
		return checkpointIntent{}, err
	}
	hash := sha256.New()
	size, err := io.Copy(hash, staged)
	if err != nil {
		return checkpointIntent{}, err
	}
	digest := hex.EncodeToString(hash.Sum(nil))
	// Bind the stable update key to both bytes and boundary. Retrying a
	// lost response reuses the exact original checksum-bound claim.
	key := sha256.Sum256([]byte(stream + ":" + horizon + ":" + digest))
	intent := checkpointIntent{OrderingKey: hex.EncodeToString(key[:8]), Publication: CheckpointPublication{
		Version: AcceleratorRecoveryVersion, VolumeID: v.CloudVolumeId, StreamID: stream,
		Base: RecoveryCheckpoint{RecoveryObject: RecoveryObject{Size: size, SHA256: digest},
			ThroughLabel: horizon, ImageSize: info.Size(), ImageSHA256: checksum},
	}}
	if err := os.Rename(staged.Name(), payloadPath); err != nil {
		return checkpointIntent{}, err
	}
	data, err := json.Marshal(intent)
	if err != nil {
		return checkpointIntent{}, err
	}
	return intent, writeRecoveryFile(v.DiskPath, checkpointIntentFile, data)
}
