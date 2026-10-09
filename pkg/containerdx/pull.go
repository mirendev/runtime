package containerdx

import (
	"context"
	"log/slog"
	"strings"
	"time"

	containerd "github.com/containerd/containerd/v2/client"
)

// PullImage preserves the caller's resolver and unpack options while tolerating
// a registry temporarily losing a blob referenced by a resolved manifest.
func PullImage(ctx context.Context, log *slog.Logger, cc *containerd.Client, ref string, opts ...containerd.RemoteOpt) (containerd.Image, error) {
	return RetryBlobFetch(ctx, log, func() (containerd.Image, error) {
		return cc.Pull(ctx, ref, opts...)
	})
}

// RetryBlobFetch retries only containerd's missing-content error, also propagated
// by BuildKit. A missing manifest/tag, authentication error, or failed build is
// not evidence of this registry failure. Callers must be safe to repeat; pulls
// and image builds reuse content-addressed results from the previous attempt.
func RetryBlobFetch[T any](ctx context.Context, log *slog.Logger, fetch func() (T, error)) (T, error) {
	var zero T
	for attempt := 0; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return zero, err
		}
		result, err := fetch()
		if err == nil || attempt == 2 || !isBlobNotFound(err) {
			return result, err
		}
		if ctx.Err() != nil {
			return zero, ctx.Err()
		}
		delay := time.Second << attempt
		log.Warn("registry blob not found; retrying image operation", "attempt", attempt+2, "delay", delay, "error", err)
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return zero, ctx.Err()
		case <-timer.C:
		}
	}
}

func isBlobNotFound(err error) bool {
	// BuildKit transports this error over RPC, losing the original errdefs
	// type. Match the descriptor error itself, not a generic "not found".
	_, descriptor, ok := strings.Cut(err.Error(), "could not fetch content descriptor ")
	if !ok {
		return false
	}
	_, mediaType, ok := strings.Cut(descriptor, " (")
	if !ok {
		return false
	}
	mediaType, _, ok = strings.Cut(mediaType, ") from remote: not found")
	if !ok {
		return false
	}
	return mediaType == "application/vnd.oci.image.config.v1+json" ||
		strings.HasPrefix(mediaType, "application/vnd.oci.image.layer.v1.tar") ||
		mediaType == "application/vnd.docker.container.image.v1+json" ||
		strings.HasPrefix(mediaType, "application/vnd.docker.image.rootfs.diff.tar")
}
