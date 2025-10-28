package auth

import (
	"context"
)

type contextKey struct{}

var claimsContextKey = contextKey{}

// WithClaims adds JWT claims to the context
func WithClaims(ctx context.Context, claims *Claims) context.Context {
	return context.WithValue(ctx, claimsContextKey, claims)
}

// GetClaims retrieves JWT claims from the context
func GetClaims(ctx context.Context) (*Claims, bool) {
	claims, ok := ctx.Value(claimsContextKey).(*Claims)
	return claims, ok
}

// GetUserEmail retrieves the user email from context claims
func GetUserEmail(ctx context.Context) string {
	claims, ok := GetClaims(ctx)
	if !ok || claims == nil {
		return ""
	}
	// JWT uses "sub" for subject, which is typically the email in Miren
	return claims.Subject
}

// GetUserGroups retrieves the user's groups from context claims  
func GetUserGroups(ctx context.Context) []string {
	claims, ok := GetClaims(ctx)
	if !ok || claims == nil {
		return nil
	}
	return claims.GroupIDs
}

// GetOrganizationID retrieves the organization ID from context claims
func GetOrganizationID(ctx context.Context) string {
	claims, ok := GetClaims(ctx)
	if !ok || claims == nil {
		return ""
	}
	return claims.OrganizationID
}