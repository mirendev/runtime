package exec

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"miren.dev/runtime/controllers/sandbox"
)

type sinkFunc func([]byte) (int, error)

func (f sinkFunc) Write(p []byte) (int, error) { return f(p) }

func TestFinishAttachWaitsForQueuedOutput(t *testing.T) {
	h := sandbox.NewHub()

	release := make(chan struct{})
	delivered := make(chan struct{}, 8)
	sub := h.Subscribe(sinkFunc(func(p []byte) (int, error) {
		<-release
		delivered <- struct{}{}
		return len(p), nil
	}))
	defer sub.Stop()

	_, _ = h.Write([]byte("first"))
	_, _ = h.Write([]byte("second"))
	h.Close()

	result := make(chan error, 1)
	go func() { result <- finishAttach(context.Background(), sub) }()

	select {
	case <-result:
		t.Fatal("finishAttach returned while output was still queued for the client")
	case <-time.After(50 * time.Millisecond):
	}

	close(release)
	require.NoError(t, <-result)
	assert.Len(t, delivered, 2, "both chunks reached the client before the attach ended")
}

func TestFinishAttachReportsDroppedOutput(t *testing.T) {
	h := sandbox.NewHub()

	release := make(chan struct{})
	sub := h.Subscribe(sinkFunc(func(p []byte) (int, error) {
		<-release
		return len(p), nil
	}))
	defer sub.Stop()

	// Far past the queue depth, so the stalled client loses some of it.
	for range 1024 {
		_, _ = h.Write([]byte("x"))
	}
	h.Close()
	close(release)

	err := finishAttach(context.Background(), sub)
	var truncated OutputTruncatedError
	require.ErrorAs(t, err, &truncated)
	assert.Contains(t, err.Error(), "output truncated")
	assert.Contains(t, err.Error(), "bytes of output never reached the client")
}

func TestFinishAttachClientGoneIsNotAnError(t *testing.T) {
	h := sandbox.NewHub()

	blocked := make(chan struct{})
	defer close(blocked)
	sub := h.Subscribe(sinkFunc(func(p []byte) (int, error) {
		<-blocked
		return len(p), nil
	}))
	defer sub.Stop()

	_, _ = h.Write([]byte("never delivered"))
	h.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	assert.NoError(t, finishAttach(ctx, sub))
}

// A run that finished, and wrote more than the replay buffer holds, before the
// client attached: the client gets only the tail, and the attach must say so.
func TestFinishAttachReportsOutputMissingFromTheReplay(t *testing.T) {
	h := sandbox.NewHub()
	_, _ = h.Write(make([]byte, 128<<10))
	h.Close()

	sub := h.Subscribe(sinkFunc(func(p []byte) (int, error) { return len(p), nil }))
	defer sub.Stop()

	err := finishAttach(context.Background(), sub)
	var truncated OutputTruncatedError
	require.ErrorAs(t, err, &truncated)
}
