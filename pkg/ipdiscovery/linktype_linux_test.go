//go:build linux

package ipdiscovery

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLinkTypeOf(t *testing.T) {
	// Every Linux network namespace has a loopback, and netlink files it
	// under the generic "device" type like any physical NIC.
	assert.Equal(t, "device", linkTypeOf("lo"))
	assert.Equal(t, "", linkTypeOf("no-such-link0"))
}
