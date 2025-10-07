package lsvd

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
)

// SegmentReconciler syncs segments from primary to replica
type SegmentReconciler struct {
	log     *slog.Logger
	primary Volume
	replica Volume
}

// NewSegmentReconciler creates a new segment reconciler
func NewSegmentReconciler(log *slog.Logger, primary, replica Volume) *SegmentReconciler {
	return &SegmentReconciler{
		log:     log.With("module", "segment-reconciler"),
		primary: primary,
		replica: replica,
	}
}

// ReconcileResult contains the results of a reconciliation operation
type ReconcileResult struct {
	TotalPrimary   int
	TotalReplica   int
	Missing        int
	Uploaded       int
	Failed         int
	FailedSegments []SegmentId
}

// Reconcile finds segments in primary that are missing from replica and uploads them
func (r *SegmentReconciler) Reconcile(ctx context.Context) (*ReconcileResult, error) {
	result := &ReconcileResult{}

	// List segments from both sources
	primarySegs, err := r.primary.ListSegments(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list primary segments: %w", err)
	}
	result.TotalPrimary = len(primarySegs)

	replicaSegs, err := r.replica.ListSegments(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list replica segments: %w", err)
	}
	result.TotalReplica = len(replicaSegs)

	// Create a set of replica segments for fast lookup
	replicaSet := make(map[SegmentId]bool, len(replicaSegs))
	for _, seg := range replicaSegs {
		replicaSet[seg] = true
	}

	// Find missing segments
	var missingSegments []SegmentId
	for _, seg := range primarySegs {
		if !replicaSet[seg] {
			missingSegments = append(missingSegments, seg)
		}
	}
	result.Missing = len(missingSegments)

	if len(missingSegments) == 0 {
		r.log.Info("reconciliation complete - no missing segments")
		return result, nil
	}

	r.log.Info("starting segment reconciliation",
		"total_primary", result.TotalPrimary,
		"total_replica", result.TotalReplica,
		"missing", result.Missing)

	// Upload missing segments
	for _, seg := range missingSegments {
		if err := r.uploadSegment(ctx, seg); err != nil {
			r.log.Error("failed to upload segment", "segment", seg.String(), "error", err)
			result.Failed++
			result.FailedSegments = append(result.FailedSegments, seg)
		} else {
			r.log.Info("uploaded segment to replica", "segment", seg.String())
			result.Uploaded++
		}
	}

	r.log.Info("reconciliation complete",
		"uploaded", result.Uploaded,
		"failed", result.Failed)

	return result, nil
}

// uploadSegment uploads a single segment from primary to replica
func (r *SegmentReconciler) uploadSegment(ctx context.Context, seg SegmentId) error {
	// Open segment from primary
	reader, err := r.primary.OpenSegment(ctx, seg)
	if err != nil {
		return fmt.Errorf("failed to open primary segment: %w", err)
	}
	defer reader.Close()

	// Get segment layout
	layout, err := reader.Layout(ctx)
	if err != nil {
		return fmt.Errorf("failed to get segment layout: %w", err)
	}

	// Create temp file to hold segment data
	tempFile, err := os.CreateTemp("", "segment-reconcile-*.dat")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	defer os.Remove(tempFile.Name())
	defer tempFile.Close()

	// Copy segment data to temp file using ReadAt
	// Read in chunks until EOF
	buf := make([]byte, 1024*1024) // 1MB chunks
	offset := int64(0)
	for {
		n, err := reader.ReadAt(buf, offset)
		if n > 0 {
			_, writeErr := tempFile.Write(buf[:n])
			if writeErr != nil {
				return fmt.Errorf("failed to write segment data: %w", writeErr)
			}
			offset += int64(n)
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read segment data: %w", err)
		}
	}

	// Seek back to start for upload
	_, err = tempFile.Seek(0, io.SeekStart)
	if err != nil {
		return fmt.Errorf("failed to seek temp file to start: %w", err)
	}

	// Upload to replica
	err = r.replica.NewSegment(ctx, seg, layout, tempFile)
	if err != nil {
		return fmt.Errorf("failed to upload to replica: %w", err)
	}

	return nil
}
