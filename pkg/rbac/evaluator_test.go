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
	eval := NewEvaluator(store, nil)

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
				Tags: map[string]interface{}{
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
				Tags: map[string]interface{}{
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
				Tags: map[string]interface{}{
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
				Tags: map[string]interface{}{
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
				Tags: map[string]interface{}{
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
				Tags: map[string]interface{}{
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
				Tags: map[string]interface{}{
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
		tags        map[string]interface{}
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
			tags: map[string]interface{}{
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
			tags: map[string]interface{}{
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
			tags: map[string]interface{}{
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
			tags: map[string]interface{}{
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
			tags: map[string]interface{}{
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
			tags: map[string]interface{}{
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
			tags: map[string]interface{}{
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
			tags:        map[string]interface{}{},
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
			tags: map[string]interface{}{
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
			tags: map[string]interface{}{
				"env": "dev",
			},
			shouldMatch: false,
		},
		{
			name: "empty selector matches all",
			rule: Rule{
				TagSelector: TagSelector{},
			},
			tags: map[string]interface{}{
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
			tags: map[string]interface{}{
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
						{Tag: "port", Value: []interface{}{80, 443, 8080}, Operator: "in"},
					},
				},
			},
			tags: map[string]interface{}{
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
	eval := NewEvaluator(store, nil)

	req := &Request{
		Subject:  "test-user",
		Groups:   []string{"test-group"},
		Resource: "test/resource",
		Action:   "read",
		Tags:     map[string]interface{}{},
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
	eval := NewEvaluator(store, nil)

	req := &Request{
		Subject:  "test-user",
		Groups:   []string{"test-group"},
		Resource: "test/resource",
		Action:   "read",
		Tags:     map[string]interface{}{},
	}

	decision := eval.Evaluate(req)
	if decision != DecisionDeny {
		t.Errorf("expected deny with no policy, got %v", decision)
	}
}

