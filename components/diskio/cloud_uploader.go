package diskio

import (
	"context"
	"fmt"
	"hash/crc32"
	"log/slog"
	"os"

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

// leaseNonceFor finds the nonce held for a volume in mount state, if any. The
// cloud requires it on writes to a leased volume.
func (u *CloudSegmentUploader) leaseNonceFor(volumeID string) string {
	if u.state == nil {
		return ""
	}
	for _, m := range u.state.ListMounts() {
		if m.VolumeId == volumeID && m.LeaseNonce != "" {
			return m.LeaseNonce
		}
	}
	return ""
}
