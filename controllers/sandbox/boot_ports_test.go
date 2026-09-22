package sandbox

import (
	"testing"

	"github.com/stretchr/testify/require"

	compute "miren.dev/runtime/api/compute/compute_v1alpha"
)

func TestTCPPortsToWait(t *testing.T) {
	tcp := compute.SandboxSpecContainerPortTCP
	udp := compute.SandboxSpecContainerPortUDP

	t.Run("keeps tcp ports", func(t *testing.T) {
		r := require.New(t)

		ports, wait := tcpPortsToWait("web", []compute.SandboxSpecContainerPort{
			{Port: 7880, Protocol: tcp},
			{Port: 7881, Protocol: tcp},
		})

		r.Equal([]int{7880, 7881}, ports)
		r.Equal([]WaitPort{{ID: "web", Port: 7880}, {ID: "web", Port: 7881}}, wait)
	})

	t.Run("treats an unset protocol as tcp", func(t *testing.T) {
		r := require.New(t)

		ports, wait := tcpPortsToWait("web", []compute.SandboxSpecContainerPort{
			{Port: 8080},
		})

		r.Equal([]int{8080}, ports)
		r.Equal([]WaitPort{{ID: "web", Port: 8080}}, wait)
	})

	t.Run("skips udp ports so readiness never waits on a socket it cannot see", func(t *testing.T) {
		r := require.New(t)

		ports, wait := tcpPortsToWait("web", []compute.SandboxSpecContainerPort{
			{Port: 7880, Protocol: tcp},
			{Port: 7882, Protocol: udp},
			{Port: 7881, Protocol: tcp},
		})

		r.Equal([]int{7880, 7881}, ports)
		r.Equal([]WaitPort{{ID: "web", Port: 7880}, {ID: "web", Port: 7881}}, wait)
	})

	t.Run("udp-only container has nothing to wait on", func(t *testing.T) {
		r := require.New(t)

		ports, wait := tcpPortsToWait("dns", []compute.SandboxSpecContainerPort{
			{Port: 53, Protocol: udp},
		})

		r.Empty(ports)
		r.Empty(wait)
	})
}
