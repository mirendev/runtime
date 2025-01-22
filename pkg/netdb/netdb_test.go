package netdb

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNetDB(t *testing.T) {
	t.Run("can reserve an address", func(t *testing.T) {
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

		ip3, err := subnet.Reserve()
		r.NoError(err)

		r.Equal("172.16.8.2/24", ip3.String())
	})

	t.Run("can reserve a subnet", func(t *testing.T) {
		r := require.New(t)

		n, err := New(filepath.Join(t.TempDir(), "net.db"))
		r.NoError(err)

		subnet, err := n.Subnet("172.16.0.0/16")
		r.NoError(err)

		sub, err := subnet.ReserveSubnet(24)
		r.NoError(err)

		r.Equal("172.16.0.0/24", sub.Prefix().String())

		sub2, err := subnet.ReserveSubnet(24)
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

		iface, err := n.ReserveInterface("miren")
		r.NoError(err)

		r.Equal("miren1", iface)

		iface2, err := n.ReserveInterface("miren")
		r.NoError(err)

		r.Equal("miren2", iface2)

		err = n.ReleaseInterface("miren1")

		iface3, err := n.ReserveInterface("miren")
		r.NoError(err)

		r.Equal("miren1", iface3)
	})
}
