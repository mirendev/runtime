package secret

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateBackendName(t *testing.T) {
	for _, name := range []string{ClusterBackendName, "prod-vault", "aws_sm", "vault.eu", "A1"} {
		assert.NoError(t, ValidateBackendName(name), name)
	}

	for _, name := range []string{"", "team/a", "/leading", "trailing/", " padded", "padded "} {
		assert.Error(t, ValidateBackendName(name), name)
	}
}

// The ambiguity this guards against: with a slash in the name, format and parse
// disagree about where the backend ends.
func TestSentinelRoundTripIsUnambiguousForValidNames(t *testing.T) {
	const ref = "payments/stripe-key@x1A"

	for _, name := range []string{ClusterBackendName, "prod-vault", "vault.eu"} {
		require.NoError(t, ValidateBackendName(name))

		backend, got, ok := ParseSentinel(FormatSentinel(name, ref))
		require.True(t, ok, name)
		assert.Equal(t, name, backend)
		assert.Equal(t, ref, got)
	}

	// Demonstrates why the name is validated rather than encoded around: this
	// one does not round-trip, which is exactly what ValidateBackendName stops
	// from ever reaching a sentinel.
	backend, got, ok := ParseSentinel(FormatSentinel("team/a", ref))
	require.True(t, ok)
	assert.NotEqual(t, "team/a", backend)
	assert.Equal(t, "team", backend)
	assert.Equal(t, "a/"+ref, got)
	assert.Error(t, ValidateBackendName("team/a"))
}
