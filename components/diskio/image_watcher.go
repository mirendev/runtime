package diskio

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"miren.dev/runtime/api/storage/storage_v1alpha"
	"miren.dev/runtime/pkg/snapshot"
)

// DefaultImageSnapshotInterval is how often universal-mode volumes are
// considered for a snapshot.
//
// A loop_image update is a *full* compressed image, so every upload re-sends the
// whole thing. RFD-0064 argued against continuous backup for universal mode for
// this reason. The interval is deliberately long, and an unchanged image is
// skipped outright.
const DefaultImageSnapshotInterval = time.Hour

// imageMarkerFile records what was last uploaded, so an unchanged image is not
// snapshotted again. It sits beside the disk, like the accelerator path's
// log_horizon.
const imageMarkerFile = "image_snapshot"

// imageMarker is the last-uploaded state of a volume's backing image.
type imageMarker struct {
	SizeBytes int64     `json:"size_bytes"`
	ModTime   time.Time `json:"mod_time"`
	// OrderingKey of the update this marker describes, for diagnostics
	OrderingKey string `json:"ordering_key"`
}

// ImageWatcher periodically snapshots universal-mode volumes and uploads them
// as loop_image updates.
//
// Accelerator-mode volumes are left alone: lbd already logs every write, and
// LogWatcher ships those segments.
type ImageWatcher struct {
	log      *slog.Logger
	state    *State
	updates  CloudUpdatesClient
	interval time.Duration
	done     chan struct{}
}

// NewImageWatcher creates a watcher over the given state. A nil updates client
// disables it, matching how LogWatcher tolerates an unconfigured cloud.
func NewImageWatcher(log *slog.Logger, state *State, updates CloudUpdatesClient, interval time.Duration) *ImageWatcher {
	return &ImageWatcher{
		log:      log.With("module", "image-watcher"),
		state:    state,
		updates:  updates,
		interval: interval,
		done:     make(chan struct{}),
	}
}

// Run starts the watcher loop. It blocks until the context is cancelled.
func (w *ImageWatcher) Run(ctx context.Context) error {
	defer close(w.done)

	if w.interval <= 0 {
		return fmt.Errorf("image watcher interval must be positive, got %s", w.interval)
	}

	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			w.snapshotAll(ctx)
		}
	}
}

// Wait blocks until Run has returned.
func (w *ImageWatcher) Wait() {
	<-w.done
}

// snapshotAll considers every universal-mode volume once.
func (w *ImageWatcher) snapshotAll(ctx context.Context) {
	if w.updates == nil {
		return
	}

	for _, vol := range w.state.ListVolumes() {
		if vol.Mode != storage_v1alpha.VM_UNIVERSAL {
			continue
		}
		if err := w.snapshotVolume(ctx, vol); err != nil {
			// One volume's failure must not stop the others; the next tick
			// retries.
			w.log.Warn("failed to snapshot volume image",
				"volume_id", vol.VolumeId,
				"error", err,
			)
		}
	}
}

