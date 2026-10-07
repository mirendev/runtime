package auth

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
)

// ParseUnverifiedClaims parses JWT claims without verification
// This is only for client-side display purposes and should NOT be used for authentication
func ParseUnverifiedClaims(token string) (*Claims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid token format")
	}

	// Decode the claims part (second segment)
	claimsData, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("failed to decode claims: %w", err)
	}

	var claims Claims
	if err := json.Unmarshal(claimsData, &claims); err != nil {
		return nil, fmt.Errorf("failed to parse claims: %w", err)
	}

	return &claims, nil
}
