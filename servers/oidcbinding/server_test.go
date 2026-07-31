package oidcbinding

import (
	"testing"

	"miren.dev/runtime/api/core/core_v1alpha"
)

func TestIdentifiesCaller(t *testing.T) {
	tests := []struct {
		name       string
		subject    string
		conditions []core_v1alpha.ClaimConditions
		want       bool
	}{
		{
			name:    "subject pattern alone",
			subject: "repo:acme/app:*",
			want:    true,
		},
		{
			name: "repository claim alone",
			conditions: []core_v1alpha.ClaimConditions{
				{Key: "repository", Pattern: "acme/app"},
				{Key: "event_name", Pattern: "push,workflow_dispatch"},
			},
			want: true,
		},
		{
			name:       "nothing at all",
			conditions: nil,
			want:       false,
		},
		{
			// event_name says what triggered the workflow, not who ran it. A
			// binding constrained only by it accepts a push from any repo on
			// github.com.
			name: "event_name alone",
			conditions: []core_v1alpha.ClaimConditions{
				{Key: "event_name", Pattern: "push"},
			},
			want: false,
		},
		{
			// Reachable from the CLI's generic path with just --issuer and
			// --allowed-refs. A branch name says nothing about which repo the
			// push came from, so this would accept any repo on github.com.
			name: "ref alone",
			conditions: []core_v1alpha.ClaimConditions{
				{Key: "ref", Pattern: "refs/heads/main"},
			},
			want: false,
		},
		{
			name: "run-scoped claims however specific",
			conditions: []core_v1alpha.ClaimConditions{
				{Key: "event_name", Pattern: "push"},
				{Key: "ref", Pattern: "refs/heads/main"},
				{Key: "ref_type", Pattern: "branch"},
				{Key: "ref_protected", Pattern: "true"},
				{Key: "runner_environment", Pattern: "github-hosted"},
				{Key: "repository_visibility", Pattern: "private"},
			},
			want: false,
		},
		{
			name: "one identifying claim among run-scoped ones is enough",
			conditions: []core_v1alpha.ClaimConditions{
				{Key: "ref", Pattern: "refs/heads/main"},
				{Key: "repository", Pattern: "acme/app"},
			},
			want: true,
		},
		{
			name:    "bare wildcard subject",
			subject: "*",
			conditions: []core_v1alpha.ClaimConditions{
				{Key: "event_name", Pattern: "push"},
			},
			want: false,
		},
		{
			name:    "wildcard subject rescued by a repository claim",
			subject: "*",
			conditions: []core_v1alpha.ClaimConditions{
				{Key: "repository", Pattern: "acme/app"},
			},
			want: true,
		},
		{
			name: "wildcard repository claim constrains nothing",
			conditions: []core_v1alpha.ClaimConditions{
				{Key: "repository", Pattern: "*"},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := identifiesCaller(tt.subject, tt.conditions); got != tt.want {
				t.Errorf("identifiesCaller(%q, %v) = %v, want %v", tt.subject, tt.conditions, got, tt.want)
			}
		})
	}
}
