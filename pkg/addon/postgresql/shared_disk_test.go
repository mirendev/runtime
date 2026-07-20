package postgresql

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"miren.dev/runtime/api/addon/addon_v1alpha"
)

// resolveSharedDiskName prefers the stored disk name and only falls back to the
// legacy password-derived name for servers provisioned before disk_name was
// tracked.
func TestResolveSharedDiskName(t *testing.T) {
	t.Run("uses stored disk name when set", func(t *testing.T) {
		server := &addon_v1alpha.PostgresServer{
			DiskName:          "pg-shared-data-deadbeef",
			SuperuserPassword: "su-secret",
		}
		assert.Equal(t, "pg-shared-data-deadbeef", resolveSharedDiskName(server))
	})

	t.Run("falls back to legacy password derivation when empty", func(t *testing.T) {
		server := &addon_v1alpha.PostgresServer{
			SuperuserPassword: "su-secret",
		}
		assert.Equal(t, sharedDiskNameForPassword("su-secret"), resolveSharedDiskName(server))
	})
}

// The whole point of the change: once a server records its disk name, rotating
// the superuser password must not move the disk identity out from under the
// existing data.
func TestSharedDiskNameStableAcrossPasswordRotation(t *testing.T) {
	server := &addon_v1alpha.PostgresServer{
		DiskName:          newSharedDiskName(),
		SuperuserPassword: "su-original",
	}
	before := resolveSharedDiskName(server)

	server.SuperuserPassword = "su-rotated"
	after := resolveSharedDiskName(server)

	assert.Equal(t, before, after, "rotating the password must not change the disk name")
	assert.NotEqual(t, sharedDiskNameForPassword("su-rotated"), after,
		"the disk name must not track the (rotated) password")
}

// newSharedDiskName is independent of any password and yields unique,
// correctly-prefixed names per generation.
func TestNewSharedDiskName(t *testing.T) {
	a := newSharedDiskName()
	b := newSharedDiskName()

	require.True(t, strings.HasPrefix(a, sharedDiskName+"-"), "unexpected prefix: %s", a)
	assert.NotEqual(t, a, b, "each generation should get a distinct disk name")
}
