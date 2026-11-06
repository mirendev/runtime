package diskio

import (
	"context"
	"errors"
	"fmt"
	"hash/crc32"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"

	"miren.dev/lbd"
	"miren.dev/runtime/pkg/cloudauth"
)

var crc32cTable = crc32.MakeTable(crc32.Castagnoli)

// CloudSegmentUploader implements LogSegmentUploader by uploading lbd log
// segments to miren.cloud as lbd_log volume updates.
//
// A segment's TAI64N label becomes the update's ordering key, which is what
// replay sorts and horizon-compares on.
type CloudSegmentUploader struct {
	log     *slog.Logger
	updates CloudUpdatesClient
	state   *State
}

func NewCloudSegmentUploader(log *slog.Logger, baseURL string, authClient *cloudauth.AuthClient, state *State) *CloudSegmentUploader {
	return NewCloudSegmentUploaderWithClient(
		log,
		NewCloudUpdatesClient(log, baseURL, authClient),
		state,
	)
}

// NewCloudSegmentUploaderWithClient builds an uploader over an existing updates
// client, so callers that already have one need not construct a second.
func NewCloudSegmentUploaderWithClient(log *slog.Logger, updates CloudUpdatesClient, state *State) *CloudSegmentUploader {
	return &CloudSegmentUploader{
		log:     log.With("module", "cloud-uploader"),
		updates: updates,
		state:   state,
	}
}

// UploadSegment uploads a log segment file to the cloud and returns the cloud segment ID.
func (u *CloudSegmentUploader) UploadSegment(ctx context.Context, volumeID, segmentPath string) (string, error) {
	var id string
	err := u.withCloudUpload(ctx, volumeID, nil, func() error {
		var err error
		id, err = u.uploadSegment(ctx, volumeID, segmentPath)
		return err
	}, nil)
	return id, err
}

const (
	uploadConcurrency = 8
	uploadWindow      = 32
	uploadWindowBytes = 64 << 20
)

// uploadTurnSize bounds retained preparation as well as active transfers. An
// oversized oldest segment runs alone rather than wedging the ordered stream.
func uploadTurnSize(paths []string) (int, error) {
	var bytes int64
	for i, path := range paths[:min(uploadWindow, len(paths))] {
		info, err := os.Stat(path)
		if err != nil {
			return 0, err
		}
		if i > 0 && bytes+info.Size() > uploadWindowBytes {
			return i, nil
		}
		bytes += info.Size()
	}
	return min(uploadWindow, len(paths)), nil
}

// UploadSegments overlaps bounded preparation with ordered publication.
// No local files are retired until the entire batch is authenticated remotely.
// Thus a crash or partial publication leaves the original batch available to
// resume by checksum-bound ordering key, including a lost begin response.
func (u *CloudSegmentUploader) UploadSegments(ctx context.Context, volumeID, diskPath string, paths []string, committed func(string) error) error {
	if u.state != nil {
		unlock, err := u.state.LockCloudVolume(ctx, volumeID)
		if err != nil {
			return err
		}
		defer unlock()
	}
	return u.uploadSegmentsLocked(ctx, volumeID, diskPath, paths, committed)
}

