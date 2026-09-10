package commands

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
)

func TestPrintDeployOutcome(t *testing.T) {
	newCtx := func() (*Context, *bytes.Buffer) {
		var out bytes.Buffer
		return &Context{
			Context: context.Background(),
			Log:     slog.New(slog.NewTextHandler(io.Discard, nil)),
			Stdout:  &out,
			Stderr:  io.Discard,
		}, &out
	}

	t.Run("success prints marker and greppable version line", func(t *testing.T) {
		ctx, out := newCtx()
		printDeployOutcome(ctx, true, "app_version/meet-vCXq399vn6s44fkCen5KTu")

		got := out.String()
		if !strings.Contains(got, "✓ Deploy successful\n") {
			t.Fatalf("missing success marker in %q", got)
		}
		// The version is the full ID with the entity prefix stripped, so it can
		// be fed straight back into `miren deploy --version`.
		if !strings.Contains(got, "\nVersion: meet-vCXq399vn6s44fkCen5KTu\n") {
			t.Fatalf("missing Version line in %q", got)
		}
	})

	t.Run("failure prints failure marker", func(t *testing.T) {
		ctx, out := newCtx()
		printDeployOutcome(ctx, false, "meet-v1")

		got := out.String()
		if !strings.Contains(got, "✗ Deploy failed\n") || !strings.Contains(got, "Version: meet-v1\n") {
			t.Fatalf("unexpected failure output %q", got)
		}
	})

	t.Run("no version omits the Version line", func(t *testing.T) {
		ctx, out := newCtx()
		printDeployOutcome(ctx, false, "")
		if strings.Contains(out.String(), "Version:") {
			t.Fatalf("Version line printed with empty version: %q", out.String())
		}
	})
}
