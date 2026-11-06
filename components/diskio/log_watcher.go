package diskio

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
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

// Each turn publishes at most one transport batch. A FIFO of pending volumes
// prevents large backlogs from taking every turn when all workers are busy.
const logVolumeConcurrency = 4

// Per-volume failure backoff. Retry-After can extend a wait to at most
// maxLogRetryDelay so one bad header cannot park a volume for hours.
const (
	minLogRetryDelay = time.Second
	maxLogRetryDelay = time.Minute
)

type logVolumeScan struct {
	volume *VolumeState
	paths  []string
	more   bool
	err    error
}

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
	discovery := time.NewTimer(0)
	defer discovery.Stop()
	nextDiscovery := time.Now()

	completed := make(chan *logVolumeScan, logVolumeConcurrency)
	active := make(map[string]bool)
	queued := make(map[string]bool)
	type retry struct {
		until time.Time
		delay time.Duration
	}
	retries := make(map[string]retry)
	var pending []*logVolumeScan
	// Shutdown must join uploads before the runner tears down their dependencies.
	defer func() {
		for len(active) > 0 {
			delete(active, (<-completed).volume.EntityId)
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-discovery.C:
			now := time.Now()
			// A due retry wakes only its own volume; only the interval
			// starts a full discovery pass.
			full := !now.Before(nextDiscovery)
			if full {
				nextDiscovery = now.Add(w.interval)
			}
			for _, vol := range w.state.ListVolumes() {
				r, retrying := retries[vol.EntityId]
				if (full || retrying) && vol.Mode == storage_v1alpha.VM_ACCELERATOR && !active[vol.EntityId] && !queued[vol.EntityId] && !r.until.After(now) {
					pending = append(pending, &logVolumeScan{volume: vol})
					queued[vol.EntityId] = true
				}
			}
		case scan := <-completed:
			id := scan.volume.EntityId
			delete(active, id)
			if scan.err != nil {
				delay := min(max(minLogRetryDelay, retries[id].delay*2), maxLogRetryDelay)
				// Jitter within the upper half so the first retry still waits
				// at least half a second rather than retrying immediately.
				wait := delay/2 + time.Duration(rand.Int64N(int64(delay/2)+1))
				var response *cloudUpdateHTTPError
				if errors.As(scan.err, &response) {
					wait = max(wait, min(response.retryAfter, maxLogRetryDelay))
					if response.status >= 400 && response.status < 500 && response.status != 429 {
						wait = max(wait, maxLogRetryDelay)
					}
				}
				retries[id] = retry{until: time.Now().Add(wait), delay: delay}
			} else {
				delete(retries, id)
			}
			if scan.err == nil && scan.more {
				pending = append(pending, scan)
				queued[id] = true
			}
		}
		for len(pending) > 0 && len(active) < logVolumeConcurrency && ctx.Err() == nil {
			scan := pending[0]
			pending[0] = nil
			pending = pending[1:]
			id := scan.volume.EntityId
			delete(queued, id)
			// Resolve queued state at dispatch rather than retaining stale paths
			// across a deletion or registration transition.
			vol := w.state.GetVolume(id)
			if vol == nil {
				continue
			}
			if vol.DiskPath != scan.volume.DiskPath || vol.CloudVolumeId != scan.volume.CloudVolumeId {
				scan.paths = nil
			}
			scan.volume = vol
			active[id] = true
			go func() {
				scan.paths, scan.more, scan.err = w.scanVolume(ctx, vol, scan.paths)
				completed <- scan
			}()
		}
		next := nextDiscovery
		for id, retry := range retries {
			// Drop entries for volumes that are gone or no longer upload logs;
			// otherwise a past-due entry would fire the timer in a tight loop.
			if vol := w.state.GetVolume(id); vol == nil || vol.Mode != storage_v1alpha.VM_ACCELERATOR {
				delete(retries, id)
				continue
			}
			if !active[id] && !queued[id] && retry.until.Before(next) {
				next = retry.until
			}
		}
		discovery.Reset(max(0, time.Until(next)))
	}
}

// Wait blocks until the watcher's Run method has returned.
func (w *LogWatcher) Wait() {
	<-w.done
}