func (u *CloudSegmentUploader) uploadSegmentsLocked(ctx context.Context, volumeID, diskPath string, paths []string, committed func(string) error) error {
	if len(paths) == 0 || len(paths) > uploadWindow {
		return fmt.Errorf("upload batch must contain 1..%d segments", uploadWindow)
	}
	var previous string
	for _, path := range paths {
		label := lbd.LabelFromLogPath(path)
		if !isValidTAI64NLabel(label) || label <= previous {
			return fmt.Errorf("upload batch must have strictly increasing TAI64N labels")
		}
		previous = label
	}
	selected := paths
	return u.withCloudUploadLocked(volumeID, func() (bool, error) {
		// Mount replay may have advanced the horizon while this batch waited
		// for the volume lock. Never publish writes now covered by replay.
		horizon, err := readLogHorizon(diskPath)
		if err != nil {
			return false, err
		}
		if horizon != "" {
			if err := syncLogHorizon(diskPath); err != nil {
				return false, err
			}
		}
		for len(paths) > 0 && lbd.LabelFromLogPath(paths[0]) <= horizon {
			paths = paths[1:]
		}
		n, err := uploadTurnSize(paths)
		if err != nil {
			return false, err
		}
		if n != len(paths) {
			return false, fmt.Errorf("upload batch exceeds the preparation byte budget")
		}
		return len(paths) > 0, nil
	}, func() error {
		staged, ok := u.updates.(stagedUpdatesClient)
		if !ok {
			for _, path := range paths {
				if _, err := u.uploadSegment(ctx, volumeID, path); err != nil {
					return err
				}
			}
			return nil
		}
		uploadCtx := ctx
		ctx, cancel := context.WithCancel(ctx)
		var wg sync.WaitGroup
		defer func() {
			cancel()
			wg.Wait()
		}()
		type result struct {
			prepared *preparedUpload
			err      error
		}
		ready := make([]chan result, len(paths))
		jobs := make(chan int, len(paths))
		for i := range paths {
			ready[i] = make(chan result, 1)
			jobs <- i
		}
		close(jobs)
		var failed atomic.Bool
		nonce := u.leaseNonceFor(volumeID)
		for range min(uploadConcurrency, len(paths)) {
			wg.Go(func() {
				for {
					if failed.Load() || ctx.Err() != nil {
						return
					}
					i, ok := <-jobs
					if !ok {
						return
					}
					p, err := func() (*preparedUpload, error) {
						f, err := os.Open(paths[i])
						if err != nil {
							return nil, err
						}
						defer f.Close()
						info, err := f.Stat()
						if err != nil || info.Size() == 0 {
							return nil, err
						}
						return staged.PrepareUpload(ctx, volumeID, UploadRequest{
							Kind: KindLBDLog, OrderingKey: lbd.LabelFromLogPath(paths[i]), LeaseNonce: nonce,
						}, f, info.Size())
					}()
					ready[i] <- result{p, err}
					if err != nil {
						failed.Store(true)
						return
					}
				}
			})
		}
		var head *publicationBoundary
		headKnown := false
		publisher, batchSupported := u.updates.(prefixUpdatesClient)
		for i := 0; i < len(paths); {
			var r result
			select {
			case <-ctx.Done():
				return ctx.Err()
			case r = <-ready[i]:
			}
			if r.err != nil {
				if errors.Is(r.err, errPreparationUnsupported) {
					// Join every preparation before any legacy completion can
					// publish. Old replicas must never interpret preparation as activation.
					cancel()
					wg.Wait()
					for _, path := range paths[i:] {
						if _, err := u.uploadSegment(uploadCtx, volumeID, path); err != nil {
							return err
						}
					}
					return nil
				}
				return fmt.Errorf("preparing %s: %w", paths[i], r.err)
			}
			p := r.prepared
			if p == nil {
				i++
				continue
			}
			prefix := []*preparedUpload{p}
			if batchSupported && p.batchLimit > 0 && !p.active {
				if !headKnown {
					updates, err := u.updates.List(ctx, volumeID, ListOptions{Kind: KindLBDLog, Descending: true, Limit: 1})
					if err != nil {
						return err
					}
					if len(updates) > 0 {
						head = &publicationBoundary{updates[0].UpdateID, updates[0].OrderingKey}
					}
					headKnown = true
				}
			collect:
				for len(prefix) < p.batchLimit && i+len(prefix) < len(paths) {
					index := i + len(prefix)
					select {
					case next := <-ready[index]:
						if next.err != nil || next.prepared == nil || next.prepared.active || next.prepared.batchLimit < len(prefix)+1 {
							ready[index] <- next
							break collect
						}
						prefix = append(prefix, next.prepared)
					default:
						break collect
					}
				}
				var err error
				head, err = publisher.PublishPrepared(ctx, head, prefix)
				if err == nil {
					i += len(prefix)
					continue
				}
				if ctx.Err() != nil {
					return fmt.Errorf("publishing %s: %w", paths[i], err)
				}
				// Batch publication is only an accelerator. Finish this turn
				// through ordered single completion, which every server version
				// accepts. It is safe even if this batch committed or is still
				// running: both paths activate only these exact prepared
				// identities oldest-first, so whatever lands is still a prefix,
				// and completing an already active entry just authenticates it.
				u.log.Warn("batch publication failed, completing this turn one segment at a time",
					"volume_id", volumeID, "count", len(prefix), "error", err)
				batchSupported = false
			}
			for _, entry := range prefix {
				if _, err := staged.CompleteUpload(ctx, entry); err != nil {
					return fmt.Errorf("publishing %s: %w", paths[i], err)
				}
				head = &publicationBoundary{entry.updateID, entry.orderingKey}
				i++
			}
		}
		return nil
	}, func() error {
		for _, path := range selected {
			if err := committed(path); err != nil {
				return err
			}
		}
		return nil
	})
}

