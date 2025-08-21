package rbac

import (
	"fmt"
	"strings"
)

// Policy represents the RBAC policy document from miren.cloud
type Policy struct {
	Rules       []Rule `json:"rules"`
	ClusterID   string `json:"cluster_id,omitempty"`
	ClusterName string `json:"cluster_name,omitempty"`
}

// TagExpression represents a single tag matching expression
type TagExpression struct {
	Tag      string `json:"tag"`
	Value    any    `json:"value"`    // Can be a string, number, boolean, or array
	Operator string `json:"operator"` // "equals", "not_equals", "in", "not_in", "exists", "not_exists"
}

// TagSelector defines tag matching criteria using expressions
type TagSelector struct {
	Expressions []TagExpression `json:"expressions"`
}

// Rule represents a single RBAC rule from miren.cloud
type Rule struct {
	ID          string       `json:"id"`
	Name        string       `json:"name"`
	Description string       `json:"description,omitempty"`
	TagSelector TagSelector  `json:"tag_selector"`
	Groups      []string     `json:"groups"`
	Permissions []Permission `json:"permissions"`
	CreatedAt   string       `json:"created_at"`
	UpdatedAt   string       `json:"updated_at"`
}

// Permission represents a permission in a rule
type Permission struct {
	Resource string   `json:"resource"`
	Actions  []string `json:"actions"`
}

// Request represents an authorization request with tags
type Request struct {
	Subject  string         // Subject identifier (from JWT)
	Groups   []string       // Group IDs the subject belongs to
	Resource string         // Resource being accessed
	Action   string         // Action being performed
	Tags     map[string]any // Tags to match against rule selectors
	Context  map[string]any // Additional context
}

// MatchesTags checks if a rule's tag selector matches the provided tags
func (r *Rule) MatchesTags(tags map[string]any) bool {
	// If no expressions, rule matches all
	if len(r.TagSelector.Expressions) == 0 {
		return true
	}

	// All expressions must evaluate to true
	for _, expr := range r.TagSelector.Expressions {
		if !evaluateExpression(expr, tags) {
			return false
		}
	}

	return true
}

// evaluateExpression evaluates a single tag expression against the provided tags
func evaluateExpression(expr TagExpression, tags map[string]any) bool {
	tagValue, exists := tags[expr.Tag]

	switch expr.Operator {
	case "equals":
		if !exists {
			return false
		}
		return valuesEqual(expr.Value, tagValue)

	case "not_equals":
		if !exists {
			return true // Tag doesn't exist, so it's not equal to the value
		}
		return !valuesEqual(expr.Value, tagValue)

	case "exists":
		return exists

	case "not_exists":
		return !exists

	case "in":
		if !exists {
			return false
		}
		values := convertToArray(expr.Value)
		for _, v := range values {
			if valuesEqual(v, tagValue) {
				return true
			}
		}
		return false

	case "not_in":
		if !exists {
			return true // Tag doesn't exist, so it's not in the list
		}
		values := convertToArray(expr.Value)
		for _, v := range values {
			if valuesEqual(v, tagValue) {
				return false
			}
		}
		return true

	default:
		// Unknown operator, default to false for safety
		return false
	}
}

// convertToArray converts an any to an array
func convertToArray(value any) []any {
	if value == nil {
		return []any{}
	}

	// If it's already a slice, convert it
	switch v := value.(type) {
	case []any:
		return v
	case []string:
		result := make([]any, len(v))
		for i, s := range v {
			result[i] = s
		}
		return result
	case string:
		// If it's a string with commas, split it (for backwards compatibility)
		if strings.Contains(v, ",") {
			parts := strings.Split(v, ",")
			result := make([]any, len(parts))
			for i, part := range parts {
				result[i] = strings.TrimSpace(part)
			}
			return result
		}
		return []any{v}
	default:
		// Single value, wrap it in an array
		return []any{value}
	}
}

// HasPermission checks if the rule grants permission for a resource and action
func (r *Rule) HasPermission(resource, action string) bool {
	for _, perm := range r.Permissions {
		if matchResource(perm.Resource, resource) {
			for _, permAction := range perm.Actions {
				if matchAction(permAction, action) {
					return true
				}
			}
		}
	}
	return false
}

// AppliesTo checks if the rule applies to given groups
func (r *Rule) AppliesTo(groups []string) bool {
	// Check if any of the user's groups match the rule's groups
	for _, ruleGroup := range r.Groups {
		for _, userGroup := range groups {
			if ruleGroup == userGroup {
				return true
			}
		}
	}
	return false
}

// valuesEqual compares two values from tag selectors
func valuesEqual(expected, actual any) bool {
	// Direct equality
	if expected == actual {
		return true
	}

	// Try string comparison
	expectedStr := fmt.Sprintf("%v", expected)
	actualStr := fmt.Sprintf("%v", actual)
	return expectedStr == actualStr
}

// matchResource checks if a resource matches a pattern (with wildcard support)
func matchResource(pattern, resource string) bool {
	// Exact match
	if pattern == resource {
		return true
	}

	// Wildcard matching
	if strings.Contains(pattern, "*") {
		// Convert pattern to a simple glob matcher
		if pattern == "*" {
			return true
		}

		// Handle patterns like "apps/*" or "*/config"
		parts := strings.Split(pattern, "*")
		if len(parts) == 2 {
			prefix := parts[0]
			suffix := parts[1]
			return strings.HasPrefix(resource, prefix) && strings.HasSuffix(resource, suffix)
		}

		// TODO handle more complex patterns. There are some careful cases we need to
		// consider so that we're sure that we're matching the whole input and not
		// a portion (which is always invalid)

		return false
	}

	return false
}

// matchAction checks if an action matches a pattern
func matchAction(pattern, action string) bool {
	// Exact match or wildcard
	return pattern == action || pattern == "*"
}
