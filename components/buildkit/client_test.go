package buildkit

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
)

// startInProgress marks a Start as under way, the way Start does on entry, and
// returns a func that finishes it without the daemon coming up.
func startInProgress(c *Component) (finish func()) {
	done := make(chan struct{})
	c.starting.Store(&done)
	return func() { close(done) }
}

func TestClientWaitsForStartInProgress(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := NewComponent(slog.New(slog.NewTextHandler(io.Discard, nil)), nil, "", t.TempDir())
		finish := startInProgress(c)
		go func() {
			time.Sleep(10 * time.Second)
			finish()
		}()

		begun := time.Now()
		_, err := c.Client(t.Context())
		// A Start that fails still releases waiting clients, which then see
		// the daemon is not running rather than hanging until their deadline.
		require.ErrorContains(t, err, "not running")
		require.GreaterOrEqual(t, time.Since(begun), 10*time.Second, "Client returned before Start finished")
	})
}

func TestClientGivesUpWhenItsContextEnds(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := NewComponent(slog.New(slog.NewTextHandler(io.Discard, nil)), nil, "", t.TempDir())
		finish := startInProgress(c)
		defer finish()

		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()
		_, err := c.Client(ctx)
		require.ErrorIs(t, err, context.DeadlineExceeded)
	})
}
