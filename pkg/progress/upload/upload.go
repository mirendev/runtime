package upload

import (
	"fmt"
	"io"
	"sync"
	"time"
)

// ProgressReader wraps an io.Reader and periodically reports upload progress.
type ProgressReader struct {
	reader     io.ReadCloser
	readBytes  int64
	mu         sync.RWMutex
	startTime  time.Time
	lastUpdate time.Time
	onProgress func(Progress)
}

// Progress represents a snapshot of the current upload stats.
type Progress struct {
	BytesRead      int64
	BytesPerSecond float64
	Duration       time.Duration
}

// NewProgressReader creates a new progress tracking reader that wraps the given io.Reader.
func NewProgressReader(r io.ReadCloser, onProgress func(Progress)) *ProgressReader {
	return &ProgressReader{
		reader:     r,
		startTime:  time.Now(),
		lastUpdate: time.Now(),
		onProgress: onProgress,
	}
}

func (pr *ProgressReader) Close() error {
	return pr.reader.Close()
}

func (pr *ProgressReader) Read(p []byte) (int, error) {
	n, err := pr.reader.Read(p)
	if n > 0 {
		pr.mu.Lock()
		pr.readBytes += int64(n)
		now := time.Now()

		if now.Sub(pr.lastUpdate) >= 100*time.Millisecond || err == io.EOF {
			pr.lastUpdate = now
			progress := pr.calculateProgress()
			pr.mu.Unlock()

			if pr.onProgress != nil {
				pr.onProgress(progress)
			}
		} else {
			pr.mu.Unlock()
		}
	} else if err == io.EOF && pr.onProgress != nil {
		// Emit a final update on EOF even when no bytes were read
		pr.mu.RLock()
		progress := pr.calculateProgress()
		pr.mu.RUnlock()
		pr.onProgress(progress)
	}
	return n, err
}

func (pr *ProgressReader) calculateProgress() Progress {
	elapsed := time.Since(pr.startTime)
	elapsedSec := elapsed.Seconds()
	if elapsedSec == 0 {
		elapsedSec = 0.001
	}

	bytesPerSecond := float64(pr.readBytes) / elapsedSec

	return Progress{
		BytesRead:      pr.readBytes,
		BytesPerSecond: bytesPerSecond,
		Duration:       elapsed,
	}
}

// GetProgress returns the current progress snapshot.
func (pr *ProgressReader) GetProgress() Progress {
	pr.mu.RLock()
	defer pr.mu.RUnlock()
	return pr.calculateProgress()
}

// FormatBytes formats a byte count as a human-readable string (e.g., "1.5 MB").
func FormatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

// FormatSpeed formats a speed as a human-readable string (e.g., "1.5 MB/s").
func FormatSpeed(bytesPerSecond float64) string {
	return fmt.Sprintf("%s/s", FormatBytes(int64(bytesPerSecond+0.5))) // Round instead of truncate
}

// FormatDuration formats a duration as a human-readable string (e.g., "1m 30s").
func FormatDuration(d time.Duration) string {
	if d < time.Second {
		return "< 1s"
	}
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		minutes := int(d.Minutes())
		seconds := int(d.Seconds()) % 60
		if seconds > 0 {
			return fmt.Sprintf("%dm %ds", minutes, seconds)
		}
		return fmt.Sprintf("%dm", minutes)
	}
	hours := int(d.Hours())
	minutes := int(d.Minutes()) % 60
	if minutes > 0 {
		return fmt.Sprintf("%dh %dm", hours, minutes)
	}
	return fmt.Sprintf("%dh", hours)
}
