package diskio

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"miren.dev/lbd"
	"miren.dev/runtime/api/storage/storage_v1alpha"
)

// LogSegmentUploader uploads completed log segments to cloud storage.
type LogSegmentUploader interface {
	// UploadSegment uploads a log segment and returns the cloud segment ID.
	UploadSegment(ctx context.Context, volumeID, segmentPath string) (segmentID string, err error)
}

type logSegmentBatchUploader interface {
	UploadSegments(context.Context, string, string, []string, func(string) error) error
}

// Directory metadata is bounded independently of backlog size. A window
// amortizes directory enumeration over many small transport batches.
const logScanWindow = 1024

func oldestSegments(ctx context.Context, logDir string) ([]string, error) {
	dir, err := os.Open(logDir)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	names := make([]string, 0, logScanWindow)
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		entries, err := dir.ReadDir(256)
		if err != nil && err != io.EOF {
			return nil, err
		}
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() || !strings.HasSuffix(name, ".log") {
				continue
			}
			if len(names) == logScanWindow && name >= names[len(names)-1] {
				continue
			}
			i := sort.SearchStrings(names, name)
			if len(names) < logScanWindow {
				names = append(names, "")
			}
			copy(names[i+1:], names[i:len(names)-1])
			names[i] = name
		}
		if err == io.EOF {
			return names, nil
		}
	}
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
		names, err := oldestSegments(ctx, logDir)
		if err != nil {
			if !os.IsNotExist(err) {
				w.log.Warn("failed to read log directory", "path", logDir, "error", err)
			}
			continue
		}
		horizon, err := readLogHorizon(vol.DiskPath)
		if err != nil {
			w.log.Warn("failed to read log horizon, pausing this volume's scan",
				"path", vol.DiskPath, "error", err)
			continue
		}
		// A previous rename may have succeeded just before fsync failed.
		// Make the observed horizon durable before trusting it for deletion.
		if horizon != "" {
			if err := syncLogHorizon(vol.DiskPath); err != nil {
				w.log.Warn("failed to sync log horizon, pausing this volume's scan", "path", vol.DiskPath, "error", err)
				continue
			}
		}

		var paths []string
		for _, name := range names {
			segPath := filepath.Join(logDir, name)
			label := lbd.LabelFromLogPath(segPath)
			if isValidTAI64NLabel(label) && label <= horizon {
				// Replay can also advance the horizon past local segments that were
				// never uploaded. They cannot be replayed after this boundary;
				// do not reintroduce stale writes into the cloud history.
				if err := os.Remove(segPath); err != nil {
					w.log.Warn("failed to remove segment", "path", segPath, "error", err)
				} else {
					w.log.Warn("removed segment covered by log horizon without upload",
						"path", segPath, "horizon", horizon)
				}
				continue
			}
			paths = append(paths, segPath)
		}

		retire := func(segPath string) error {
			if err := updateLogHorizonFromPath(vol.DiskPath, segPath); err != nil {
				return fmt.Errorf("updating log horizon for %s: %w", segPath, err)
			}
			if err := os.Remove(segPath); err != nil {
				w.log.Warn("failed to remove segment", "path", segPath, "error", err)
			}
			return nil
		}
		for len(paths) > 0 {
			if err := ctx.Err(); err != nil {
				return
			}
			if batch, ok := w.uploader.(logSegmentBatchUploader); ok {
				n := min(uploadConcurrency, len(paths))
				if err := batch.UploadSegments(ctx, vol.CloudVolumeId, vol.DiskPath, paths[:n], retire); err != nil {
					w.log.Warn("failed to upload segments, pausing this volume's scan", "path", paths[0], "error", err)
					break
				}
				paths = paths[n:]
				continue
			}
			path := paths[0]
			if w.uploader != nil {
				if _, err := w.uploader.UploadSegment(ctx, vol.CloudVolumeId, path); err != nil {
					w.log.Warn("failed to upload segment, pausing this volume's scan", "path", path, "error", err)
					break
				}
			}
			if err := retire(path); err != nil {
				w.log.Warn("failed to update log horizon, pausing this volume's scan", "path", path, "error", err)
				break
			}
			paths = paths[1:]
		}
	}
}
