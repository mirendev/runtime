package netdb

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNetDB(t *testing.T) {
	t.Run("respects cooldown period for released addresses", func(t *testing.T) {
		r := require.New(t)

		n, err := New(filepath.Join(t.TempDir(), "net.db"))
		r.NoError(err)

		subnet, err := n.Subnet("172.16.8.0/24")
		r.NoError(err)

		// Reserve first IP
		ip1, err := subnet.Reserve()
		r.NoError(err)
		r.Equal("172.16.8.2/24", ip1.String())

		// Release it
		err = subnet.Release(ip1)
		r.NoError(err)

		// Immediate reservation should skip the recently released IP
		ip2, err := subnet.Reserve()
		r.NoError(err)
		r.Equal("172.16.8.3/24", ip2.String(), "should skip recently released IP")

		// Reserve all remaining IPs
		for i := 4; i <= 254; i++ {
			ip, err := subnet.Reserve()
			r.NoError(err)
			r.Equal(fmt.Sprintf("172.16.8.%d/24", i), ip.String())
		}

		// Now that we're out of fresh IPs, we should get the released one
		ip3, err := subnet.Reserve()
		r.NoError(err)
		r.Equal("172.16.8.2/24", ip3.String(), "should reuse released IP when no others available")
	})

	t.Run("respects the cooldown time of an ip", func(t *testing.T) {
		r := require.New(t)

		n, err := New(filepath.Join(t.TempDir(), "net.db"))
		r.NoError(err)

		subnet, err := n.Subnet("172.16.8.0/24")
		r.NoError(err)

		ip, err := subnet.Reserve()
		r.NoError(err)

		r.Equal("172.16.8.2/24", ip.String())

		ip2, err := subnet.Reserve()
		r.NoError(err)

		r.Equal("172.16.8.3/24", ip2.String())

		err = subnet.Release(ip)
		r.NoError(err)

		n.cooldownDur = 0

		ip3, err := subnet.Reserve()
		r.NoError(err)

		r.Equal("172.16.8.2/24", ip3.String())
	})

	t.Run("releases and reservations track timing correctly", func(t *testing.T) {
		r := require.New(t)

		n, err := New(filepath.Join(t.TempDir(), "net.db"))
		r.NoError(err)

		subnet, err := n.Subnet("172.16.8.0/24")
		r.NoError(err)

		// Reserve and release several IPs
		ips := make([]string, 3)
		for i := 0; i < 3; i++ {
			ip, err := subnet.Reserve()
			r.NoError(err)
			ips[i] = ip.String()
			err = subnet.Release(ip)
			r.NoError(err)
			time.Sleep(time.Millisecond) // Ensure different timestamps
		}

		// Verify we get new IPs while they're available
		for i := 3; i < 6; i++ {
			ip, err := subnet.Reserve()
			r.NoError(err)
			r.NotContains(ips, ip.String(), "should not reuse recently released IPs")
		}
	})

	t.Run("can reserve a subnet", func(t *testing.T) {
		r := require.New(t)

		n, err := New(filepath.Join(t.TempDir(), "net.db"))
		r.NoError(err)

		subnet, err := n.Subnet("172.16.0.0/16")
		r.NoError(err)

		sub, err := subnet.ReserveSubnet(24, "a")
		r.NoError(err)

		r.Equal("172.16.0.0/24", sub.Prefix().String())

		sub2, err := subnet.ReserveSubnet(24, "b")
		r.NoError(err)

		r.Equal("172.16.1.0/24", sub2.Prefix().String())

		ip, err := sub2.Reserve()
		r.NoError(err)

		r.Equal("172.16.1.2/24", ip.String())
	})

	t.Run("can reserve an interface", func(t *testing.T) {
		r := require.New(t)

		n, err := New(filepath.Join(t.TempDir(), "net.db"))
		r.NoError(err)

		iface, err := n.ReserveInterface("rt")
		r.NoError(err)

		r.Equal("rt1", iface)

		iface2, err := n.ReserveInterface("rt")
		r.NoError(err)

		r.Equal("rt2", iface2)

		err = n.ReleaseInterface("rt1")
		r.NoError(err)

		iface3, err := n.ReserveInterface("rt")
		r.NoError(err)

		r.Equal("rt1", iface3)
	})
}
