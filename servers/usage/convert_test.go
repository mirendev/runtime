package usage

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// roughly compares a duration against a target, absorbing the time.Now() call
// inside restWindow. The tolerance is far tighter than any span under test.
func roughly(t *testing.T, want, got time.Duration, msg string) {
	t.Helper()
	require.WithinDuration(t,
		time.Time{}.Add(want), time.Time{}.Add(got), time.Second, msg)
}

// since and until are each measured backwards from now. Deriving since from an
// already shifted end would make them cumulative, so the window a caller asks
// for by name would not be the window they get.
func TestRESTWindowMeasuresBothOffsetsFromNow(t *testing.T) {
	now := time.Now()

	w := restWindow("2h", "1h", "")

	roughly(t, time.Hour, now.Sub(w.end), "until should place the end one hour ago")
	roughly(t, 2*time.Hour, now.Sub(w.start), "since should place the start two hours ago")
	roughly(t, time.Hour, w.duration(), "?since=2h&until=1h names a one-hour span")
}

func TestRESTWindowDefaults(t *testing.T) {
	now := time.Now()

	w := restWindow("", "", "")

	roughly(t, 0, now.Sub(w.end), "an absent until ends the window now")
	roughly(t, defaultWindow, w.duration(), "an absent since uses the default length")
}

// until alone still leaves a window of the default length, ending where it asks.
func TestRESTWindowUntilAloneKeepsDefaultLength(t *testing.T) {
	now := time.Now()

	w := restWindow("", "30m", "")

	roughly(t, 30*time.Minute, now.Sub(w.end), "until should place the end")
	roughly(t, defaultWindow, w.duration(), "the span keeps its default length")
}

// A since inside until describes no span. Answering with the default length is
// what the RPC surface does for the same case, and a diagnostic endpoint is
// better off answering than returning a 400.
func TestRESTWindowRejectsAnInvertedSpan(t *testing.T) {
	now := time.Now()

	w := restWindow("1h", "2h", "")

	roughly(t, 2*time.Hour, now.Sub(w.end), "until still places the end")
	require.True(t, w.start.Before(w.end), "the window must not be inverted")
	roughly(t, defaultWindow, w.duration(), "an inverted span falls back to the default length")
}

// An unparseable duration is ignored rather than rejected, so a typo answers
// with the default rather than an error.
func TestRESTWindowIgnoresUnparseableDurations(t *testing.T) {
	now := time.Now()

	w := restWindow("banana", "-5m", "")

	roughly(t, 0, now.Sub(w.end), "a negative until is ignored")
	roughly(t, defaultWindow, w.duration(), "an unparseable since is ignored")
}
