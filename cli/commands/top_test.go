package commands

import (
	"context"
	"errors"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTopModel() *topModel {
	// The interval only has to be short enough that running a scheduled tick
	// does not slow the test; nothing here measures elapsed time.
	return &topModel{interval: time.Millisecond, ctx: context.Background()}
}

func pressR() tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}}
}

// A completed refresh schedules the next tick, so a manual refresh arriving
// while one is already pending leaves two chains running: the manual result
// schedules its own tick, the pending one fires and schedules another, and the
// poll rate doubles for every press. In the node view one refresh is several
// metrics queries, so this is not a subtle cost.
func TestTopWatchManualRefreshDoesNotMultiplyThePollRate(t *testing.T) {
	r := require.New(t)
	m := newTopModel()

	r.NotNil(m.Init(), "the model refreshes on start")

	_, cmd := m.Update(topResultMsg{gen: 1, body: "first"})
	r.NotNil(cmd)
	r.Equal(topRefreshMsg{gen: 1}, cmd(), "a result schedules the next tick")

	_, cmd = m.Update(pressR())
	r.NotNil(cmd, "r refreshes immediately")

	_, cmd = m.Update(topRefreshMsg{gen: 1})
	r.Nil(cmd, "the superseded chain's tick must not start another refresh")

	_, cmd = m.Update(topResultMsg{gen: 2, body: "second"})
	r.NotNil(cmd)
	r.Equal(topRefreshMsg{gen: 2}, cmd(), "the current chain schedules its own tick")
	r.Equal("second", m.body)

	_, cmd = m.Update(topRefreshMsg{gen: 2})
	r.NotNil(cmd, "the current chain keeps refreshing")
}

// Two presses in quick succession leave two refreshes in flight. The first to
// return is already out of date, and drawing it would show the older of the two
// snapshots.
func TestTopWatchIgnoresASupersededResult(t *testing.T) {
	r := require.New(t)
	m := newTopModel()

	m.Init()
	m.Update(pressR())

	_, cmd := m.Update(topResultMsg{gen: 1, body: "stale"})
	r.Nil(cmd, "a superseded result must not schedule a tick")
	r.Empty(m.body, "a superseded result must not be drawn")
}

// Which chain found a failure says nothing about whether it is real, so an
// error is reported even when its refresh has been superseded.
func TestTopWatchReportsAnErrorFromAnySuperseededChain(t *testing.T) {
	r := require.New(t)
	m := newTopModel()

	m.Init()
	m.Update(pressR())

	boom := errors.New("metrics backend unreachable")
	_, cmd := m.Update(topResultMsg{gen: 1, err: boom})

	r.Equal(boom, m.err)
	r.NotNil(cmd, "an error quits rather than waiting for the current chain")
}

// formatDuration stops rolling up at minutes, which renders a week as
// "10080m0s". These views span seconds to weeks, so the unit has to follow.
func TestFormatSpanFollowsTheScale(t *testing.T) {
	cases := []struct {
		in   time.Duration
		want string
	}{
		{45 * time.Second, "45s"},
		{90 * time.Second, "1m"},
		{2 * time.Hour, "2h"},
		{2*time.Hour + 30*time.Minute, "2h30m"},
		{7 * 24 * time.Hour, "7d"},
		{50 * time.Hour, "2d2h"},
		{-time.Second, "0s"},
	}

	for _, c := range cases {
		assert.Equal(t, c.want, formatSpan(c.in), "formatSpan(%s)", c.in)
	}
}
