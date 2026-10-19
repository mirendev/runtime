package rbac

import (
	"sync"
	"testing"
)

// MemoryPolicyStore is a simple in-memory implementation of PolicyProvider for testing
type MemoryPolicyStore struct {
	mu     sync.RWMutex
	policy *Policy
}

// NewMemoryPolicyStore creates a new in-memory policy store
func NewMemoryPolicyStore() *MemoryPolicyStore {
	return &MemoryPolicyStore{}
}

// GetPolicy returns the current policy
func (s *MemoryPolicyStore) GetPolicy() *Policy {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.policy
}

// UpdatePolicy updates the stored policy
func (s *MemoryPolicyStore) UpdatePolicy(policy *Policy) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.policy = policy
}

func TestEvaluatorWithTags(t *testing.T) {
	// Create a policy with tag-based rules
	policy := &Policy{
		Rules: []Rule{
			{
				ID:          "prod-admin",
				Name:        "Production Admin Access",
				Description: "Full access for production cluster",
				TagSelector: TagSelector{
					Expressions: []TagExpression{
						{Tag: "environment", Value: "production", Operator: "equals"},
						{Tag: "cluster", Value: "prod-1", Operator: "equals"},
					},
				},
				Groups: []string{"admins"},
				Permissions: []Permission{
					{
						Resource: "*",
						Actions:  []string{"*"},
					},
				},
			},
			{
				ID:          "dev-developer",
				Name:        "Developer Access",
				Description: "Developer access for dev clusters",
				TagSelector: TagSelector{
					Expressions: []TagExpression{
						{Tag: "environment", Value: "development", Operator: "equals"},
					},
				},
				Groups: []string{"developers"},
				Permissions: []Permission{
					{
						Resource: "apps/*",
						Actions:  []string{"read", "write", "execute"},
					},
					{
						Resource: "sandboxes/*",
						Actions:  []string{"read", "write", "execute"},
					},
				},
			},
			{
				ID:          "all-viewers",
				Name:        "Viewer Access",
				Description: "Read-only access for all clusters",
				TagSelector: TagSelector{}, // Matches all tags
				Groups:      []string{"viewers"},
				Permissions: []Permission{
					{
						Resource: "apps/*",
						Actions:  []string{"read"},
					},
					{
						Resource: "sandboxes/*",
						Actions:  []string{"read"},
					},
				},
			},
		},
	}

	// Create a memory store with the policy
	store := NewMemoryPolicyStore()
	store.UpdatePolicy(policy)

	// Create evaluator
	eval := NewEvaluator(t.Context(), store, nil)

	tests := []struct {
		name     string
		req      *Request
		expected Decision
	}{
		{
			name: "admin in production cluster allowed everything",
			req: &Request{
				Subject:  "sa-admin",
				Groups:   []string{"admins"},
				Resource: "secrets/private",
				Action:   "delete",
				Tags: map[string]any{
					"environment": "production",
					"cluster":     "prod-1",
				},
			},
			expected: DecisionAllow,
		},
		{
			name: "admin in dev cluster denied (wrong tags)",
			req: &Request{
				Subject:  "sa-admin",
				Groups:   []string{"admins"},
				Resource: "secrets/private",
				Action:   "delete",
				Tags: map[string]any{
					"environment": "development",
					"cluster":     "dev-1",
				},
			},
			expected: DecisionDeny,
		},
		{
			name: "developer in dev cluster allowed apps write",
			req: &Request{
				Subject:  "sa-dev",
				Groups:   []string{"developers"},
				Resource: "apps/myapp",
				Action:   "write",
				Tags: map[string]any{
					"environment": "development",
					"cluster":     "dev-1",
				},
			},
			expected: DecisionAllow,
		},
		{
			name: "developer in prod cluster denied",
			req: &Request{
				Subject:  "sa-dev",
				Groups:   []string{"developers"},
				Resource: "apps/myapp",
				Action:   "write",
				Tags: map[string]any{
					"environment": "production",
					"cluster":     "prod-1",
				},
			},
			expected: DecisionDeny,
		},
		{
			name: "viewer allowed read in any cluster",
			req: &Request{
				Subject:  "sa-viewer",
				Groups:   []string{"viewers"},
				Resource: "apps/myapp",
				Action:   "read",
				Tags: map[string]any{
					"environment": "production",
					"cluster":     "prod-1",
				},
			},
			expected: DecisionAllow,
		},
		{
			name: "viewer denied write in any cluster",
			req: &Request{
				Subject:  "sa-viewer",
				Groups:   []string{"viewers"},
				Resource: "apps/myapp",
				Action:   "write",
				Tags: map[string]any{
					"environment": "development",
					"cluster":     "dev-1",
				},
			},
			expected: DecisionDeny,
		},
		{
			name: "no matching groups denied",
			req: &Request{
				Subject:  "sa-unknown",
				Groups:   []string{"unknown-group"},
				Resource: "apps/myapp",
				Action:   "read",
				Tags: map[string]any{
					"environment": "development",
					"cluster":     "dev-1",
				},
			},
			expected: DecisionDeny,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decision := eval.Evaluate(tt.req)
			if decision != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, decision)
			}
		})
	}
}

