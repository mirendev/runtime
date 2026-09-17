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

func TestSourceReportsComponents(t *testing.T) {
	s := New()
	require.Nil(t, s.Info().Components)

	s.SetComponent("containerd", "v2.0.4")
	s.SetComponent("runc", "")
	s.SetComponent("", "x")
	require.Equal(t, map[string]string{"containerd": "v2.0.4"}, s.Info().Components)

	// Info hands out a copy; a caller editing it does not reach the source.
	s.Info().Components["runc"] = "1.2.2"
	require.Equal(t, map[string]string{"containerd": "v2.0.4"}, s.Info().Components)
}
