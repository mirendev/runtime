package idgen

import (
	"testing"
	"time"
)

// The point of TimeOf is that an id carries its own creation time, so a record
// whose timestamp field was never written still has a stable one available.
func TestTimeOfRecoversGenerationTime(t *testing.T) {
	before := time.Now().UTC().Add(-time.Second)
	id := GenNS("deployment")
	after := time.Now().UTC().Add(time.Second)

	got, ok := TimeOf(id)
	if !ok {
		t.Fatalf("TimeOf(%q) reported no time", id)
	}
	if got.Before(before) || got.After(after) {
		t.Errorf("TimeOf(%q) = %s, want between %s and %s", id, got, before, after)
	}
}

// Stability is the property the deploy log actually leans on: the value is used
// as a partition key, so the same id must always decode to the same instant.
func TestTimeOfIsStable(t *testing.T) {
	id := GenNS("deployment")

	first, ok := TimeOf(id)
	if !ok {
		t.Fatalf("TimeOf(%q) reported no time", id)
	}

	for range 5 {
		again, ok := TimeOf(id)
		if !ok || !again.Equal(first) {
			t.Fatalf("TimeOf(%q) = %s, %v on a repeat call; want a stable %s", id, again, ok, first)
		}
	}
}

// Kind prefixes can contain a separator themselves, so the decoder has to cut
// at the last one rather than the first.
func TestTimeOfHandlesPrefixesContainingSeparators(t *testing.T) {
	id := GenNS("disk-lease")

	if _, ok := TimeOf(id); !ok {
		t.Errorf("TimeOf(%q) reported no time for a multi-part prefix", id)
	}
}

func TestTimeOfRejectsUnreadableIds(t *testing.T) {
	cases := map[string]string{
		"empty":          "",
		"prefix only":    "deployment-",
		"not base58":     "deployment-0OIl+/",
		"too short":      "deployment-" + "2g",
		"no time bytes":  "deployment-" + "11111111111111111111111",
		"bare separator": "-",
	}

	// A truncated id decodes to fewer than sixteen bytes, and reading a
	// timestamp out of its leading bytes would give a confident wrong answer.
	full := GenNS("deployment")
	cases["truncated id"] = full[:len(full)-3]

	for name, id := range cases {
		t.Run(name, func(t *testing.T) {
			if got, ok := TimeOf(id); ok {
				t.Errorf("TimeOf(%q) = %s, true; want false so the caller has to decide what an unreadable id means", id, got)
			}
		})
	}
}