func TestRuleTagMatching(t *testing.T) {
	tests := []struct {
		name        string
		rule        Rule
		tags        map[string]any
		shouldMatch bool
	}{
		{
			name: "exact match with equals",
			rule: Rule{
				TagSelector: TagSelector{
					Expressions: []TagExpression{
						{Tag: "env", Value: "prod", Operator: "equals"},
						{Tag: "cluster", Value: "us-west", Operator: "equals"},
					},
				},
			},
			tags: map[string]any{
				"env":     "prod",
				"cluster": "us-west",
			},
			shouldMatch: true,
		},
		{
			name: "not_equals operator",
			rule: Rule{
				TagSelector: TagSelector{
					Expressions: []TagExpression{
						{Tag: "env", Value: "dev", Operator: "not_equals"},
					},
				},
			},
			tags: map[string]any{
				"env": "prod",
			},
			shouldMatch: true,
		},
		{
			name: "exists operator",
			rule: Rule{
				TagSelector: TagSelector{
					Expressions: []TagExpression{
						{Tag: "cluster", Value: nil, Operator: "exists"},
					},
				},
			},
			tags: map[string]any{
				"cluster": "any-value",
			},
			shouldMatch: true,
		},
		{
			name: "not_exists operator",
			rule: Rule{
				TagSelector: TagSelector{
					Expressions: []TagExpression{
						{Tag: "test", Value: nil, Operator: "not_exists"},
					},
				},
			},
			tags: map[string]any{
				"env": "prod",
			},
			shouldMatch: true,
		},
		{
			name: "in operator with array",
			rule: Rule{
				TagSelector: TagSelector{
					Expressions: []TagExpression{
						{Tag: "env", Value: []string{"prod", "staging", "dev"}, Operator: "in"},
					},
				},
			},
			tags: map[string]any{
				"env": "staging",
			},
			shouldMatch: true,
		},
		{
			name: "in operator with comma-separated string",
			rule: Rule{
				TagSelector: TagSelector{
					Expressions: []TagExpression{
						{Tag: "env", Value: "prod,staging,dev", Operator: "in"},
					},
				},
			},
			tags: map[string]any{
				"env": "staging",
			},
			shouldMatch: true,
		},
		{
			name: "not_in operator with array",
			rule: Rule{
				TagSelector: TagSelector{
					Expressions: []TagExpression{
						{Tag: "env", Value: []string{"test", "dev"}, Operator: "not_in"},
					},
				},
			},
			tags: map[string]any{
				"env": "prod",
			},
			shouldMatch: true,
		},
		{
			name: "missing required tag",
			rule: Rule{
				TagSelector: TagSelector{
					Expressions: []TagExpression{
						{Tag: "env", Value: "prod", Operator: "equals"},
					},
				},
			},
			tags:        map[string]any{},
			shouldMatch: false,
		},
		{
			name: "extra tags allowed",
			rule: Rule{
				TagSelector: TagSelector{
					Expressions: []TagExpression{
						{Tag: "env", Value: "prod", Operator: "equals"},
					},
				},
			},
			tags: map[string]any{
				"env":     "prod",
				"cluster": "us-west",
				"extra":   "value",
			},
			shouldMatch: true,
		},
		{
			name: "wrong tag value",
			rule: Rule{
				TagSelector: TagSelector{
					Expressions: []TagExpression{
						{Tag: "env", Value: "prod", Operator: "equals"},
					},
				},
			},
			tags: map[string]any{
				"env": "dev",
			},
			shouldMatch: false,
		},
		{
			name: "empty selector matches all",
			rule: Rule{
				TagSelector: TagSelector{},
			},
			tags: map[string]any{
				"any": "value",
			},
			shouldMatch: true,
		},
		{
			name: "multiple expressions all must match",
			rule: Rule{
				TagSelector: TagSelector{
					Expressions: []TagExpression{
						{Tag: "env", Value: "prod", Operator: "equals"},
						{Tag: "region", Value: []string{"us-east", "us-west"}, Operator: "in"},
						{Tag: "test", Value: nil, Operator: "not_exists"},
					},
				},
			},
			tags: map[string]any{
				"env":    "prod",
				"region": "us-west",
			},
			shouldMatch: true,
		},
		{
			name: "in operator with numeric values",
			rule: Rule{
				TagSelector: TagSelector{
					Expressions: []TagExpression{
						{Tag: "port", Value: []any{80, 443, 8080}, Operator: "in"},
					},
				},
			},
			tags: map[string]any{
				"port": 443,
			},
			shouldMatch: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matches := tt.rule.MatchesTags(tt.tags)
			if matches != tt.shouldMatch {
				t.Errorf("expected %v, got %v", tt.shouldMatch, matches)
			}
		})
	}
}

