package testutils

import (
	"io"
	"os"
)

func CaptureStdout(fn func()) (string, error) {
	originalStdout := os.Stdout

	r, w, err := os.Pipe()
	if err != nil {
		return "", err
	}

	os.Stdout = w

	// Use defer to ensure cleanup happens
	defer func() {
		w.Close()
		os.Stdout = originalStdout
	}()

	// Execute function
	fn()

	// Close writer before reading
	w.Close()
	os.Stdout = originalStdout

	captured, err := io.ReadAll(r)
	return string(captured), err
}
