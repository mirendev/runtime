package grunge

import (
	"log/slog"
	"testing"
)

// A device that isn't there is the ordinary case on a node that never ran the
// other backend, so it has to read as success rather than as a lookup failure.
func TestRemoveDeviceIfPresentTreatsAbsenceAsSuccess(t *testing.T) {
	n := &Network{log: slog.Default()}

	// Deliberately a name nothing will have, so the test can exercise the
	// lookup without any chance of deleting a real interface.
	if err := n.removeDeviceIfPresent("miren-absent-dev0"); err != nil {
		t.Fatalf("expected absence to be treated as success, got %v", err)
	}
}