func (w *LogWatcher) scanVolume(ctx context.Context, vol *VolumeState, paths []string) ([]string, bool, error) {
	if vol.Mode != storage_v1alpha.VM_ACCELERATOR {
		return nil, false, nil
	}

	// A volume with no cloud id has not been registered yet. Uploading
	// would fail, and deleting would throw away a segment that was never
	// backed up, so leave both alone: registration is retried on every
	// reconcile and the next scan will pick these up.
	if w.uploader != nil && vol.CloudVolumeId == "" {
		w.log.Debug("volume not registered with miren.cloud, deferring segment upload",
			"volume_id", vol.VolumeId)
		return nil, false, nil
	}

	if len(paths) == 0 {
		logDir := filepath.Join(vol.DiskPath, "logs")
		names, err := oldestSegments(ctx, logDir)
		if err != nil {
			if !os.IsNotExist(err) {
				w.log.Warn("failed to read log directory", "path", logDir, "error", err)
			}
			return nil, false, nil
		}
		for _, name := range names {
			paths = append(paths, filepath.Join(logDir, name))
		}
	}
	if len(paths) == 0 {
		return nil, false, nil
	}
	horizon, err := readLogHorizon(vol.DiskPath)
	if err != nil {
		w.log.Warn("failed to read log horizon, pausing this volume's scan",
			"path", vol.DiskPath, "error", err)
		return nil, false, err
	}
	// A previous rename may have succeeded just before fsync failed.
	// Make the observed horizon durable before trusting it for deletion.
	if horizon != "" {
		if err := syncLogHorizon(vol.DiskPath); err != nil {
			w.log.Warn("failed to sync log horizon, pausing this volume's scan", "path", vol.DiskPath, "error", err)
			return nil, false, err
		}
	}

	var uncovered []string
	for _, segPath := range paths {
		label := lbd.LabelFromLogPath(segPath)
		if isValidTAI64NLabel(label) && label <= horizon {
			// Replay can also advance the horizon past local segments that were
			// never uploaded. They cannot be replayed after this boundary;
			// do not reintroduce stale writes into the cloud history.
			if err := os.Remove(segPath); err != nil && !os.IsNotExist(err) {
				w.log.Warn("failed to remove segment", "path", segPath, "error", err)
				return nil, false, err
			} else if err == nil {
				w.log.Warn("removed segment covered by log horizon without upload",
					"path", segPath, "horizon", horizon)
			}
			continue
		}
		uncovered = append(uncovered, segPath)
	}
	paths = uncovered

	retire := func(segPath string) error {
		if err := updateLogHorizonFromPath(vol.DiskPath, segPath); err != nil {
			return fmt.Errorf("updating log horizon for %s: %w", segPath, err)
		}
		if err := os.Remove(segPath); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	n := min(uploadConcurrency, len(paths))
	if _, ok := w.uploader.(logSegmentBatchUploader); ok {
		n, err = uploadTurnSize(paths)
		if err != nil {
			w.log.Warn("failed to inspect segment, pausing this volume's scan", "error", err)
			return nil, false, err
		}
	}
	remaining := paths[n:]
	paths = paths[:n]
	for len(paths) > 0 {
		if err := ctx.Err(); err != nil {
			return nil, false, err
		}
		if batch, ok := w.uploader.(logSegmentBatchUploader); ok {
			if err := batch.UploadSegments(ctx, vol.CloudVolumeId, vol.DiskPath, paths, retire); err != nil {
				w.log.Warn("failed to upload segments, pausing this volume's scan", "path", paths[0], "error", err)
				return nil, false, err
			}
			break
		}
		path := paths[0]
		if w.uploader != nil {
			if _, err := w.uploader.UploadSegment(ctx, vol.CloudVolumeId, path); err != nil {
				w.log.Warn("failed to upload segment, pausing this volume's scan", "path", path, "error", err)
				return nil, false, err
			}
		}
		if err := retire(path); err != nil {
			w.log.Warn("failed to update log horizon, pausing this volume's scan", "path", path, "error", err)
			return nil, false, err
		}
		paths = paths[1:]
	}
	// Even after the cached scan window is exhausted, immediately check for
	// more work. Only an empty scan or a failure should wait for discovery ticks.
	return remaining, true, nil
}
