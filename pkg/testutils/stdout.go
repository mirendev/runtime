package testutils

import (
	"bufio"
	"io"
	"os"
	"strings"
	"testing"
)

func CaptureStdout(t *testing.T, logOutput bool, fn func()) string {
	t.Helper()

	originalStdout := os.Stdout

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create pipe: %v", err)
	}

	os.Stdout = w

	// Channel to collect output
	outputChan := make(chan string, 1)
	errorChan := make(chan error, 1)

	// Start goroutine to read output
	go func() {
		var output strings.Builder

		if logOutput {
			// Read line by line to log to t.Log
			scanner := bufio.NewScanner(r)
			for scanner.Scan() {
				line := scanner.Text()
				output.WriteString(line)
				output.WriteString("\n")
				t.Log(line)
			}
			if err := scanner.Err(); err != nil {
				errorChan <- err
				return
			}
		} else {
			// Just capture without logging
			captured, err := io.ReadAll(r)
			if err != nil {
				errorChan <- err
				return
			}
			output.Write(captured)
		}

		outputChan <- output.String()
	}()

	// Execute function
	fn()

	// Close writer and restore stdout
	w.Close()
	os.Stdout = originalStdout

	// Wait for reading to complete
	select {
	case output := <-outputChan:
		return output
	case err := <-errorChan:
		t.Fatalf("failed to read captured output: %v", err)
		return ""
	}
}
