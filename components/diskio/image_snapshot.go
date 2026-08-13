package diskio

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"miren.dev/runtime/pkg/snapshot"
)

// ImageSnapshotter uploads a compressed snapshot of a universal-mode volume's
// backing image to the cloud as a loop_image update.
//
// This is deliberately on demand only. Snapshotting a mounted, actively-written
// image means reading a file while its single writer -- the loop device -- keeps
// changing it, so the head and tail of the read come from different moments. The
// result is not crash-consistent; it is a state that never existed on the disk
// at any instant, which a journalling filesystem and Postgres's WAL are both
// entitled to be confused by. Taking that automatically on a timer and filing
// the result as a restore point would be worse than not backing up at all,
// because it looks like protection.
//
// So the decision that it is safe -- the disk is idle, or unmounted, or the
// operator accepts the risk -- belongs to a person, not a ticker. Accelerator
// mode is the answer for continuous backup: lbd captures writes in order inside
// the kernel, so its log segments cannot smear.
type ImageSnapshotter struct {
	log     *slog.Logger
	updates CloudUpdatesClient
}

// NewImageSnapshotter creates a snapshotter over the given updates client.
func NewImageSnapshotter(log *slog.Logger, updates CloudUpdatesClient) *ImageSnapshotter {
	return &ImageSnapshotter{
		log:     log.With("module", "image-snapshot"),
		updates: updates,
	}
}

// SnapshotRequest describes one image to snapshot and upload.
type SnapshotRequest struct {
	// VolumeID is the cloud volume the update belongs to.
	VolumeID string
	// ImagePath is the backing file to snapshot.
	ImagePath string
	// Name and Filesystem are recorded in the snapshot header so a restore
	// knows what it is rebuilding.
	Name       string
	Filesystem string
	// SnapshotName optionally pins this as a named restore point.
	SnapshotName string
	// LeaseNonce is required when the volume has an active lease.
	LeaseNonce string
	// StagingDir is where the compressed snapshot is written before upload.
	// Defaults to the image's own directory.
	StagingDir string
}

// Snapshot compresses the image and uploads it, returning the cloud's update ID.
func (s *ImageSnapshotter) Snapshot(ctx context.Context, req SnapshotRequest) (string, error) {
	if s.updates == nil {
		return "", fmt.Errorf("no cloud updates client configured")
	}
	if req.VolumeID == "" {
		return "", fmt.Errorf("volume ID is required")
	}

	info, err := os.Stat(req.ImagePath)
	if err != nil {
		return "", fmt.Errorf("stat image: %w", err)
	}

	staged, checksum, err := s.stage(req, info)
	if err != nil {
		return "", err
	}
	defer os.Remove(staged.Name())
	defer staged.Close()

	stagedInfo, err := staged.Stat()
	if err != nil {
		return "", fmt.Errorf("stat staged snapshot: %w", err)
	}
	if _, err := staged.Seek(0, io.SeekStart); err != nil {
		return "", fmt.Errorf("rewind staged snapshot: %w", err)
	}

	// The ordering key must sort against its siblings and match the cloud's
	// 16-lowercase-hex rule for this kind. Unix nanoseconds satisfy both and
	// need no persisted counter.
	orderingKey := fmt.Sprintf("%016x", time.Now().UnixNano())

	updateID, err := s.updates.Upload(ctx, req.VolumeID, UploadRequest{
		Kind:         KindLoopImage,
		OrderingKey:  orderingKey,
		SnapshotName: req.SnapshotName,
		Metadata: map[string]any{
			"compression":     "zstd",
			"format":          "miren-snapshot",
			"filesystem":      req.Filesystem,
			"image_size":      info.Size(),
			"image_sha256":    checksum,
			"compressed_size": stagedInfo.Size(),
		},
		LeaseNonce: req.LeaseNonce,
	}, staged, stagedInfo.Size())
	if err != nil {
		return "", fmt.Errorf("upload image snapshot: %w", err)
	}

	s.log.Info("uploaded volume image snapshot",
		"volume_id", req.VolumeID,
		"update_id", updateID,
		"ordering_key", orderingKey,
		"image_size", info.Size(),
		"compressed_size", stagedInfo.Size(),
	)
	return updateID, nil
}

// stage writes a compressed snapshot to a temp file.
//
// snapshot.Backup needs an io.WriteSeeker because it rewrites the header with
// the checksum after streaming, so the snapshot cannot go straight into the
// upload body.
func (s *ImageSnapshotter) stage(req SnapshotRequest, info os.FileInfo) (*os.File, string, error) {
	img, err := os.Open(req.ImagePath)
	if err != nil {
		return nil, "", fmt.Errorf("open image: %w", err)
	}
	defer img.Close()

	dir := req.StagingDir
	if dir == "" {
		dir = filepath.Dir(req.ImagePath)
	}

	staged, err := os.CreateTemp(dir, ".image-snapshot-*")
	if err != nil {
		return nil, "", fmt.Errorf("create staging file: %w", err)
	}

	name := req.Name
	if name == "" {
		name = req.VolumeID
	}

	checksum, err := snapshot.Backup(staged, img, name, info.Size(), req.Filesystem)
	if err != nil {
		staged.Close()
		os.Remove(staged.Name())
		return nil, "", fmt.Errorf("compress image: %w", err)
	}

	return staged, checksum, nil
}
