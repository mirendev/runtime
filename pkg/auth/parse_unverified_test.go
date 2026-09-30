package auth

import (
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseUnverifiedClaimsProfile(t *testing.T) {
	payload := base64.RawURLEncoding.EncodeToString([]byte(
		`{"sub":"usr-ada","organization_id":"org-1","email":"ada@example.com","name":"Ada Lovelace"}`))
	claims, err := ParseUnverifiedClaims("e30." + payload + ".sig")
	require.NoError(t, err)
	assert.Equal(t, "usr-ada", claims.Subject)
	assert.Equal(t, "ada@example.com", claims.Email)
	assert.Equal(t, "Ada Lovelace", claims.Name)
}
