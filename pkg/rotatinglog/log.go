package rotatinglog

import (
	"fmt"
	"io"
	"os"
	"sync"
)

// rotatingLogger handles log rotation
type rotatingLogger struct {
	path       string
	maxSize    int64
	maxFiles   int
	currentLog *os.File
	size       int64
	mu         sync.Mutex
}

// newRotatingLogger creates a new rotating logger
func Open(path string, maxSizeMB, maxFiles int) (io.WriteCloser, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, err
	}

	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, err
	}

	return &rotatingLogger{
		path:       path,
		maxSize:    int64(maxSizeMB) * 1024 * 1024,
		maxFiles:   maxFiles,
		currentLog: file,
		size:       info.Size(),
	}, nil
}

// Write implements io.Writer
func (r *rotatingLogger) Write(p []byte) (n int, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Check if we need to rotate
	if r.size+int64(len(p)) > r.maxSize {
		if err := r.rotate(); err != nil {
			return 0, err
		}
	}

	// Write to the current log file
	n, err = r.currentLog.Write(p)
	r.size += int64(n)
	return n, err
}

// rotate rotates the log files
func (r *rotatingLogger) rotate() error {
	// Close current log file
	if err := r.currentLog.Close(); err != nil {
		return err
	}

	// Rotate existing log files
	for i := r.maxFiles - 1; i > 0; i-- {
		oldPath := fmt.Sprintf("%s.%d", r.path, i)
		newPath := fmt.Sprintf("%s.%d", r.path, i+1)

		// Remove the oldest log file if it exists
		if i == r.maxFiles-1 {
			os.Remove(newPath)
		}

		// Rename existing log files
		if err := os.Rename(oldPath, newPath); err != nil {
			return err
		}
	}

	// Rename current log file
	if err := os.Rename(r.path, fmt.Sprintf("%s.1", r.path)); err != nil {
		return err
	}
	// Create new log file
	file, err := os.OpenFile(r.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return err
	}

	r.currentLog = file
	r.size = 0

	return nil
}

// Close closes the logger
func (r *rotatingLogger) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.currentLog != nil {
		return r.currentLog.Close()
	}

	return nil
}
