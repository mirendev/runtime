package rbac

// PolicyProvider is an interface for providing RBAC policies
type PolicyProvider interface {
	// GetPolicy returns the current policy
	GetPolicy() *Policy
}
