package diskio

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"miren.dev/lbd"
	"miren.dev/runtime/pkg/snapshot"
)

// AcceleratorRecoveryVersion versions the recovery fields on update operations.
const AcceleratorRecoveryVersion = 1

// RecoveryObject binds an immutable payload, not just a listing row.
type RecoveryObject struct {
	UpdateID string `json:"update_id"`
	Size     int64  `json:"size"`
	SHA256   string `json:"sha256"`
}

type RecoverySegment struct {
	RecoveryObject
	Label         string `json:"label"`
	PreviousLabel string `json:"previous_label"`
	FirstSequence uint64 `json:"first_sequence"`
	EntryCount    uint64 `json:"entry_count"`
}

type RecoveryCheckpoint struct {
	RecoveryObject
	ThroughLabel string `json:"through_label"`
	ImageSize    int64  `json:"image_size"`
	ImageSHA256  string `json:"image_sha256"`
}

// RecoveryManifest is one complete, immutable recovery set, protected by a
// renewable session. Count and head detect truncated listings even at EOF.
// The cloud must build predecessor links from authenticated ordered activation,
// never by chaining whatever objects happen to remain in storage.
type RecoveryManifest struct {
	Version       int                 `json:"version"`
	VolumeID      string              `json:"volume_id"`
	StreamID      string              `json:"stream_id"`
	DeviceSize    uint64              `json:"device_size"`
	Status        string              `json:"status"`
	SessionID     string              `json:"session_id"`
	ExpiresAt     time.Time           `json:"expires_at"`
	Complete      bool                `json:"complete"`
	CoverageFloor string              `json:"coverage_floor"`
	Head          string              `json:"head"`
	SegmentCount  int                 `json:"segment_count"`
	Checkpoint    *RecoveryCheckpoint `json:"checkpoint"`
	// Segments are assembled from the existing paginated update rows.
	Segments []RecoverySegment `json:"-"`
}

type CheckpointPublication struct {
	Version  int                `json:"version"`
	VolumeID string             `json:"volume_id"`
	StreamID string             `json:"stream_id"`
	Base     RecoveryCheckpoint `json:"checkpoint"`
}

// protectedUpdatesClient extends update listing with renewable restore
// protection. There is no fallback to an unprotected listing.
type protectedUpdatesClient interface {
	BeginRecovery(context.Context, string, string) (*RecoveryManifest, error)
	RenewRecovery(context.Context, string, string) (time.Time, error)
}

func validRecoveryObject(o RecoveryObject) bool {
	digest, err := hex.DecodeString(o.SHA256)
	return o.UpdateID != "" && o.Size > 0 && err == nil && len(digest) == sha256.Size
}

func (m *RecoveryManifest) validate(volume string) error {
	if m == nil || m.Version != AcceleratorRecoveryVersion || m.VolumeID != volume || m.StreamID == "" ||
		!m.Complete || m.SessionID == "" || !m.ExpiresAt.After(time.Now()) || m.SegmentCount != len(m.Segments) {
		return fmt.Errorf("invalid or incomplete accelerator recovery manifest")
	}
	if m.Status == "never_backed_up" {
		if m.Checkpoint != nil || len(m.Segments) != 0 || m.Head != "" || m.CoverageFloor != "" {
			return fmt.Errorf("never-backed-up manifest contains history")
		}
		return nil
	}
	if m.Status != "ready" || m.Checkpoint == nil {
		return fmt.Errorf("accelerator history is %q, not recoverable", m.Status)
	}
	b := m.Checkpoint
	if m.DeviceSize == 0 || m.DeviceSize > 1<<63-1 || !validRecoveryObject(b.RecoveryObject) || b.ImageSize <= 0 || !validRecoveryObject(RecoveryObject{UpdateID: b.UpdateID, Size: b.ImageSize, SHA256: b.ImageSHA256}) ||
		b.ThroughLabel < m.CoverageFloor || b.ThroughLabel > m.Head {
		return fmt.Errorf("invalid accelerator checkpoint or retained coverage floor")
	}
	for _, label := range []string{b.ThroughLabel, m.CoverageFloor, m.Head} {
		if label != "" && !isValidTAI64NLabel(label) {
			return fmt.Errorf("invalid accelerator recovery boundary %q", label)
		}
	}
	previous := b.ThroughLabel
	seen := map[string]bool{b.UpdateID: true}
	for _, s := range m.Segments {
		if !validRecoveryObject(s.RecoveryObject) || !isValidTAI64NLabel(s.Label) || s.Label <= previous ||
			s.PreviousLabel != previous || seen[s.UpdateID] || s.EntryCount > ^uint64(0)-s.FirstSequence {
			return fmt.Errorf("incomplete accelerator coverage after %q", previous)
		}
		seen[s.UpdateID] = true
		previous = s.Label
	}
	if previous != m.Head {
		return fmt.Errorf("accelerator tail ends at %q, expected head %q", previous, m.Head)
	}
	return nil
}

const acceleratorStreamFile = "accelerator-stream"
const acceleratorInstallFile = "accelerator-installing"

