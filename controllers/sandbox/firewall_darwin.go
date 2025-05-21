//go:build darwin

package sandbox

import (
	compute "miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/network"
)

func (c *SandboxController) configureFirewall(sb *compute.Sandbox, ep *network.EndpointConfig) error {
	return nil
}
