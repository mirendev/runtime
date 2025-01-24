package slogrus

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLogger(t *testing.T) {
	t.Run("redirects logs to slog", func(t *testing.T) {
		var buf bytes.Buffer

		log := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{
			Level: slog.LevelDebug,
		}))

		logger := NewLogger(log)

		// Log some messages
		logger.WithField("key", "value").Debug("Debug message")
		logger.Infof("Info message: %s", "thing")
		logger.Warn("Warning message")
		logger.WithError(fmt.Errorf("bad air")).Error("breathing hurts")

		expected := []map[string]any{
			{"level": "DEBUG", "msg": "Debug message", "key": "value"},
			{"level": "INFO", "msg": "Info message: thing"},
			{"level": "WARN", "msg": "Warning message"},
			{"level": "ERROR", "msg": "breathing hurts", "error": "bad air"},
		}

		r := require.New(t)

		var actual []map[string]any

		dec := json.NewDecoder(bytes.NewReader(buf.Bytes()))

		for {
			var entry map[string]any
			if err := dec.Decode(&entry); err != nil {
				break
			}

			actual = append(actual, entry)
		}

		for _, a := range actual {
			delete(a, "time")
		}

		r.Equal(expected, actual)
	})
}
