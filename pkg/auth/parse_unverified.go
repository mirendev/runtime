package auth

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
)

// ExtendedClaims represents the full JWT claims from miren.cloud including custom fields
type ExtendedClaims struct {
	Claims
	UserID   string   `json:"user_id,omitempty"`
	UserName string   `json:"name,omitempty"`
	Groups   []string `json:"groups,omitempty"`
}

// ParseUnverifiedClaims parses JWT claims without verification
// This is only for client-side display purposes and should NOT be used for authentication
func ParseUnverifiedClaims(token string) (*ExtendedClaims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid token format")
	}

	// Decode the claims part (second segment)
	claimsData, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("failed to decode claims: %w", err)
	}

	// First unmarshal into a map to see all fields
	var rawClaims map[string]interface{}
	if err := json.Unmarshal(claimsData, &rawClaims); err != nil {
		return nil, fmt.Errorf("failed to parse raw claims: %w", err)
	}

	// Then unmarshal into our extended claims structure
	var claims ExtendedClaims
	if err := json.Unmarshal(claimsData, &claims); err != nil {
		return nil, fmt.Errorf("failed to parse claims: %w", err)
	}
	
	// Map additional fields that might have different names
	if orgID, ok := rawClaims["organization_id"].(string); ok {
		claims.OrganizationID = orgID
	}
	
	// The subject field is the user ID in miren JWTs
	if claims.Subject != "" && claims.UserID == "" {
		claims.UserID = claims.Subject
	}

	return &claims, nil
}