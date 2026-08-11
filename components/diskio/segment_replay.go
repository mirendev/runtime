package diskio

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"miren.dev/lbd"
)

const logHorizonFile = "log_horizon"

// replayMissingSegments downloads and replays any log segments from the cloud
// that have not yet been applied to the local disk image.
//
// The horizon is read first and passed to the server, which returns only the
// segments past it. Asking for the whole history and discarding most of it
// worked, but grew with the volume's age rather than with what is actually
// missing.
func (c *DiskMountController) replayMissingSegments(ctx context.Context, volState *VolumeState) error {
	horizon, err := readLogHorizon(volState.DiskPath)
	if err != nil {
		return fmt.Errorf("reading log horizon: %w", err)
	}

	missing, err := c.cloudClient.ListLogSegments(ctx, volState.VolumeId, horizon)
	if err != nil {
		return fmt.Errorf("listing remote log segments: %w", err)
	}

	// Sort by label to ensure chronological replay order. The server returns
	// them ordered already; this keeps replay correct regardless.
	sort.Slice(missing, func(i, j int) bool { return missing[i].Label < missing[j].Label })

	if len(missing) == 0 {
		c.log.Info("all segments already applied", "volume_id", volState.VolumeId, "horizon", horizon)
		return nil
	}

	c.log.Info("replaying missing log segments",
		"volume_id", volState.VolumeId,
		"horizon", horizon,
		"to_replay", len(missing),
	)

	imagePath := filepath.Join(volState.DiskPath, "disk.img")
	img, err := openDiskImage(imagePath)
	if err != nil {
		return fmt.Errorf("opening disk image: %w", err)
	}
	defer img.Close()

	var lastLabel string
	for _, seg := range missing {
		if err := c.replayOneSegment(ctx, volState.VolumeId, seg.SegmentID, img); err != nil {
			return fmt.Errorf("replaying segment %s (label %s): %w", seg.SegmentID, seg.Label, err)
		}

		lastLabel = seg.Label
		c.log.Info("replayed log segment", "segment_id", seg.SegmentID, "label", seg.Label, "volume_id", volState.VolumeId)
	}

	if err := img.Sync(); err != nil {
		return fmt.Errorf("syncing disk image: %w", err)
	}

	// Update horizon to the last replayed label
	if lastLabel != "" {
		if err := writeLogHorizon(volState.DiskPath, lastLabel); err != nil {
			return fmt.Errorf("updating log horizon: %w", err)
		}
	}

	c.log.Info("segment replay complete",
		"volume_id", volState.VolumeId,
		"segments_replayed", len(missing),
		"new_horizon", lastLabel,
	)
	return nil
}

// diskImage abstracts raw and qcow2 disk image formats for replay.
type diskImage interface {
	WriteAt(b []byte, off int64) (int, error)
	Trim(offset int64, length int) error
	Sync() error
	Close() error
}

// openDiskImage opens a disk image file, auto-detecting qcow2 vs raw format.
func openDiskImage(path string) (diskImage, error) {
	f, err := os.OpenFile(path, os.O_RDWR, 0666)
	if err != nil {
		return nil, err
	}

	var magic [8]byte
	if _, err := f.ReadAt(magic[:], 0); err != nil {
		f.Close()
		return nil, fmt.Errorf("reading magic bytes: %w", err)
	}

	m := binary.BigEndian.Uint64(magic[:])
	if m == lbd.QCow2Magic {
		f.Close()
		img, err := lbd.OpenQCow2(path)
		if err != nil {
			return nil, fmt.Errorf("opening qcow2 image: %w", err)
		}
		return &qcow2DiskImage{img: img}, nil
	}

	return &rawDiskImage{f: f}, nil
}

// rawDiskImage wraps a raw disk image file.
type rawDiskImage struct {
	f *os.File
}

func (r *rawDiskImage) WriteAt(b []byte, off int64) (int, error) {
	return r.f.WriteAt(b, off)
}

func (r *rawDiskImage) Trim(offset int64, length int) error {
	return punchHole(r.f, offset, int64(length))
}

func (r *rawDiskImage) Sync() error {
	return r.f.Sync()
}

func (r *rawDiskImage) Close() error {
	return r.f.Close()
}

// qcow2DiskImage wraps an lbd.QCow2Image.
type qcow2DiskImage struct {
	img *lbd.QCow2Image
}

func (q *qcow2DiskImage) WriteAt(b []byte, off int64) (int, error) {
	return q.img.WriteAt(b, off)
}

func (q *qcow2DiskImage) Trim(offset int64, length int) error {
	return q.img.Trim(offset, length)
}

func (q *qcow2DiskImage) Sync() error {
	return q.img.Flush()
}

func (q *qcow2DiskImage) Close() error {
	return q.img.Close()
}

func (c *DiskMountController) replayOneSegment(ctx context.Context, volumeID, segID string, img diskImage) error {
	rc, err := c.cloudClient.DownloadLogSegment(ctx, volumeID, segID)
	if err != nil {
		return fmt.Errorf("downloading segment: %w", err)
	}
	defer rc.Close()

	rd, err := lbd.NewReader(rc)
	if err != nil {
		return fmt.Errorf("reading segment header: %w", err)
	}

	blockSize := int64(rd.Header.BlockSize)

	for {
		entry, err := rd.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("reading entry: %w", err)
		}

		offset := int64(entry.Block) * blockSize

		if entry.IsWrite() {
			if _, err := img.WriteAt(entry.Data, offset); err != nil {
				return fmt.Errorf("writing at offset %d: %w", offset, err)
			}
		} else if entry.IsTrim() {
			if err := img.Trim(offset, int(entry.Length)); err != nil {
				return fmt.Errorf("trimming at offset %d: %w", offset, err)
			}
		}
	}

	return nil
}

func readLogHorizon(diskPath string) (string, error) {
	path := filepath.Join(diskPath, logHorizonFile)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

func writeLogHorizon(diskPath, label string) error {
	path := filepath.Join(diskPath, logHorizonFile)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(label+"\n"), 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// isValidTAI64NLabel checks if a string is a valid TAI64N label (24 hex chars).
func isValidTAI64NLabel(s string) bool {
	if len(s) != 24 {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}

// updateLogHorizonFromPath extracts the TAI64N label from a log file path
// and updates the horizon if the label is newer than the current one.
func updateLogHorizonFromPath(diskPath, logPath string) error {
	label := lbd.LabelFromLogPath(logPath)
	if !isValidTAI64NLabel(label) {
		return nil
	}

	current, err := readLogHorizon(diskPath)
	if err != nil {
		return err
	}

	if label > current {
		return writeLogHorizon(diskPath, label)
	}
	return nil
}
