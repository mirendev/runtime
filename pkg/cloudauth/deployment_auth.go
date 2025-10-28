package cloudauth

import (
	"context"
	"fmt"

	"miren.dev/runtime/pkg/auth"
	"miren.dev/runtime/pkg/rbac"
)

// DeploymentAuthorizer checks deployment permissions
type DeploymentAuthorizer struct {
	evaluator     *rbac.Evaluator
	policyFetcher *PolicyFetcher
	tags          map[string]any
}

// NewDeploymentAuthorizer creates a new deployment authorizer
func NewDeploymentAuthorizer(evaluator *rbac.Evaluator, policyFetcher *PolicyFetcher, tags map[string]any) *DeploymentAuthorizer {
	return &DeploymentAuthorizer{
		evaluator:     evaluator,
		policyFetcher: policyFetcher,
		tags:          tags,
	}
}

// AuthorizeDeployment checks if the user can deploy the specified app
func (a *DeploymentAuthorizer) AuthorizeDeployment(ctx context.Context, appName string) error {
	// Get claims from context
	claims, ok := auth.GetClaims(ctx)
	if !ok || claims == nil {
		return fmt.Errorf("no authentication claims found")
	}

	// Create RBAC request
	req := &rbac.Request{
		Subject:  claims.Subject,
		Groups:   claims.GroupIDs,
		Resource: fmt.Sprintf("app:%s", appName),
		Action:   "deploy",
		Tags:     a.tags,
		Context: map[string]any{
			"organization_id": claims.OrganizationID,
		},
	}

	// Check permission
	decision := a.evaluator.Evaluate(req)
	if decision == rbac.DecisionDeny {
		// Trigger a refresh of RBAC rules
		if a.policyFetcher != nil {
			a.policyFetcher.RefreshIfNeeded(ctx)
		}
		return fmt.Errorf("permission denied: you don't have permission to deploy %s on this cluster", appName)
	}

	return nil
}

// GetDeploymentUserInfo extracts user info from context for deployment records
func GetDeploymentUserInfo(ctx context.Context) (userID, userEmail string) {
	claims, ok := auth.GetClaims(ctx)
	if !ok || claims == nil {
		// Return placeholder values if no claims
		return "unknown", "unknown@example.com"
	}
	
	// Subject is the user ID, and in Miren it's typically the email
	return claims.Subject, claims.Subject
}