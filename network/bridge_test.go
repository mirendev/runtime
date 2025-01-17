package network

import (
	"cmp"
	"fmt"
	"net/netip"
	"slices"
	"testing"
	"time"

	"github.com/coreos/go-iptables/iptables"
	"github.com/stretchr/testify/require"
	"github.com/vishvananda/netlink"
)

func noTestBridge(t *testing.T) {
	t.Run("can setup a bridge for use", func(t *testing.T) {
		r := require.New(t)

		ts := time.Now()

		name := fmt.Sprintf("t%d%d", ts.UnixMilli(), ts.UnixNano())[:15]

		addrs := []netip.Prefix{
			netip.MustParsePrefix("192.168.56.1/24"),
			netip.MustParsePrefix("fd99::a:b:c/64"),
		}

		defer func() {
			li, _ := netlink.LinkByName(name)
			if li != nil {
				netlink.LinkDel(li)
			}
		}()

		_, err := SetupBridge(&BridgeConfig{
			Name:      name,
			Addresses: addrs,
		})
		r.NoError(err)

		br, err := netlink.LinkByName(name)
		r.NoError(err)

		v4addrs, err := netlink.AddrList(br, netlink.FAMILY_V4)
		r.NoError(err)

		r.Len(v4addrs, 1)

		r.Equal("192.168.56.1/24", v4addrs[0].IPNet.String())

		v6addrs, err := netlink.AddrList(br, netlink.FAMILY_V6)
		r.NoError(err)

		r.Len(v6addrs, 2)
		slices.SortFunc(v6addrs, func(a, b netlink.Addr) int {
			return cmp.Compare(a.String(), b.String())
		})

		r.Equal("fd99::a:b:c/64", v6addrs[0].IPNet.String())

		tbl, err := iptables.NewWithProtocol(iptables.ProtocolIPv4)
		r.NoError(err)

		rules, err := tbl.List("nat", name+"-EXT")
		r.NoError(err)

		r.Contains(rules, fmt.Sprintf(
			"-A %s ! -d 224.0.0.0/4 -m comment --comment %s -j MASQUERADE",
			name+"-EXT", name))

		tbl, err = iptables.NewWithProtocol(iptables.ProtocolIPv6)
		r.NoError(err)

		rules, err = tbl.List("nat", name+"-EXT")
		r.NoError(err)

		r.Contains(rules, fmt.Sprintf(
			"-A %s ! -d ff00::/8 -m comment --comment %s -j MASQUERADE",
			name+"-EXT", name))
	})
}
