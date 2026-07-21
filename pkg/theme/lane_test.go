package theme

import (
	"slices"
	"testing"
)

// TestLaneStable checks that a service name always hashes to the same lane, so a
// service keeps its color across restarts and throughout a session.
func TestLaneStable(t *testing.T) {
	for _, seed := range []string{"web", "bgtask", "worker", "linear-issue-bridge"} {
		first := Lane(seed)
		for range 100 {
			if got := Lane(seed); got != first {
				t.Fatalf("Lane(%q) not stable: %v != %v", seed, got, first)
			}
		}
	}
}

// TestLaneEmptyIsMuted checks that an empty seed falls back to Muted rather than
// hashing to a random lane.
func TestLaneEmptyIsMuted(t *testing.T) {
	if Lane("") != Muted {
		t.Fatalf("Lane(\"\") = %v, want Muted", Lane(""))
	}
}

// TestLaneWithinPalette checks that every returned lane is a member of the
// exported palette (guards against index math drift).
func TestLaneWithinPalette(t *testing.T) {
	lanes := Lanes()
	for _, seed := range []string{"a", "b", "c", "web", "worker", "cache", "queue", "api"} {
		got := Lane(seed)
		if !slices.Contains(lanes, got) {
			t.Fatalf("Lane(%q) = %v not in Lanes()", seed, got)
		}
	}
}