func TestRulePermissionMatching(t *testing.T) {
	rule := Rule{
		Permissions: []Permission{
			{
				Resource: "apps/*",
				Actions:  []string{"read", "write"},
			},
			{
				Resource: "sandboxes/prod-*",
				Actions:  []string{"*"},
			},
		},
	}

	tests := []struct {
		name        string
		resource    string
		action      string
		shouldAllow bool
	}{
		{
			name:        "apps read allowed",
			resource:    "apps/myapp",
			action:      "read",
			shouldAllow: true,
		},
		{
			name:        "apps write allowed",
			resource:    "apps/myapp",
			action:      "write",
			shouldAllow: true,
		},
		{
			name:        "apps delete not allowed",
			resource:    "apps/myapp",
			action:      "delete",
			shouldAllow: false,
		},
		{
			name:        "sandboxes prod wildcard allowed",
			resource:    "sandboxes/prod-test",
			action:      "delete",
			shouldAllow: true,
		},
		{
			name:        "sandboxes dev not matched",
			resource:    "sandboxes/dev-test",
			action:      "read",
			shouldAllow: false,
		},
		{
			name:        "secrets not allowed",
			resource:    "secrets/private",
			action:      "read",
			shouldAllow: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			allowed := rule.HasPermission(tt.resource, tt.action)
			if allowed != tt.shouldAllow {
				t.Errorf("expected %v, got %v", tt.shouldAllow, allowed)
			}
		})
	}
}

func TestEvaluatorCache(t *testing.T) {
	policy := &Policy{
		Rules: []Rule{
			{
				ID:          "test-rule",
				Name:        "Test Rule",
				TagSelector: TagSelector{},
				Groups:      []string{"test-group"},
				Permissions: []Permission{
					{
						Resource: "test/*",
						Actions:  []string{"read"},
					},
				},
			},
		},
	}

	store := NewMemoryPolicyStore()
	store.UpdatePolicy(policy)
	eval := NewEvaluator(t.Context(), store, nil)

	req := &Request{
		Subject:  "test-user",
		Groups:   []string{"test-group"},
		Resource: "test/resource",
		Action:   "read",
		Tags:     map[string]any{},
	}

	// First evaluation
	decision1 := eval.Evaluate(req)
	if decision1 != DecisionAllow {
		t.Errorf("expected allow, got %v", decision1)
	}

	// Second evaluation should hit cache
	decision2 := eval.Evaluate(req)
	if decision2 != DecisionAllow {
		t.Errorf("expected allow from cache, got %v", decision2)
	}

	// Clear cache and re-evaluate
	eval.ClearCache()
	decision3 := eval.Evaluate(req)
	if decision3 != DecisionAllow {
		t.Errorf("expected allow after cache clear, got %v", decision3)
	}
}

func TestEvaluatorWithNoPolicy(t *testing.T) {
	// Create evaluator with nil policy
	store := NewMemoryPolicyStore()
	store.UpdatePolicy(nil)
	eval := NewEvaluator(t.Context(), store, nil)

	req := &Request{
		Subject:  "test-user",
		Groups:   []string{"test-group"},
		Resource: "test/resource",
		Action:   "read",
		Tags:     map[string]any{},
	}

	decision := eval.Evaluate(req)
	if decision != DecisionDeny {
		t.Errorf("expected deny with no policy, got %v", decision)
	}
}

