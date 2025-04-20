package ipalloc

import (
	"net/netip"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAllocator_random(t *testing.T) {
	r := require.New(t)

	t.Run("ipv4", func(t *testing.T) {
		prefix := netip.MustParsePrefix("10.10.0.0/16")

		seen := map[string]struct{}{}

		rr := newRandReader()

		for range 1000 {
			ra, err := generateRandomIPInSubnet(rr, prefix)
			r.NoError(err)

			seen[ra.String()] = struct{}{}

			r.True(prefix.Contains(ra))
		}

		r.Greater(len(seen), 800)
	})

	t.Run("ipv6", func(t *testing.T) {
		prefix := netip.MustParsePrefix("fdaa::/64")

		seen := map[string]struct{}{}

		rr := newRandReader()

		for range 1000 {
			ra, err := generateRandomIPInSubnet(rr, prefix)
			r.NoError(err)

			seen[ra.String()] = struct{}{}

			r.True(prefix.Contains(ra))
		}

		r.Greater(len(seen), 900)
	})
}
