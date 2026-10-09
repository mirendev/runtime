//go:build linux

package ipdiscovery

import "github.com/vishvananda/netlink"

// linkTypeOf asks netlink what kind of link name is. Failure is not worth
// reporting: the type only sharpens classification, and an empty answer
// falls back to the address range alone.
func linkTypeOf(name string) string {
	link, err := netlink.LinkByName(name)
	if err != nil {
		return ""
	}
	return link.Type()
}
