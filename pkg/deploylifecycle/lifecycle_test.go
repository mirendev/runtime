package deploylifecycle

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/pkg/cond"
)

// cond.ErrValidationFailure carries no Is method, so errors.Is would compare it
// by field equality. errors.AsType is the assertion that actually means "this
// kind of error". ErrConflict is matched the same way for symmetry.
func isValidationFailure(err error) bool {
	_, ok := errors.AsType[cond.ErrValidationFailure](err)
	return ok
}

func isConflict(err error) bool {
	_, ok := errors.AsType[cond.ErrConflict](err)
	return ok
}

// allowedTransitions is the expected state machine, written out independently of
// the implementation so the matrix test below is a real check rather than a
// restatement of validTransitions.
var allowedTransitions = map[Status]map[Status]bool{
	StatusInProgress: {
		StatusInProgress: true,
		StatusActive:     true,
		StatusFailed:     true,
		StatusCancelled:  true,
	},
	StatusActive: {
		StatusSucceeded:  true,
		StatusRolledBack: true,
	},
	StatusSucceeded:  {},
	StatusFailed:     {},
	StatusRolledBack: {},
	StatusCancelled:  {},
}

// TestTransitionMatrix exercises every (from, to) pair, so a new status or a
// widened edge cannot be added without updating the expectation above.
func TestTransitionMatrix(t *testing.T) {
	for _, from := range allStatuses {
		for _, to := range allStatuses {
			from, to := from, to
			t.Run(string(from)+"->"+string(to), func(t *testing.T) {
				err := Transition(from, to)

				if allowedTransitions[from][to] {
					require.NoError(t, err)
					return
				}

				require.Error(t, err)
				assert.True(t, isConflict(err),
					"a rejected transition is a conflict, got %T: %v", err, err)
				assert.Contains(t, err.Error(), string(from))
				assert.Contains(t, err.Error(), string(to))
			})
		}
	}
}

func TestTransitionToUnknownStatusIsValidationFailure(t *testing.T) {
	err := Transition(StatusInProgress, Status("banana"))

	require.Error(t, err)
	assert.True(t, isValidationFailure(err),
		"an unrecognized target status is malformed input, not a conflict; got %T", err)
}

// A record whose stored status we do not recognize must not become a wildcard
// that transitions anywhere.
func TestTransitionFromUnknownStatusIsRejected(t *testing.T) {
	for _, to := range allStatuses {
		err := Transition(Status("banana"), to)
		require.Error(t, err, "unknown source status must not reach %s", to)
	}
}

func TestTerminal(t *testing.T) {
	terminal := map[Status]bool{
		StatusSucceeded:  true,
		StatusFailed:     true,
		StatusRolledBack: true,
		StatusCancelled:  true,
	}

	for _, s := range allStatuses {
		assert.Equal(t, terminal[s], s.Terminal(), "Terminal() for %s", s)
	}

	assert.False(t, Status("banana").Terminal(),
		"an unrecognized status is not terminal — it is not a status at all")
}

// Every status must be classified exactly once: terminal, or transitionable.
// This is the invariant the package init() enforces at startup; asserting it in
// a test as well gives a clear failure rather than a panic during import.
func TestEveryStatusIsClassifiedExactlyOnce(t *testing.T) {
	for _, s := range allStatuses {
		_, terminal := terminalStatuses[s]
		_, hasEdges := validTransitions[s]
		assert.NotEqual(t, terminal, hasEdges,
			"status %s must be exactly one of terminal or transitionable", s)
	}
}

func TestParseStatus(t *testing.T) {
	for _, s := range allStatuses {
		got, err := ParseStatus(string(s))
		require.NoError(t, err)
		assert.Equal(t, s, got)
	}

	_, err := ParseStatus("banana")
	require.Error(t, err)
	assert.True(t, isValidationFailure(err))
	assert.Contains(t, err.Error(), "cancelled",
		"the message should list every accepted status")

	_, err = ParseStatus("")
	require.Error(t, err)
}

func TestParsePhase(t *testing.T) {
	for _, p := range allPhases {
		got, err := ParsePhase(string(p))
		require.NoError(t, err)
		assert.Equal(t, p, got)
	}

	_, err := ParsePhase("banana")
	require.Error(t, err)
	assert.True(t, isValidationFailure(err))

	_, err = ParsePhase("")
	require.Error(t, err)
}

func TestCheckPhase(t *testing.T) {
	for _, p := range allPhases {
		require.NoError(t, CheckPhase(StatusInProgress, p),
			"%s is valid while in_progress", p)
	}

	for _, s := range allStatuses {
		if s == StatusInProgress {
			continue
		}

		err := CheckPhase(s, PhaseBuilding)
		require.Error(t, err, "phases are meaningless in %s", s)
		assert.True(t, isConflict(err), "got %T", err)
	}

	err := CheckPhase(StatusInProgress, Phase("banana"))
	require.Error(t, err)
	assert.True(t, isValidationFailure(err),
		"an unrecognized phase is malformed input, not a conflict; got %T", err)
}
