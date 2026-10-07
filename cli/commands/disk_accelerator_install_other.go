//go:build !linux

package commands

import (
	"fmt"

	"miren.dev/runtime/clientconfig"
	"miren.dev/runtime/pkg/runnerconfig"
)

func currentDiskAcceleratorNode(_ *Context, _, _, _ string) (string, *runnerconfig.Config, string, error) {
	return "", nil, "", fmt.Errorf("inferring the local node is only available on Linux; pass a node name or ID")
}

func localDiskAcceleratorCluster(_ *Context, _, _ string) (*clientconfig.ClusterConfig, error) {
	return nil, fmt.Errorf("inferring the local server is only available on Linux; pass a node name or ID")
}
