package serverinfo

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSourceReadyFlipsOnce(t *testing.T) {
	s := New()
	require.NotEmpty(t, s.InstanceID())
	require.False(t, s.Info().Ready)
	require.False(t, s.Info().StartedAt.IsZero())

	s.MarkReady()
	require.True(t, s.Info().Ready)
	require.Equal(t, s.InstanceID(), s.Info().InstanceID)
}

func TestNewMintsDistinctInstances(t *testing.T) {
	require.NotEqual(t, New().InstanceID(), New().InstanceID())
}
