package slogfmt

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMultiLineRecordEndsInOneNewline(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(NewTextHandler(&buf, nil))

	log.Error("failed", "trace", "line one\nline two")
	log.Info("next")

	assert.NotContains(t, buf.String(), "\n\n", "no blank line between records")
	assert.Equal(t, 1, strings.Count(buf.String(), "[INFO]"))
}
