//go:build linux

package sandbox

import (
	"fmt"
	"os/exec"
	"strconv"

	"github.com/containernetworking/plugins/pkg/utils/sysctl"
	"github.com/davecgh/go-spew/spew"
	compute "miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/network"
)

func (c *SandboxController) configureFirewall(sb *compute.Sandbox, ep *network.EndpointConfig) error {
	for _, co := range sb.Container {
		c.Log.Info("configuring firewall", "sandbox", sb.ID.String(), "ports", len(co.Port))

		for _, p := range co.Port {
			if err := c.configurePort(p, ep); err != nil {
				return err
			}
		}
	}

	return nil
}

func (c *SandboxController) configurePort(p compute.Port, ep *network.EndpointConfig) error {
	// Configure the firewall to forward traffic on node port to port

	if p.NodePort != 0 {

		c.Log.Info("configuring firewall", "port", p.NodePort, "targetPort", p.Port)

		addr := ep.Addresses[0]

		sysctl.Sysctl("net.ipv4.conf.all.route_localnet", "1")

		exe := exec.Command("iptables",
			"-t", "nat",
			"-A", "PREROUTING",
			"!", "-i", c.Bridge,
			"-p", "tcp",
			"-m", "tcp",
			"--dport", strconv.Itoa(int(p.NodePort)),
			"-j", "DNAT", "--to-destination", fmt.Sprintf("%s:%d", addr.Addr(), p.Port),
		)

		if output, err := exe.CombinedOutput(); err != nil {
			spew.Dump(string(output))
			return fmt.Errorf("failed to configure port %d: %w", p.NodePort, err)
		}

		exe = exec.Command("iptables",
			"-t", "nat",
			"-A", "OUTPUT",
			"-p", "tcp",
			"-m", "tcp",
			"-d", "127.0.0.1",
			"--dport", strconv.Itoa(int(p.NodePort)),
			"-j", "DNAT", "--to-destination", fmt.Sprintf("%s:%d", addr.Addr(), p.Port),
		)

		if output, err := exe.CombinedOutput(); err != nil {
			spew.Dump(string(output))
			return fmt.Errorf("failed to configure port %d: %w", p.NodePort, err)
		}

		exe = exec.Command("iptables",
			"-t", "nat",
			"-A", "POSTROUTING",
			"-s", "127.0.0.1",
			"-p", "tcp",
			"-d", addr.Addr().String(),
			"--dport", strconv.Itoa(int(p.Port)),
			"-j", "MASQUERADE",
		)

		if output, err := exe.CombinedOutput(); err != nil {
			spew.Dump(string(output))
			return fmt.Errorf("failed to configure port %d: %w", p.NodePort, err)
		}
	}

	return nil
}
