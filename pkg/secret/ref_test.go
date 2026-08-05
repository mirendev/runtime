package secret

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseRef(t *testing.T) {
	tests := []struct {
		name    string
		ref     string
		path    string
		version string
	}{
		{"floating", "payments/stripe-key", "payments/stripe-key", ""},
		{"pinned", "payments/stripe-key@x1A", "payments/stripe-key", "x1A"},
		{"no separator in a bare name", "token", "token", ""},
		{"surrounding whitespace is trimmed", "  payments/key@x1A  ", "payments/key", "x1A"},
		// External backends address secrets in shapes Miren does not control,
		// so only the last @ delimits the version.
		{"at signs in the path", "secret/data/app@team#signing@42", "secret/data/app@team#signing", "42"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path, version, err := ParseRef(tt.ref)
			require.NoError(t, err)
			assert.Equal(t, tt.path, path)
			assert.Equal(t, tt.version, version)
		})
	}
}

func TestParseRefRejectsMalformed(t *testing.T) {
	for _, ref := range []string{"", "   ", "@x1A", "payments/stripe-key@"} {
		t.Run(ref, func(t *testing.T) {
			_, _, err := ParseRef(ref)
			assert.Error(t, err)
		})
	}
}

func TestFormatRef(t *testing.T) {
	assert.Equal(t, "payments/stripe-key@x1A", FormatRef("payments/stripe-key", "x1A"))
	assert.Equal(t, "payments/stripe-key", FormatRef("payments/stripe-key", ""))
}

// A pinned reference must survive a parse/format round trip unchanged: that
// identity is what makes pinning at ConfigVersion mint idempotent.
func TestFormatRefRoundTrip(t *testing.T) {
	const ref = "payments/stripe-key@x1A"
	path, version, err := ParseRef(ref)
	require.NoError(t, err)
	assert.Equal(t, ref, FormatRef(path, version))
}

func TestIsPinned(t *testing.T) {
	assert.True(t, IsPinned("payments/stripe-key@x1A"))
	assert.False(t, IsPinned("payments/stripe-key"))
	assert.False(t, IsPinned(""))
}
