package saga

import (
	"sync"
	"time"
)

// Event is one thing that can happen to an execution that is worth counting.
//
// The set is fixed so that, with the definition name, it is the whole label
// space of the counters built from it. Nothing here may carry an execution ID,
// an entity, or an error string.
type Event int

const (
	// EventStarted is a new execution persisted for the first time. A named
	// execution continued by a later Execute is not a new start.
	EventStarted Event = iota

	// EventCompleted is an execution that ran every action and finished.
	EventCompleted

	// EventRolledBack is an execution that failed and undid everything it had
	// done, ending Failed.
	EventRolledBack

	// EventCompensationFailed is an undo pass that left at least one action
	// not undone, so the execution stays Undoing for recovery to retry. It
	// counts attempts, not executions: one that cannot be compensated keeps
	// adding to it every time something retries it.
	EventCompensationFailed

	// EventRecovered is an execution that Recover drove to a terminal state.
	EventRecovered

	// EventRecoveryFailed is an execution that Recover attempted and left in
	// flight, for any reason: an action or undo that failed again, a write that
	// did not land, or a refusal to resume at all.
	EventRecoveryFailed

	// EventStrandedForced is an execution the stalled sweep forced to Failed
	// because nothing would ever resume it. On a current cluster any of these
	// means something is still stranding sagas.
	EventStrandedForced

	numEvents
)

// NumEvents is the number of distinct Event values, for iterating a snapshot.
const NumEvents = int(numEvents)

var eventNames = [numEvents]string{
	EventStarted:            "started",
	EventCompleted:          "completed",
	EventRolledBack:         "rolled_back",
	EventCompensationFailed: "compensation_failed",
	EventRecovered:          "recovered",
	EventRecoveryFailed:     "recovery_failed",
	EventStrandedForced:     "stranded_forced",
}

func (e Event) String() string { return eventNames[e] }

// Counts holds cumulative event counts for one process, by definition name.
//
// Executors are created all over the runtime, many of them short-lived inside
// addon providers, so the counts live here rather than on any one executor and
// every executor in the process adds to the same set.
type Counts struct {
	mu    sync.Mutex
	byDef map[string]*definitionCounts
}

type definitionCounts struct {
	events [numEvents]uint64
	since  time.Time
}

// DefinitionCounts is one definition's counts in a Snapshot.
type DefinitionCounts struct {
	// Events holds the cumulative count of each Event.
	Events [NumEvents]uint64

	// Since is when this process first counted anything for the definition.
	// Every count was zero until then, which is a fact the collector can
	// publish: a series whose first pushed sample is already nonzero would
	// otherwise give increase() nothing to measure from.
	Since time.Time
}

// DefaultCounts is the process-wide set executors count into unless given
// another with WithCounts, and the one the metrics collector reads.
var DefaultCounts = &Counts{}

// Add records n occurrences of ev for definition.
func (c *Counts) Add(definition string, ev Event, n uint64) {
	if c == nil || n == 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.byDef == nil {
		c.byDef = make(map[string]*definitionCounts)
	}
	d, ok := c.byDef[definition]
	if !ok {
		d = &definitionCounts{since: time.Now()}
		c.byDef[definition] = d
	}
	d.events[ev] += n
}

// Snapshot copies the current counts. A definition appears once anything has
// been counted for it, with every event present, zeros included.
func (c *Counts) Snapshot() map[string]DefinitionCounts {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make(map[string]DefinitionCounts, len(c.byDef))
	for def, d := range c.byDef {
		out[def] = DefinitionCounts{Events: d.events, Since: d.since}
	}
	return out
}