// replayAccelerator keeps legacy replay as the default. Protected recovery is
// additive: only a volume with an established stream on a v1 cloud uses it.
// Falling back on an old cloud is safe only while the cloud retains every log;
// removing the fallback must precede enabling prefix retirement.
func (c *DiskMountController) replayAccelerator(ctx context.Context, v *VolumeState, nonce string) error {
	if _, err := os.Stat(filepath.Join(v.DiskPath, acceleratorStreamFile)); err == nil {
		if _, ok := c.updatesClient.(protectedUpdatesClient); ok {
			err := c.recoverAccelerator(ctx, v, nonce)
			if !errors.Is(err, errRecoveryUnsupported) {
				return err
			}
			c.log.Info("cloud does not support protected accelerator recovery; using legacy replay",
				"volume_id", v.CloudVolumeId)
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	return c.replayMissingSegments(ctx, v)
}

// Recovery always reconstructs from the protected base, not an unproven local
// horizon. This costs a base download on mount but avoids trusting a backing
// image from a different host/incarnation. Local crash-tail logs are drained
// first under the acquired lease and volume lock.
func (c *DiskMountController) recoverAccelerator(ctx context.Context, v *VolumeState, nonce string) error {
	client, ok := c.updatesClient.(protectedUpdatesClient)
	if !ok {
		return errRecoveryUnsupported
	}
	stream, err := os.ReadFile(filepath.Join(v.DiskPath, acceleratorStreamFile))
	if err != nil {
		return err
	}
	names, err := oldestSegments(ctx, filepath.Join(v.DiskPath, "logs"))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if len(names) > 0 {
		// Check stream identity before publishing a crash tail.
		manifest, err := client.BeginRecovery(ctx, v.CloudVolumeId, nonce)
		if err != nil {
			return err
		}
		if manifest.StreamID != string(stream) || manifest.Status != "ready" {
			return fmt.Errorf("accelerator stream changed with local logs outstanding")
		}
		uploader := NewCloudSegmentUploaderWithClient(c.log, c.updatesClient, c.state)
		if err := uploader.drainSegmentsLocked(ctx, v.CloudVolumeId, v.DiskPath); err != nil {
			return err
		}
	}
	m, err := client.BeginRecovery(ctx, v.CloudVolumeId, nonce)
	if err != nil {
		return fmt.Errorf("begin protected accelerator recovery: %w", err)
	}
	// A published stream has a checkpoint; "never backed up" here means the
	// cloud lost history, never permission to start an empty volume.
	if m.Status != "ready" {
		return fmt.Errorf("accelerator history for stream %s is %q, not recoverable", stream, m.Status)
	}
	if m.DeviceSize != uint64(v.SizeBytes) {
		return fmt.Errorf("accelerator manifest device size does not match volume")
	}
	if string(stream) != m.StreamID {
		return fmt.Errorf("accelerator stream incarnation changed; explicit recovery required")
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	// Renew while large downloads/decompression are running, not just between
	// objects. An expired/rejected session cancels reads and prevents install.
	var wg sync.WaitGroup
	var protectionErr error
	wg.Go(func() {
		expiry := m.ExpiresAt
		for {
			wait := time.Until(expiry) / 3
			if wait <= 0 {
				protectionErr = fmt.Errorf("accelerator restore protection expired")
				cancel()
				return
			}
			timer := time.NewTimer(wait)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
			renewCtx, stop := context.WithDeadline(ctx, expiry)
			next, err := client.RenewRecovery(renewCtx, v.CloudVolumeId, m.SessionID)
			stop()
			if err != nil || !next.After(time.Now()) || !time.Now().Before(expiry) {
				protectionErr = fmt.Errorf("accelerator restore protection lost: %v", err)
				cancel()
				return
			}
			expiry = next
		}
	})
	err = c.installAcceleratorRecovery(ctx, client, v, m)
	cancel()
	wg.Wait()
	// The install renews protection itself just before committing, so a
	// completed install stands even if a background renewal was cut short
	// by the cancel above.
	if err != nil {
		if protectionErr != nil {
			return protectionErr
		}
		return err
	}
	c.log.Info("restored accelerator checkpoint and tail", "volume_id", v.CloudVolumeId,
		"checkpoint", m.Checkpoint.UpdateID, "head", m.Head, "segments", len(m.Segments))
	return nil
}

// writeRecoveryFile persists a small transition record before any rename that
// depends on it. Interrupted installs are rebuilt, never exposed as complete.
func writeRecoveryFile(dir, name string, data []byte) error {
	f, err := os.CreateTemp(dir, ".recovery-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(f.Name(), filepath.Join(dir, name)); err != nil {
		return err
	}
	return syncRecoveryDir(dir)
}

func syncRecoveryDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

func (c *DiskMountController) downloadRecoveryObject(ctx context.Context, volume, dir string, obj RecoveryObject) (*os.File, error) {
	body, err := c.updatesClient.Download(ctx, volume, obj.UpdateID)
	if err != nil {
		return nil, err
	}
	defer body.Close()
	f, err := os.CreateTemp(dir, ".recovery-object-*")
	if err != nil {
		return nil, err
	}
	hash := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, hash), io.LimitReader(body, obj.Size+1))
	if err == nil && (n != obj.Size || hex.EncodeToString(hash.Sum(nil)) != obj.SHA256) {
		err = fmt.Errorf("recovery object %s size/digest mismatch", obj.UpdateID)
	}
	if err == nil {
		_, err = f.Seek(0, io.SeekStart)
	}
	if err != nil {
		f.Close()
		os.Remove(f.Name())
		return nil, err
	}
	return f, nil
}

func (c *DiskMountController) installAcceleratorRecovery(ctx context.Context, client protectedUpdatesClient, v *VolumeState, m *RecoveryManifest) error {
	base, err := c.downloadRecoveryObject(ctx, v.CloudVolumeId, v.DiskPath, m.Checkpoint.RecoveryObject)
	if err != nil {
		return err
	}
	defer os.Remove(base.Name())
	defer base.Close()
	meta, err := snapshot.ReadHeader(base)
	if err != nil {
		return err
	}
	if meta.SizeBytes != m.Checkpoint.ImageSize || meta.Checksum != m.Checkpoint.ImageSHA256 {
		return fmt.Errorf("checkpoint image identity mismatch")
	}
	image, err := os.CreateTemp(v.DiskPath, ".accelerator-image-*")
	if err != nil {
		return err
	}
	defer os.Remove(image.Name())
	defer image.Close()
	if err := image.Truncate(meta.SizeBytes); err != nil {
		return err
	}
	if err := snapshot.RestoreImage(image, base, meta); err != nil {
		return err
	}
	if size, err := image.Seek(0, io.SeekCurrent); err != nil || size != meta.SizeBytes {
		return fmt.Errorf("checkpoint decompressed size mismatch: got %d, expected %d (%v)", size, meta.SizeBytes, err)
	}
	if err := image.Close(); err != nil {
		return err
	}
	img, err := openDiskImage(image.Name())
	if err != nil {
		return err
	}
	defer img.Close()
	for _, s := range m.Segments {
		f, err := c.downloadRecoveryObject(ctx, v.CloudVolumeId, v.DiskPath, s.RecoveryObject)
		if err != nil {
			return err
		}
		err = replayRecoverySegment(f, img, s, m.DeviceSize)
		f.Close()
		os.Remove(f.Name())
		if err != nil {
			return fmt.Errorf("replay %s: %w", s.Label, err)
		}
	}
	if err := img.Sync(); err != nil {
		return err
	}
	if err := img.Close(); err != nil {
		return err
	}
	// Reauthenticate immediately before committing the selected recovery set.
	expiry, err := client.RenewRecovery(ctx, v.CloudVolumeId, m.SessionID)
	if err != nil || !expiry.After(time.Now()) {
		return fmt.Errorf("accelerator restore protection unavailable at install: %v", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := writeRecoveryFile(v.DiskPath, acceleratorInstallFile, []byte(m.StreamID+"\n"+m.Head)); err != nil {
		return err
	}
	if err := os.Rename(image.Name(), filepath.Join(v.DiskPath, "disk.img")); err != nil {
		return err
	}
	if err := writeLogHorizon(v.DiskPath, m.Head); err != nil {
		return err
	}
	if err := writeRecoveryFile(v.DiskPath, acceleratorStreamFile, []byte(m.StreamID)); err != nil {
		return err
	}
	return os.Remove(filepath.Join(v.DiskPath, acceleratorInstallFile))
}

func replayRecoverySegment(src io.Reader, img diskImage, s RecoverySegment, deviceSize uint64) error {
	rd, err := lbd.NewReader(src)
	if err != nil {
		return err
	}
	h := rd.Header
	if h.Version != 2 || h.SegmentLabel != s.Label || h.BlockSize == 0 || h.DeviceSize != deviceSize {
		return fmt.Errorf("invalid LBD header identity")
	}
	var count uint64
	for {
		e, err := rd.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if count >= s.EntryCount || e.Sequence != s.FirstSequence+count || e.Length == 0 ||
			e.Block > h.DeviceSize/uint64(h.BlockSize) || uint64(e.Length) > h.DeviceSize-e.Block*uint64(h.BlockSize) ||
			(!e.IsWrite() && !e.IsTrim()) || (e.IsWrite() && (len(e.Data) != int(e.Length) || !e.ValidateCRC())) {
			return fmt.Errorf("invalid LBD entry sequence, bounds, operation or CRC")
		}
		offset := int64(e.Block * uint64(h.BlockSize))
		if e.IsWrite() {
			if _, err := img.WriteAt(e.Data, offset); err != nil {
				return err
			}
		} else if err := img.Trim(offset, int(e.Length)); err != nil {
			return err
		}
		count++
	}
	if count != s.EntryCount {
		return fmt.Errorf("LBD entry count mismatch: got %d, expected %d", count, s.EntryCount)
	}
	return nil
}
