package diskio

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"miren.dev/runtime/api/storage/storage_v1alpha"
)

// LogSegmentUploader uploads completed log segments to cloud storage.
type LogSegmentUploader interface {
	// UploadSegment uploads a log segment and returns the cloud segment ID.
	UploadSegment(ctx context.Context, volumeID, segmentPath string) (segmentID string, err error)
}

// LogWatcher monitors accelerator volume log directories for completed segments.
// When an uploader is configured, segments are uploaded then removed.
// When no uploader is configured (cloud not available), segments are simply deleted.
type LogWatcher struct {
	log      *slog.Logger
	state    *State
	uploader LogSegmentUploader // nil when cloud is not configured
	interval time.Duration
	done     chan struct{}
}

// NewLogWatcher creates a new LogWatcher that scans at the given interval.
// Pass nil for uploader to just delete logs without uploading.
func NewLogWatcher(log *slog.Logger, state *State, uploader LogSegmentUploader, interval time.Duration) *LogWatcher {
	return &LogWatcher{
		log:      log.With("module", "log-watcher"),
		state:    state,
		uploader: uploader,
		interval: interval,
		done:     make(chan struct{}),
	}
}

// Run starts the log watcher loop. It blocks until the context is cancelled.
func (w *LogWatcher) Run(ctx context.Context) error {
	defer close(w.done)

	if w.interval <= 0 {
		return fmt.Errorf("log watcher interval must be positive, got %s", w.interval)
	}
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			w.scanAndUpload(ctx)
		}
	}
}

// Wait blocks until the watcher's Run method has returned.
func (w *LogWatcher) Wait() {
	<-w.done
}

func (w *LogWatcher) scanAndUpload(ctx context.Context) {
	for _, vol := range w.state.ListVolumes() {
		if vol.Mode != storage_v1alpha.VM_ACCELERATOR {
			continue
		}

		// A volume with no cloud id has not been registered yet. Uploading
		// would fail, and deleting would throw away a segment that was never
		// backed up, so leave both alone: registration is retried on every
		// reconcile and the next scan will pick these up.
		if w.uploader != nil && vol.CloudVolumeId == "" {
			w.log.Debug("volume not registered with miren.cloud, deferring segment upload",
				"volume_id", vol.VolumeId)
			continue
		}

		logDir := filepath.Join(vol.DiskPath, "logs")
		entries, err := os.ReadDir(logDir)
		if err != nil {
			if !os.IsNotExist(err) {
				w.log.Warn("failed to read log directory", "path", logDir, "error", err)
			}
			continue
		}

		for _, e := range entries {
			if e.IsDir() {
				continue
			}

			name := e.Name()

			// Only process completed .log files, skip .log.tmp (in-progress)
			if !strings.HasSuffix(name, ".log") || strings.HasSuffix(name, ".log.tmp") {
				continue
			}

			segPath := filepath.Join(logDir, name)

			if w.uploader != nil {
				_, err := w.uploader.UploadSegment(ctx, vol.CloudVolumeId, segPath)
				if err != nil {
					// Stop this volume's scan rather than moving to the next
					// segment. Entries are sorted and TAI64N labels sort
					// chronologically, so uploading a later segment would
					// advance the horizon past this one, and replay only asks
					// for what sorts after the horizon. The gap would never be
					// requested again. Leave the file and retry next scan.
					w.log.Warn("failed to upload segment, pausing this volume's scan",
						"path", segPath, "error", err)
					break
				}
			}

			// Update the log horizon so replay won't re-apply this segment
			if err := updateLogHorizonFromPath(vol.DiskPath, segPath); err != nil {
				// Same reasoning: if the horizon did not move, nothing later
				// may move it past this segment.
				w.log.Warn("failed to update log horizon, pausing this volume's scan",
					"path", segPath, "error", err)
				break
			}

			if err := os.Remove(segPath); err != nil {
				w.log.Warn("failed to remove segment", "path", segPath, "error", err)
			}
		}
	}
}
