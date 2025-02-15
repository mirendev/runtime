//go:build linux
// +build linux

package network

import (
	"log/slog"

	"github.com/containernetworking/plugins/pkg/ns"
)

func ConfigureNetNS(log *slog.Logger, pid int, ec *EndpointConfig) error {
	path := netnsPath(int(pid))

	netns, err := ns.GetNS(path)
	if err != nil {
		return err
	}

	log.Debug("configuring netns", "path", path, "pid", pid)

	br, err := SetupBridge(ec.Bridge)
	if err != nil {
		return err
	}

	hostInterface, containerInterface, err := SetupVeth(
		netns, br, "eth0", 0, true, 0, "",
	)
	if err != nil {
		return err
	}

	log.Info("configured veth", "host-iface", hostInterface.Name, "cont-iface", containerInterface.Name)

	if err := netns.Do(func(_ ns.NetNS) error {
		// Add the IP to the interface
		if err := ConfigureIface(log, "eth0", ec); err != nil {
			return err
		}
		return nil
	}); err != nil {
		return err
	}

	err = CheckBridgeStatus(hostInterface.Name)
	if err != nil {
		return err
	}

	// Refetch the bridge to get all updated attributes
	br, err = BridgeByName(ec.Bridge.Name)
	if err != nil {
		return err
	}

	err = ConfigureGW(br, ec)
	if err != nil {
		return err
	}

	err = MasqueradeEndpoint(ec)
	if err != nil {
		return err
	}

	return nil
}
