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
	"miren.dev/runtime/pkg/snapshot"
)

const checkpointIntentFile = "accelerator-checkpoint.json"
const checkpointPayloadFile = "accelerator-checkpoint.snapshot"

// checkpointPublishedFile records the last boundary the cloud acknowledged, so
// a detach with no new writes does not upload the same image again.
const checkpointPublishedFile = "accelerator-checkpoint-published"

// checkpointConflictFile holds the local stream (empty for a legacy volume)
// that the cloud refused as a conflict. While the stream file still matches,
// detach skips checkpoints instead of uploading an image that cannot publish.
const checkpointConflictFile = "accelerator-checkpoint-conflict"

// DefaultCheckpointTimeout caps a detach checkpoint, which runs while the
// writer lease is held and so delays failover. A missed detach checkpoint loses
// nothing: the drained tail is durable and attached checkpoints cover the gap.
const DefaultCheckpointTimeout = 2 * time.Minute

type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (r ctxReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}

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
	// A bounded caller such as Shutdown must still fit the lease release in
	// the same context after a full checkpoint timeout.
	if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < c.checkpointTimeout+jsonRequestTimeout {
		c.log.Info("skipping accelerator checkpoint under a short deadline", "volume_id", v.CloudVolumeId)
		return
	}
	checkpointCtx, cancel := context.WithTimeout(ctx, c.checkpointTimeout)
	defer cancel()
	err := c.publishAcceleratorCheckpoint(checkpointCtx, v, mount.LeaseNonce)
	switch {
	case err == nil:
	case errors.Is(err, errCheckpointUnsupported):
		c.checkpointsUnsupported.Store(true)
		c.log.Info("cloud does not support accelerator checkpoints; keeping full log history", "volume_id", v.CloudVolumeId)
	case errors.Is(err, errCheckpointStreamConflict):
		c.log.Warn("cloud refused the checkpoint stream; skipping checkpoints until the stream changes",
			"volume_id", v.CloudVolumeId, "error", err)
	case checkpointCtx.Err() != nil && ctx.Err() == nil:
		// The staged bytes go, but the intent stays: it holds a legacy
		// volume's proposed stream, which the cloud may already have accepted.
		if rmErr := os.Remove(filepath.Join(v.DiskPath, checkpointPayloadFile)); rmErr != nil && !os.IsNotExist(rmErr) {
			c.log.Warn("failed to discard abandoned checkpoint", "volume_id", v.CloudVolumeId, "error", rmErr)
		}
		c.log.Warn("accelerator checkpoint exceeded its timeout; abandoned", "volume_id", v.CloudVolumeId,
			"timeout", c.checkpointTimeout)
	default:
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
	conflictPath := filepath.Join(v.DiskPath, checkpointConflictFile)
	if conflict, err := os.ReadFile(conflictPath); err == nil && string(conflict) == string(stream) {
		return nil
	} else if err == nil {
		if err := os.Remove(conflictPath); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	localStream := stream
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
		// equal the cloud's head; restage, but keep the proposed stream. An
		// abandoned checkpoint keeps only its intent, so it restages too. The
		// old intent stays until staging replaces it, so a failed restage
		// still keeps a legacy volume's proposed stream.
		_, statErr := os.Stat(payloadPath)
		if intent.Publication.Base.ThroughLabel != horizon || os.IsNotExist(statErr) {
			if len(stream) == 0 {
				stream = []byte(intent.Publication.StreamID)
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
		if intent, err = c.stageCheckpoint(ctx, v, string(stream), horizon, payloadPath); err != nil {
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
	if errors.Is(err, errCheckpointUnsupported) || errors.Is(err, errCheckpointStreamConflict) {
		// Staged bytes would only be refused again.
		_ = os.Remove(intentPath)
		_ = os.Remove(payloadPath)
		if errors.Is(err, errCheckpointStreamConflict) {
			if markErr := writeRecoveryFile(v.DiskPath, checkpointConflictFile, localStream); markErr != nil {
				return errors.Join(err, markErr)
			}
		}
		return err
	}
	if errors.Is(err, errCheckpointClaimRefused) {
		// The same staged bytes would be refused again. The intent stays: it
		// holds a legacy volume's proposed stream, and the next detach
		// restages under it.
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
// Compression reads the whole image, so it stops when ctx does.
func (c *DiskMountController) stageCheckpoint(ctx context.Context, v *VolumeState, stream, horizon, payloadPath string) (checkpointIntent, error) {
	img, err := os.Open(filepath.Join(v.DiskPath, "disk.img"))
	if err != nil {
		return checkpointIntent{}, err
	}
	defer img.Close()
	info, err := img.Stat()
	if err != nil {
		return checkpointIntent{}, err
	}
	staged, err := os.CreateTemp(v.DiskPath, ".image-snapshot-*")
	if err != nil {
		return checkpointIntent{}, err
	}
	defer staged.Close()
	defer os.Remove(staged.Name())
	name := v.Name
	if name == "" {
		name = v.CloudVolumeId
	}
	checksum, err := snapshot.Backup(staged, ctxReader{ctx, img}, name, info.Size(), v.Filesystem)
	if err != nil {
		return checkpointIntent{}, fmt.Errorf("compress image: %w", err)
	}
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
	data, err := json.Marshal(intent)
	if err != nil {
		return checkpointIntent{}, err
	}
	// Retire the old payload, replace the intent, then install the payload. A
	// crash at any step leaves either the old pair or an intent without its
	// payload, which restages under the same stream; never a mismatched pair.
	if err := os.Remove(payloadPath); err != nil && !os.IsNotExist(err) {
		return checkpointIntent{}, err
	}
	if err := writeRecoveryFile(v.DiskPath, checkpointIntentFile, data); err != nil {
		return checkpointIntent{}, err
	}
	if err := os.Rename(staged.Name(), payloadPath); err != nil {
		return checkpointIntent{}, err
	}
	return intent, syncRecoveryDir(v.DiskPath)
}
