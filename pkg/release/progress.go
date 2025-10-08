package release

import (
	"fmt"
	"io"
	"time"
)

// ProgressWriter writes download progress to an output stream
type ProgressWriter struct {
	writer      io.Writer
	total       int64
	current     int64
	lastUpdate  time.Time
	updateEvery time.Duration
}

// NewProgressWriter creates a new progress writer
func NewProgressWriter(w io.Writer) *ProgressWriter {
	return &ProgressWriter{
		writer:      w,
		updateEvery: 100 * time.Millisecond,
	}
}

// SetTotal sets the total expected bytes
func (p *ProgressWriter) SetTotal(total int64) {
	p.total = total
}

// Write implements io.Writer and tracks progress
func (p *ProgressWriter) Write(data []byte) (int, error) {
	n := len(data)
	p.current += int64(n)

	// Only update display periodically to avoid flooding output
	now := time.Now()
	if now.Sub(p.lastUpdate) >= p.updateEvery {
		p.updateDisplay()
		p.lastUpdate = now
	}

	return n, nil
}

// Close prints final status
func (p *ProgressWriter) Close() error {
	p.updateDisplay()
	fmt.Fprintln(p.writer) // Final newline
	return nil
}

// updateDisplay updates the progress display
func (p *ProgressWriter) updateDisplay() {
	if p.total > 0 {
		percent := int(100 * p.current / p.total)
		mb := float64(p.current) / (1024 * 1024)
		totalMB := float64(p.total) / (1024 * 1024)
		fmt.Fprintf(p.writer, "\rDownloading: %3d%% (%.1f/%.1f MB)", percent, mb, totalMB)
	} else {
		mb := float64(p.current) / (1024 * 1024)
		fmt.Fprintf(p.writer, "\rDownloading: %.1f MB", mb)
	}
}
