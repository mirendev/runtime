package saga

import (
	"fmt"
	"strconv"
	"strings"

	saga_v1alpha "miren.dev/runtime/api/saga/saga_v1alpha"
	"miren.dev/runtime/pkg/entity"
)

// defaultPageLimit is what a backend uses when a caller names no limit.
//
// Sized against what a page costs rather than what it returns: every execution
// carries JSON blobs of its initial inputs and of every action's output, and a
// sandbox creation's outputs are not small. Two hundred of those is a page an
// operator would not notice on a 4 GB host, which is the size of box this whole
// change exists to keep alive.
const defaultPageLimit = 200

// maxPageLimit caps what a caller can ask for. A limit is a request, not an
// instruction: a caller passing a huge one has misunderstood what paging is
// for, and honouring it would reintroduce the unbounded read through the front
// door.
const maxPageLimit = 1000

// clampLimit resolves a caller's limit against the backend's own bounds.
func clampLimit(limit int) int {
	switch {
	case limit <= 0:
		return defaultPageLimit
	case limit > maxPageLimit:
		return maxPageLimit
	default:
		return limit
	}
}

// incompleteStatuses are the status indexes a recovery walk covers, in the
// order it covers them.
//
// The order is load-bearing, so do not reorder these on cost grounds. An
// execution only ever moves forward through pending, running, undoing, and the
// walk is not pinned to one revision, so walking the indexes in that same
// direction means a saga that transitions partway through the walk is seen
// twice at worst. Reverse the order and it can be missed entirely: one sitting
// in running when the undoing index is read, and in undoing by the time the
// running index is read, appears in neither.
//
// Pending also happens to come first for the cheapest reason, an execution that
// crashed between its initial save and its first transition has done the least
// work to resume, but that is the coincidence and the ordering above is the
// requirement.
var incompleteStatuses = []entity.Id{
	saga_v1alpha.SagaStatusPendingId,
	saga_v1alpha.SagaStatusRunningId,
	saga_v1alpha.SagaStatusUndoingId,
}

// terminalStatuses are the status indexes a retention walk covers.
var terminalStatuses = []entity.Id{
	saga_v1alpha.SagaStatusCompletedId,
	saga_v1alpha.SagaStatusFailedId,
}

// A walk covers several status indexes in sequence, so a cursor has to say both
// which index it is in and where inside it. The inner cursor belongs to the
// store that issued it and is opaque here, which is why the stage is encoded as
// a prefix rather than by parsing anything: splitting on the first separator
// leaves whatever the store put in the remainder untouched, including
// separators of its own.
//
// The whole cursor is opaque to callers in the same way. It is only ever handed
// back to the storage that produced it, and a cursor from one backend means
// nothing to another.

// encodeStageCursor joins a stage index and a store cursor.
func encodeStageCursor(stage int, inner string) string {
	return strconv.Itoa(stage) + ":" + inner
}

// decodeStageCursor splits a cursor back into its stage and store parts. An
// empty cursor is the start of the walk.
func decodeStageCursor(cursor string, stages int) (stage int, inner string, err error) {
	if cursor == "" {
		return 0, "", nil
	}

	head, rest, found := strings.Cut(cursor, ":")
	if !found {
		return 0, "", fmt.Errorf("malformed page cursor %q", cursor)
	}

	stage, err = strconv.Atoi(head)
	if err != nil {
		return 0, "", fmt.Errorf("malformed page cursor %q: %w", cursor, err)
	}
	if stage < 0 || stage >= stages {
		return 0, "", fmt.Errorf("page cursor %q names stage %d, which does not exist", cursor, stage)
	}

	return stage, rest, nil
}

// nextStageCursor reports where a walk resumes after finishing a page.
//
// Exhausting one index moves to the head of the next; exhausting the last ends
// the walk, which is the only thing an empty cursor means.
func nextStageCursor(stage int, inner string, stages int) string {
	if inner != "" {
		return encodeStageCursor(stage, inner)
	}
	if stage+1 < stages {
		return encodeStageCursor(stage+1, "")
	}
	return ""
}
