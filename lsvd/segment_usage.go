package lsvd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// SegmentUsageInfo contains usage information for a segment
type SegmentUsageInfo struct {
	SegmentId    string    `json:"segment_id"`
	Timestamp    time.Time `json:"timestamp"`
	ExtentCount  int       `json:"extent_count"`
	TotalBlocks  uint64    `json:"total_blocks"`
	UsedBlocks   uint64    `json:"used_blocks"`
	DensityPct   float64   `json:"density_pct"`
}

// writeSegmentUsage writes usage information for a segment to the cache directory
func (d *Disk) writeSegmentUsage(segId SegmentId) error {
	// Get segment stats
	totalBlocks, usedBlocks, extentCount := d.s.SegmentInfo(segId)

	var densityPct float64
	if totalBlocks > 0 {
		densityPct = 100.0 * (float64(usedBlocks) / float64(totalBlocks))
	}

	info := SegmentUsageInfo{
		SegmentId:    segId.String(),
		Timestamp:    time.Now(),
		ExtentCount:  extentCount,
		TotalBlocks:  totalBlocks,
		UsedBlocks:   usedBlocks,
		DensityPct:   densityPct,
	}

	// Write to cache directory
	usageDir := filepath.Join(d.path, "segment-usage")
	if err := os.MkdirAll(usageDir, 0755); err != nil {
		return fmt.Errorf("failed to create segment-usage directory: %w", err)
	}

	filename := filepath.Join(usageDir, fmt.Sprintf("%s.json", segId.String()))
	f, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("failed to create usage file: %w", err)
	}
	defer f.Close()

	encoder := json.NewEncoder(f)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(&info); err != nil {
		return fmt.Errorf("failed to encode usage info: %w", err)
	}

	d.log.Debug("wrote segment usage info",
		"segment", segId,
		"extents", extentCount,
		"total_blocks", totalBlocks,
		"used_blocks", usedBlocks,
		"density_pct", densityPct)

	return nil
}
