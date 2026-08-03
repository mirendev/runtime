package sandbox

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/pkg/entity"
)

func sandboxIDs(entries []tokenEntry) []string {
	ids := make([]string, 0, len(entries))
	for _, e := range entries {
		ids = append(ids, e.sandboxID)
	}
	return ids
}

func TestTokenRefresher_RegisterUnregister(t *testing.T) {
	tr := newTokenRefresher()

	assert.Empty(t, tr.snapshot())

	tr.register("sandbox/one", "/tmp/one/identity-token", "myapp")
	tr.register("sandbox/two", "/tmp/two/identity-token", "otherapp")

	entries := tr.snapshot()
	require.Len(t, entries, 2)
	assert.ElementsMatch(t, []string{"sandbox/one", "sandbox/two"}, sandboxIDs(entries))

	tr.unregister("sandbox/one")

	entries = tr.snapshot()
	require.Len(t, entries, 1)
	assert.Equal(t, "sandbox/two", entries[0].sandboxID)
	assert.Equal(t, "/tmp/two/identity-token", entries[0].filePath)
	assert.Equal(t, "otherapp", entries[0].appName)

	// Unregistering an unknown sandbox is a no-op.
	tr.unregister("sandbox/never-seen")
	assert.Len(t, tr.snapshot(), 1)
}

// releaseTokenState is what teardown calls, so it has to clear both registries —
// leaving the secret behind would keep a dead sandbox's token requests authorized.
func TestReleaseTokenState_ClearsBothRegistries(t *testing.T) {
	c := newTestTokenController(t)
	c.tokenRefresher = newTokenRefresher()

	id := entity.Id(testSandboxID)
	c.tokenRefresher.register(testSandboxID, "/tmp/identity-token", "myapp")
	require.Len(t, c.tokenRefresher.snapshot(), 1)
	require.True(t, c.tokenSecrets.verify(testSandboxID, testSecret))

	c.ReleaseTokenState(id)

	assert.Empty(t, c.tokenRefresher.snapshot())
	assert.False(t, c.tokenSecrets.verify(testSandboxID, testSecret))
}

func TestReleaseTokenState_NilRegistries(t *testing.T) {
	c := newTestTokenController(t)
	c.tokenRefresher = nil
	c.tokenSecrets = nil

	assert.NotPanics(t, func() {
		c.ReleaseTokenState(entity.Id(testSandboxID))
	})
}

func TestRefreshTokens_RewritesLiveTokens(t *testing.T) {
	c := newTestTokenController(t)
	c.tokenRefresher = newTokenRefresher()

	tokenPath := filepath.Join(t.TempDir(), "identity-token")
	require.NoError(t, os.WriteFile(tokenPath, []byte("stale"), 0644))

	c.tokenRefresher.register(testSandboxID, tokenPath, "myapp")
	c.refreshTokens()

	data, err := os.ReadFile(tokenPath)
	require.NoError(t, err)
	assert.NotEqual(t, "stale", string(data))
	assert.NotEmpty(t, data)

	// A live sandbox stays registered for the next tick.
	assert.Len(t, c.tokenRefresher.snapshot(), 1)
}

// A sandbox whose directory has been torn down must drop out of the refresh set
// instead of failing to write a fresh token on every tick, forever.
func TestRefreshTokens_DropsDepartedSandboxes(t *testing.T) {
	c := newTestTokenController(t)
	c.tokenRefresher = newTokenRefresher()

	liveDir := t.TempDir()
	livePath := filepath.Join(liveDir, "identity-token")
	require.NoError(t, os.WriteFile(livePath, []byte("stale"), 0644))

	// Same shape as a sandbox whose dir StopSandbox already removed.
	goneDir := filepath.Join(t.TempDir(), "removed-sandbox")
	gonePath := filepath.Join(goneDir, "identity-token")

	c.tokenRefresher.register("sandbox/live", livePath, "myapp")
	c.tokenRefresher.register("sandbox/gone", gonePath, "deadapp")

	c.refreshTokens()

	entries := c.tokenRefresher.snapshot()
	require.Len(t, entries, 1)
	assert.Equal(t, "sandbox/live", entries[0].sandboxID)

	data, err := os.ReadFile(livePath)
	require.NoError(t, err)
	assert.NotEqual(t, "stale", string(data))
}
