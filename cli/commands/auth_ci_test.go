package commands

import (
	"testing"
)

func TestGitHubClaimConditions(t *testing.T) {
	conditions, err := gitHubClaimConditions("acme/web-app", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := map[string]string{}
	for _, cc := range conditions {
		got[cc.Key()] = cc.Pattern()
	}

	want := map[string]string{
		"repository":       "acme/web-app",
		"repository_owner": "acme",
		"event_name":       "push,workflow_dispatch",
	}
	for key, pattern := range want {
		if got[key] != pattern {
			t.Errorf("%s = %q, want %q", key, got[key], pattern)
		}
	}
	if len(got) != len(want) {
		t.Errorf("got %d conditions, want %d: %v", len(got), len(want), got)
	}
}

// The subject claim is off limits for GitHub bindings: it's the thing that
// changed formats, and anything derived from owner/repo names can't match a
// post-cutover repo.
func TestGitHubClaimConditions_NoSubjectDerivedCondition(t *testing.T) {
	conditions, err := gitHubClaimConditions("acme/web-app", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, cc := range conditions {
		if cc.Key() == "sub" {
			t.Errorf("binding should not constrain the sub claim, got pattern %q", cc.Pattern())
		}
	}
}

func TestGitHubClaimConditions_Overrides(t *testing.T) {
	conditions, err := gitHubClaimConditions("acme/web-app", "workflow_dispatch", "refs/heads/main")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := map[string]string{}
	for _, cc := range conditions {
		got[cc.Key()] = cc.Pattern()
	}

	if got["event_name"] != "workflow_dispatch" {
		t.Errorf("event_name = %q, want workflow_dispatch", got["event_name"])
	}
	if got["ref"] != "refs/heads/main" {
		t.Errorf("ref = %q, want refs/heads/main", got["ref"])
	}
}

func TestGitHubClaimConditions_RejectsMalformedShorthand(t *testing.T) {
	for _, input := range []string{"acme", "", "/web-app", "acme/", "acme/web-app/extra"} {
		if _, err := gitHubClaimConditions(input, "", ""); err == nil {
			t.Errorf("expected %q to be rejected as owner/repo shorthand", input)
		}
	}
}
