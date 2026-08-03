// Package deploylifecycle owns the deployment record state machine: the set of
// valid statuses and phases, and the transitions between them.
//
// It exists as its own package because both servers/deployment and servers/build
// need it, and servers/build importing servers/deployment would be backwards.
package deploylifecycle

import (
	"fmt"
	"strings"

	"miren.dev/runtime/pkg/cond"
)

// Status is the lifecycle state of a deployment record.
type Status string

const (
	StatusInProgress Status = "in_progress"
	StatusActive     Status = "active"
	StatusSucceeded  Status = "succeeded"
	StatusFailed     Status = "failed"
	StatusRolledBack Status = "rolled_back"
	StatusCancelled  Status = "cancelled"
)

// Phase is the fine-grained progress of a deployment that is still in_progress.
type Phase string

const (
	PhasePreparing  Phase = "preparing"
	PhaseBuilding   Phase = "building"
	PhasePushing    Phase = "pushing"
	PhaseActivating Phase = "activating"
)

// allStatuses and allPhases are ordered for stable error messages. They are
// unexported so an importer cannot mutate the sets derived from them.
var (
	allStatuses = []Status{
		StatusInProgress, StatusActive, StatusSucceeded,
		StatusFailed, StatusRolledBack, StatusCancelled,
	}
	allPhases = []Phase{PhasePreparing, PhaseBuilding, PhasePushing, PhaseActivating}
)

// terminalStatuses is the explicit set of statuses with no outbound
// transitions. Stating it here — rather than inferring "terminal" from a
// missing validTransitions entry — is what makes the init check below able to
// catch a status that was added without deciding whether it is terminal.
var terminalStatuses = map[Status]struct{}{
	StatusSucceeded:  {},
	StatusFailed:     {},
	StatusRolledBack: {},
	StatusCancelled:  {},
}

// validTransitions maps a status to the statuses reachable from it.
//
// in_progress is reachable from itself so that a no-op update (the client
// re-asserting the state it is already in) is not an error.
var validTransitions = map[Status][]Status{
	StatusInProgress: {StatusInProgress, StatusActive, StatusFailed, StatusCancelled},
	StatusActive:     {StatusSucceeded, StatusRolledBack},
}

// init fails fast if the three sources of truth ever drift: every status must be
// either terminal or have outbound transitions, and never both. A new status
// added to allStatuses without a decision here panics at startup rather than
// silently becoming terminal — which the lock manager would read as "abandoned".
func init() {
	for _, s := range allStatuses {
		_, terminal := terminalStatuses[s]
		_, hasEdges := validTransitions[s]
		if terminal == hasEdges {
			panic("deploylifecycle: status " + string(s) +
				" must be exactly one of terminal or transitionable")
		}
	}
	for from := range validTransitions {
		if !from.Valid() {
			panic("deploylifecycle: validTransitions references unknown status " + string(from))
		}
	}
}

// Valid reports whether s is a status this system recognizes. Records read back
// from storage can carry anything, so callers validating stored data should use
// this rather than assuming.
func (s Status) Valid() bool {
	_, ok := statusSet[s]
	return ok
}

// Terminal reports whether s admits no further transitions. A deployment in a
// terminal status holds no lock and will not change again.
func (s Status) Terminal() bool {
	_, ok := terminalStatuses[s]
	return ok
}

func (s Status) String() string { return string(s) }

// Valid reports whether p is a phase this system recognizes.
func (p Phase) Valid() bool {
	_, ok := phaseSet[p]
	return ok
}

func (p Phase) String() string { return string(p) }

var (
	statusSet = indexOf(allStatuses)
	phaseSet  = indexOf(allPhases)
)

func indexOf[T comparable](values []T) map[T]struct{} {
	set := make(map[T]struct{}, len(values))
	for _, v := range values {
		set[v] = struct{}{}
	}
	return set
}

// ParseStatus converts a wire/stored string into a Status, rejecting unknown
// values with a validation failure.
func ParseStatus(s string) (Status, error) {
	status := Status(s)
	if !status.Valid() {
		return "", cond.ValidationFailure("invalid-status",
			"status must be one of: "+joinStatuses(allStatuses))
	}
	return status, nil
}

// ParsePhase converts a wire/stored string into a Phase, rejecting unknown
// values with a validation failure.
func ParsePhase(s string) (Phase, error) {
	phase := Phase(s)
	if !phase.Valid() {
		return "", cond.ValidationFailure("invalid-phase",
			"phase must be one of: "+joinPhases(allPhases))
	}
	return phase, nil
}

// Transition reports whether a deployment may move from one status to another.
// It is the single authority on that question: every write path goes through it
// rather than re-deriving the rules.
//
// A rejected transition is a conflict, not a validation failure — the request is
// well-formed, it just lost a race or arrived against a record that has already
// finished.
func Transition(from, to Status) error {
	if !to.Valid() {
		return cond.ValidationFailure("invalid-status",
			"status must be one of: "+joinStatuses(allStatuses))
	}

	for _, allowed := range validTransitions[from] {
		if allowed == to {
			return nil
		}
	}

	return cond.Conflict("deployment-status",
		fmt.Sprintf("cannot transition deployment from %s to %s", from, to))
}

// CheckPhase reports whether a phase may be recorded against a deployment in the
// given status. Phases describe work still in flight, so they are meaningless
// once a deployment has left in_progress.
func CheckPhase(status Status, phase Phase) error {
	if !phase.Valid() {
		return cond.ValidationFailure("invalid-phase",
			"phase must be one of: "+joinPhases(allPhases))
	}

	if status != StatusInProgress {
		return cond.Conflict("deployment-status",
			fmt.Sprintf("cannot set phase on deployment in %s state", status))
	}

	return nil
}

func joinStatuses(statuses []Status) string {
	parts := make([]string, len(statuses))
	for i, s := range statuses {
		parts[i] = string(s)
	}
	return strings.Join(parts, ", ")
}

func joinPhases(phases []Phase) string {
	parts := make([]string, len(phases))
	for i, p := range phases {
		parts[i] = string(p)
	}
	return strings.Join(parts, ", ")
}
