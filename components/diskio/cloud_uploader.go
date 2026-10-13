package diskio

import (
	"context"
	"fmt"
	"hash/crc32"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

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

const uploadConcurrency = 8

// UploadSegments prepares a bounded batch concurrently, then publishes in order.
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
	if len(paths) == 0 || len(paths) > uploadConcurrency {
		return fmt.Errorf("upload batch must contain 1..%d segments", uploadConcurrency)
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
		prepared := make([]*preparedUpload, len(paths))
		errors := make([]error, len(paths))
		var wg sync.WaitGroup
		for i, path := range paths {
			wg.Go(func() {
				f, err := os.Open(path)
				if err != nil {
					errors[i] = err
					return
				}
				defer f.Close()
				info, err := f.Stat()
				if err != nil {
					errors[i] = err
					return
				}
				if info.Size() == 0 {
					return
				}
				prepared[i], errors[i] = staged.PrepareUpload(ctx, volumeID, UploadRequest{
					Kind: KindLBDLog, OrderingKey: lbd.LabelFromLogPath(path), LeaseNonce: u.leaseNonceFor(volumeID),
				}, f, info.Size())
			})
		}
		wg.Wait()
		for i, p := range prepared {
			if errors[i] != nil {
				return fmt.Errorf("preparing %s: %w", paths[i], errors[i])
			}
			if p != nil {
				if _, err := staged.CompleteUpload(ctx, p); err != nil {
					return fmt.Errorf("publishing %s: %w", paths[i], err)
				}
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
			n := min(uploadConcurrency, len(names))
			paths := make([]string, n)
			for i, name := range names[:n] {
				paths[i] = filepath.Join(diskPath, "logs", name)
			}
			if err := u.uploadSegmentsLocked(ctx, volumeID, diskPath, paths, func(path string) error {
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