func (u *CloudSegmentUploader) withCloudUpload(ctx context.Context, volumeID string, preflight func() (bool, error), upload func() error, retire func() error) error {
	if u.state != nil {
		unlock, err := u.state.LockCloudVolume(ctx, volumeID)
		if err != nil {
			return err
		}
		defer unlock()
	}
	return u.withCloudUploadLocked(volumeID, preflight, upload, retire)
}

func (u *CloudSegmentUploader) withCloudUploadLocked(volumeID string, preflight func() (bool, error), upload func() error, retire func() error) error {
	hasWork := true
	if preflight != nil {
		var err error
		hasWork, err = preflight()
		if err != nil {
			return err
		}
	}
	// A replay-covered batch must neither create a stranded intent nor clear
	// an earlier intent whose remote work has not been reconciled.
	if hasWork {
		if u.state != nil {
			// Transient failures resume the original claims on the next scan.
			// Persistent conflicts or lease rejection require reconciliation;
			// clearing this gate would permit a transition with unresolved writes.
			if err := u.state.SetPendingCloudUpload(volumeID, "uploading"); err != nil {
				return err
			}
		}
		if err := upload(); err != nil {
			return err
		}
		// Clear before retirement: after a crash here all files still exist and
		// active checksum-bound claims reconcile them. Clearing after deletion
		// could strand the transition gate with no segment left to reconcile.
		if u.state != nil {
			if err := u.state.SetPendingCloudUpload(volumeID, ""); err != nil {
				return err
			}
		}
	}
	if retire != nil {
		return retire()
	}
	return nil
}

// drainSegmentsLocked runs after detach has closed the final segment, while
// the caller retains the lease and cloud-volume lock through publication.
func (u *CloudSegmentUploader) drainSegmentsLocked(ctx context.Context, volumeID, diskPath string) error {
	if _, err := os.Stat(diskPath); err != nil {
		return fmt.Errorf("inspect log backing directory: %w", err)
	}
	for {
		names, err := oldestSegments(ctx, filepath.Join(diskPath, "logs"))
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if len(names) == 0 {
			return nil
		}
		if u.updates == nil {
			return fmt.Errorf("cannot drain cloud logs without an updates client")
		}
		for len(names) > 0 {
			n := min(uploadWindow, len(names))
			paths := make([]string, n)
			for i, name := range names[:n] {
				paths[i] = filepath.Join(diskPath, "logs", name)
			}
			n, err = uploadTurnSize(paths)
			if err != nil {
				return err
			}
			if err := u.uploadSegmentsLocked(ctx, volumeID, diskPath, paths[:n], func(path string) error {
				if err := updateLogHorizonFromPath(diskPath, path); err != nil {
					return err
				}
				if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
					return err
				}
				return nil
			}); err != nil {
				return err
			}
			names = names[n:]
		}
	}
}

func (u *CloudSegmentUploader) uploadSegment(ctx context.Context, volumeID, segmentPath string) (string, error) {
	f, err := os.Open(segmentPath)
	if err != nil {
		return "", fmt.Errorf("failed to open segment file: %w", err)
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		return "", fmt.Errorf("failed to stat segment file: %w", err)
	}
	size := stat.Size()
	if size == 0 {
		return "", nil // skip empty files
	}

	// The label is the segment's ordering key. The cloud cannot reconstruct it
	// from the payload, so a segment without one cannot be ordered and is
	// refused here rather than as a 400.
	label := lbd.LabelFromLogPath(segmentPath)
	if !isValidTAI64NLabel(label) {
		return "", fmt.Errorf("segment %q has no usable TAI64N label", segmentPath)
	}

	return u.updates.Upload(ctx, volumeID, UploadRequest{
		Kind:        KindLBDLog,
		OrderingKey: label,
		LeaseNonce:  u.leaseNonceFor(volumeID),
	}, f, size)
}

// leaseNonceFor finds the nonce held for a cloud volume in mount state, if any.
// The cloud requires it on writes to a leased volume. cloudVolumeID is the
// volume's miren.cloud id; mount state keys off the local entity id, so the
// lookup is delegated to State, which bridges the two.
func (u *CloudSegmentUploader) leaseNonceFor(cloudVolumeID string) string {
	if u.state == nil {
		return ""
	}
	return u.state.LeaseNonceForCloudVolume(cloudVolumeID)
}
