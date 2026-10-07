package coordinate

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"miren.dev/runtime/pkg/uplink"
)

func selecting(names ...string) uplink.Session {
	return selectingAs("s1", names...)
}

func selectingAs(id string, names ...string) uplink.Session {
	session := uplink.Session{ID: id}
	for _, name := range names {
		session.Capabilities = append(session.Capabilities, uplink.CapabilitySelection{Name: name, Version: 1})
	}
	return session
}

// The poll goes quiet only while a session carries both of its replacements,
// and comes back when that session ends. A cloud that selects just one, or
// neither, keeps getting the poll: negotiation is the compatibility switch.
func TestPollSuppressedOnlyWhileBothCapabilitiesHold(t *testing.T) {
	c := NewCloudControl(&Foundation{Log: testLogger()}, nil)

	for _, partial := range []uplink.Session{
		selecting(),
		selecting(uplink.CapabilityClusterNetwork),
		selecting(uplink.CapabilityClusterResources),
	} {
		c.suppressPollWhile(context.Background(), partial)
		require.False(t, c.pollSuppressed.Load())
	}

	ctx, cancel := context.WithCancel(context.Background())
	c.suppressPollWhile(ctx, selecting(uplink.CapabilityClusterNetwork, uplink.CapabilityClusterResources))
	require.True(t, c.pollSuppressed.Load())

	cancel()
	require.Eventually(t, func() bool { return !c.pollSuppressed.Load() }, 2e9, 1e7)
}

// On a fast reconnect the new session claims suppression before the old
// session's teardown runs; that teardown must not clear the new claim.
func TestPollSuppressionSurvivesReplacedSessionTeardown(t *testing.T) {
	c := NewCloudControl(&Foundation{Log: testLogger()}, nil)
	both := []string{uplink.CapabilityClusterNetwork, uplink.CapabilityClusterResources}

	oldCtx, endOld := context.WithCancel(context.Background())
	c.suppressPollWhile(oldCtx, selectingAs("old", both...))
	newCtx, endNew := context.WithCancel(context.Background())
	c.suppressPollWhile(newCtx, selectingAs("new", both...))
	require.True(t, c.pollSuppressed.Load())

	endOld()
	// Give the old teardown every chance to run; it must be a no-op.
	require.Never(t, func() bool { return !c.pollSuppressed.Load() }, 200e6, 1e7)

	endNew()
	require.Eventually(t, func() bool { return !c.pollSuppressed.Load() }, 2e9, 1e7)
}