// snapshotVolume uploads one volume's image if it has changed since last time.
func (w *ImageWatcher) snapshotVolume(ctx context.Context, vol *VolumeState) error {
	imagePath := filepath.Join(vol.DiskPath, "disk.img")

	info, err := os.Stat(imagePath)
	if err != nil {
		if os.IsNotExist(err) {
			// Nothing to back up yet
			return nil
		}
		return fmt.Errorf("stat image: %w", err)
	}

	if unchanged(readImageMarker(vol.DiskPath), info) {
		w.log.Debug("image unchanged since last snapshot, skipping",
			"volume_id", vol.VolumeId,
			"image", imagePath,
		)
		return nil
	}

	// The ordering key must sort with its siblings and match the cloud's
	// 16-lowercase-hex rule for this kind. Unix nanoseconds satisfy both and
	// need no persisted counter.
	orderingKey := fmt.Sprintf("%016x", time.Now().UnixNano())

	staged, checksum, err := w.stageSnapshot(vol, imagePath, info)
	if err != nil {
		return err
	}
	defer os.Remove(staged.Name())
	defer staged.Close()

	stagedInfo, err := staged.Stat()
	if err != nil {
		return fmt.Errorf("stat staged snapshot: %w", err)
	}

	if _, err := staged.Seek(0, 0); err != nil {
		return fmt.Errorf("rewind staged snapshot: %w", err)
	}

	updateID, err := w.updates.Upload(ctx, vol.VolumeId, UploadRequest{
		Kind:        KindLoopImage,
		OrderingKey: orderingKey,
		Metadata: map[string]any{
			"compression":     "zstd",
			"format":          "miren-snapshot",
			"filesystem":      vol.Filesystem,
			"image_size":      info.Size(),
			"image_sha256":    checksum,
			"compressed_size": stagedInfo.Size(),
		},
		LeaseNonce: w.leaseNonceFor(vol.VolumeId),
	}, staged, stagedInfo.Size())
	if err != nil {
		return fmt.Errorf("upload image snapshot: %w", err)
	}

	// Record only after a confirmed upload, so a failure retries next tick
	if err := writeImageMarker(vol.DiskPath, imageMarker{
		SizeBytes:   info.Size(),
		ModTime:     info.ModTime(),
		OrderingKey: orderingKey,
	}); err != nil {
		w.log.Warn("failed to record image snapshot marker",
			"volume_id", vol.VolumeId,
			"error", err,
		)
	}

	w.log.Info("uploaded volume image snapshot",
		"volume_id", vol.VolumeId,
		"update_id", updateID,
		"ordering_key", orderingKey,
		"image_size", info.Size(),
		"compressed_size", stagedInfo.Size(),
	)
	return nil
}

// stageSnapshot writes a compressed snapshot to a temp file and returns it,
// positioned anywhere — the caller rewinds.
//
// snapshot.Backup needs an io.WriteSeeker because it rewrites the header with
// the checksum after streaming, so the snapshot cannot go straight into the
// upload body.
func (w *ImageWatcher) stageSnapshot(vol *VolumeState, imagePath string, info os.FileInfo) (*os.File, string, error) {
	img, err := os.Open(imagePath)
	if err != nil {
		return nil, "", fmt.Errorf("open image: %w", err)
	}
	defer img.Close()

	// Stage beside the disk rather than in /tmp, which may be small or on a
	// different filesystem
	staged, err := os.CreateTemp(vol.DiskPath, ".image-snapshot-*")
	if err != nil {
		return nil, "", fmt.Errorf("create staging file: %w", err)
	}

	name := vol.Name
	if name == "" {
		name = vol.VolumeId
	}

	checksum, err := snapshot.Backup(staged, img, name, info.Size(), vol.Filesystem)
	if err != nil {
		staged.Close()
		os.Remove(staged.Name())
		return nil, "", fmt.Errorf("compress image: %w", err)
	}

	return staged, checksum, nil
}

// leaseNonceFor finds the nonce held for a volume, which the cloud requires on
// writes to a leased volume.
func (w *ImageWatcher) leaseNonceFor(volumeID string) string {
	if w.state == nil {
		return ""
	}
	for _, m := range w.state.ListMounts() {
		if m.VolumeId == volumeID && m.LeaseNonce != "" {
			return m.LeaseNonce
		}
	}
	return ""
}

// unchanged reports whether the image looks identical to what was last uploaded.
//
// Size and mtime are a cheap proxy, not a guarantee. They are wrong only in the
// direction of uploading again, which is safe.
func unchanged(marker *imageMarker, info os.FileInfo) bool {
	if marker == nil {
		return false
	}
	return marker.SizeBytes == info.Size() && marker.ModTime.Equal(info.ModTime())
}

func imageMarkerPath(diskPath string) string {
	return filepath.Join(diskPath, imageMarkerFile)
}

// readImageMarker returns nil when there is no usable marker, which reads as
// "never uploaded".
func readImageMarker(diskPath string) *imageMarker {
	data, err := os.ReadFile(imageMarkerPath(diskPath))
	if err != nil {
		return nil
	}
	var marker imageMarker
	if err := json.Unmarshal(data, &marker); err != nil {
		return nil
	}
	return &marker
}

// writeImageMarker replaces the marker atomically, so a crash mid-write cannot
// leave a marker that claims more than was uploaded.
func writeImageMarker(diskPath string, marker imageMarker) error {
	data, err := json.Marshal(marker)
	if err != nil {
		return fmt.Errorf("marshal image marker: %w", err)
	}

	path := imageMarkerPath(diskPath)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return fmt.Errorf("write image marker: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("replace image marker: %w", err)
	}
	return nil
}
