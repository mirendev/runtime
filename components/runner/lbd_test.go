package runner

import (
	"testing"

	containerd "github.com/containerd/containerd/v2/client"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/components/netresolve"
)

func TestNodeStorageLbdDeps(t *testing.T) {
	client := &containerd.Client{}
	resolver, _ := netresolve.NewLocalResolver()
	dataPath := t.TempDir()
	storage, err := NewNodeStorage(&ClusterAccess{}, RunnerDeps{
		CC: client, Resolver: resolver,
	}, RunnerConfig{DataPath: dataPath})
	require.NoError(t, err)

	deps := storage.lbdDeps()
	require.Same(t, client, deps.CC)
	require.Same(t, resolver, deps.Resolver)
	require.Equal(t, dataPath, deps.DataPath)
}
