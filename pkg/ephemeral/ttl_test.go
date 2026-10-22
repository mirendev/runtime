package ephemeral

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseTTL(t *testing.T) {
	cases := []struct {
		in   string
		want time.Duration
	}{
		{"7d", 7 * 24 * time.Hour},
		{"2w", 14 * 24 * time.Hour},
		{"48h", 48 * time.Hour},
		{"90m", 90 * time.Minute},
		{"1h30m", 90 * time.Minute},
	}
	for _, c := range cases {
		got, err := ParseTTL(c.in)
		require.NoError(t, err, c.in)
		assert.Equal(t, c.want, got, c.in)
	}

	for _, bad := range []string{"", "0", "0d", "-1d", "-5m", "soon", "7x"} {
		_, err := ParseTTL(bad)
		assert.Error(t, err, bad)
	}
}

func TestDefaultTTLParses(t *testing.T) {
	d, err := ParseTTL(DefaultTTL)
	require.NoError(t, err)
	assert.Equal(t, 7*24*time.Hour, d)
}

func TestWireTTL(t *testing.T) {
	assert.Equal(t, "168h", WireTTL(7*24*time.Hour))
	assert.Equal(t, "48h", WireTTL(48*time.Hour))
	assert.Equal(t, "1h30m0s", WireTTL(90*time.Minute))

	// Whatever WireTTL produces has to survive a server that only knows
	// time.ParseDuration.
	for _, d := range []time.Duration{7 * 24 * time.Hour, 90 * time.Minute, 36 * time.Hour} {
		back, err := time.ParseDuration(WireTTL(d))
		require.NoError(t, err)
		assert.Equal(t, d, back)
	}
}