func TestEvaluatorScopes(t *testing.T) {
	for _, tc := range []struct {
		name, scope, instance, subject, owner string
		want                                  Decision
	}{
		{"exact", "alice-blog", "alice-blog", "hank", "alice", DecisionAllow},
		{"other app", "alice-blog", "bob-blog", "hank", "bob", DecisionDeny},
		{"whole name", "alice-blog", "alice-blog-prod", "hank", "alice", DecisionDeny},
		{"glob", "api-*-prod", "api-west-prod", "alice", "bob", DecisionAllow},
		{"glob suffix", "api-*-prod", "api-west-dev", "alice", "bob", DecisionDeny},
		{"glob overlap", "api-*api", "api", "alice", "bob", DecisionDeny},
		{"glob question", "api-?", "api-a", "alice", "bob", DecisionAllow},
		{"glob class", "api-[ab]", "api-c", "alice", "bob", DecisionDeny},
		{"invalid glob", "api-[", "api-[", "alice", "bob", DecisionDeny},
		{"unknown special", "$future", "$future", "alice", "alice", DecisionDeny},
		{"owner", "$owner", "alice-blog", "alice", "alice", DecisionAllow},
		{"other owner", "$owner", "bob-blog", "alice", "bob", DecisionDeny},
		{"missing owner", "$owner", "legacy-app", "alice", "", DecisionDeny},
		{"missing subject", "$owner", "alice-blog", "", "alice", DecisionDeny},
		{"both identities missing", "$owner", "legacy-app", "", "", DecisionDeny},
		{"owner without instance", "$owner", "", "alice", "alice", DecisionDeny},
		{"exact without instance", "alice-blog", "", "alice", "alice", DecisionDeny},
		{"glob without instance", "api-*", "", "alice", "alice", DecisionDeny},
		{"wildcard without instance", "*", "", "alice", "", DecisionAllow},
		{"legacy without instance", "", "", "alice", "", DecisionAllow},
		{"legacy other owner", "", "bob-blog", "alice", "bob", DecisionAllow},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := NewMemoryPolicyStore()
			store.UpdatePolicy(&Policy{Rules: []Rule{{Groups: []string{"members"}, Permissions: []Permission{{Resource: "app", Actions: []string{"*"}, Scope: tc.scope}}}}})
			eval := NewEvaluator(t.Context(), store, nil)
			for _, action := range []string{"read", "create", "list"} {
				req := &Request{Subject: tc.subject, Groups: []string{"members"}, Resource: "app", Action: action, Instance: tc.instance, Owner: tc.owner}
				if got := eval.Evaluate(req); got != tc.want {
					t.Errorf("%s: got %v, want %v", action, got, tc.want)
				}
			}
		})
	}
}

func TestEvaluatorScopedCacheIsolation(t *testing.T) {
	store := NewMemoryPolicyStore()
	store.UpdatePolicy(&Policy{Rules: []Rule{{Groups: []string{"members"}, Permissions: []Permission{
		{Resource: "app", Actions: []string{"*"}, Scope: "$owner"},
		{Resource: "app", Actions: []string{"list", "deploy"}, Scope: "shared-blog"},
		{Resource: "app", Actions: []string{"create"}},
	}}}})
	eval := NewEvaluator(t.Context(), store, nil)
	for _, tc := range []struct {
		name, subject, instance, owner, action, group string
		want                                          Decision
	}{
		{"owner primes cache", "alice", "alice-blog", "alice", "deploy", "members", DecisionAllow},
		{"other app", "alice", "bob-blog", "bob", "deploy", "members", DecisionDeny},
		{"ownership transferred", "alice", "alice-blog", "bob", "deploy", "members", DecisionDeny},
		{"ownership removed", "alice", "alice-blog", "", "deploy", "members", DecisionDeny},
		{"new owner", "bob", "alice-blog", "bob", "deploy", "members", DecisionAllow},
		{"other subject", "hank", "alice-blog", "bob", "deploy", "members", DecisionDeny},
		{"collaborator", "hank", "shared-blog", "alice", "deploy", "members", DecisionAllow},
		{"collaborator other instance same owner", "hank", "private-blog", "alice", "deploy", "members", DecisionDeny},
		{"collaborator cannot configure", "hank", "shared-blog", "alice", "configure", "members", DecisionDeny},
		{"group still required", "alice", "alice-blog", "alice", "deploy", "outsiders", DecisionDeny},
		{"owner list result", "alice", "alice-blog", "alice", "list", "members", DecisionAllow},
		{"hidden list result", "alice", "bob-blog", "bob", "list", "members", DecisionDeny},
		{"shared list result", "hank", "shared-blog", "alice", "list", "members", DecisionAllow},
		{"no collection bypass", "alice", "", "", "list", "members", DecisionDeny},
		{"explicit create grant", "alice", "", "", "create", "members", DecisionAllow},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := &Request{Subject: tc.subject, Groups: []string{tc.group}, Resource: "app", Action: tc.action, Instance: tc.instance, Owner: tc.owner}
			if got := eval.Evaluate(req); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
	store.UpdatePolicy(&Policy{})
	eval.ClearCache()
	if got := eval.Evaluate(&Request{Subject: "alice", Groups: []string{"members"}, Resource: "app", Action: "deploy", Instance: "alice-blog", Owner: "alice"}); got != DecisionDeny {
		t.Fatalf("revoked owner grant: got %v", got)
	}
}
