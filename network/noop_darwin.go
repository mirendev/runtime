//go:build !linux
// +build !linux

package network

import (
	"fmt"
	"log/slog"

	"github.com/vishvananda/netlink"
)

func ConfigureNetNS(log *slog.Logger, pid int, ec *EndpointConfig) error {
	return fmt.Errorf("network namespace not supported on this platform")
}

func SetupBridge(n *BridgeConfig) (*netlink.Bridge, error) {
	return nil, fmt.Errorf("network bridge not supported on this platform")
}

func TeardownBridge(name string) error {
	return fmt.Errorf("network bridge not supported on this platform")
}
