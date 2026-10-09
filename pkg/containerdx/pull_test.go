package containerdx

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"testing/synctest"
	"time"
)

func TestRetryBlobFetch(t *testing.T) {
	blobError := func(mediaType, reason string) error {
		return fmt.Errorf("rpc error: code = Unknown desc = failed to copy: httpReadSeeker: failed open: could not fetch content descriptor sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa (%s) from remote: %s", mediaType, reason)
	}
	for _, tc := range []struct {
		name  string
		err   error
		retry bool
	}{
		{"OCI config", blobError("application/vnd.oci.image.config.v1+json", "not found"), true},
		{"OCI layer", blobError("application/vnd.oci.image.layer.v1.tar+gzip", "not found"), true},
		{"Docker config", blobError("application/vnd.docker.container.image.v1+json", "not found"), true},
		{"Docker layer", blobError("application/vnd.docker.image.rootfs.diff.tar.gzip", "not found"), true},
		{"missing manifest", blobError("application/vnd.oci.image.manifest.v1+json", "not found"), false},
		{"missing tag", errors.New("example.invalid/app:missing: not found"), false},
		{"auth", blobError("application/vnd.oci.image.config.v1+json", "unauthorized"), false},
		{"failed command", errors.New("process exited: file not found"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				failed := false
				got, err := RetryBlobFetch(t.Context(), slog.New(slog.NewTextHandler(io.Discard, nil)), func() (string, error) {
					if !failed {
						failed = true
						return "", tc.err
					}
					return "image-content", nil
				})
				if tc.retry {
					if err != nil || got != "image-content" {
						t.Fatalf("got %q, %v; want recovered content", got, err)
					}
				} else if !errors.Is(err, tc.err) {
					t.Fatalf("got %v; want original error %v", err, tc.err)
				}
			})
		})
	}

	t.Run("permanent blob failure is bounded", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			failure := blobError("application/vnd.oci.image.config.v1+json", "not found")
			attempts := 0
			start := time.Now()
			_, err := RetryBlobFetch(t.Context(), slog.Default(), func() (string, error) {
				attempts++
				return "", failure
			})
			if !errors.Is(err, failure) || attempts != 3 || time.Since(start) != 3*time.Second {
				t.Fatalf("error=%v attempts=%d elapsed=%s; want original error, three attempts and 3s backoff", err, attempts, time.Since(start))
			}
		})
	})

	t.Run("cancellation interrupts backoff", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
			defer cancel()
			start := time.Now()
			_, err := RetryBlobFetch(ctx, slog.Default(), func() (string, error) {
				return "", blobError("application/vnd.oci.image.config.v1+json", "not found")
			})
			if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) != 100*time.Millisecond {
				t.Fatalf("error=%v elapsed=%s; want context deadline at 100ms", err, time.Since(start))
			}
		})
	})
}
