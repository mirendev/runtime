package compute_v1alpha

import (
	"testing"

	entity "miren.dev/runtime/pkg/entity"
)

func TestNewNodeId(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want NodeId
	}{
		{"raw uuid", "abc123", "node/abc123"},
		{"primary", "miren", "node/miren"},
		{"already prefixed", "node/abc123", "node/abc123"},
		{"double prefixed", "node/node/abc123", "node/abc123"},
		{"triple prefixed", "node/node/node/abc123", "node/abc123"},
		{"empty", "", "node/"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := NewNodeId(tc.raw); got != tc.want {
				t.Fatalf("NewNodeId(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

func TestNodeIdMatches(t *testing.T) {
	n := NewNodeId("abc123")
	if !n.Matches(entity.Id("node/abc123")) {
		t.Fatalf("expected %q to match node/abc123", n)
	}
	if n.Matches(entity.Id("node/other")) {
		t.Fatalf("did not expect %q to match node/other", n)
	}
	if n.Matches(entity.Id("abc123")) {
		t.Fatalf("unprefixed raw id should not match a canonical NodeId")
	}
}
