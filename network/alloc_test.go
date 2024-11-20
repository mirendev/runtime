package network

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAllocation(t *testing.T) {
	t.Run("can allocate and deallocate address from a pool", func(t *testing.T) {
		r := require.New(t)

		var pool IPPool

		err := pool.Init("172.16.8.0/24", true)
		r.NoError(err)

		r.Equal("172.16.8.1", pool.Router().String())

		addr, err := pool.Allocate()
		r.NoError(err)

		r.Equal("172.16.8.2/24", addr.String())
	})

	t.Run("errors out when there are no more addresses", func(t *testing.T) {
		r := require.New(t)

		var pool IPPool

		err := pool.Init("172.16.8.0/30", true)
		r.NoError(err)

		r.Equal("172.16.8.1", pool.Router().String())

		addr, err := pool.Allocate()
		r.NoError(err)

		r.Equal("172.16.8.2/30", addr.String())

		_, err = pool.Allocate()
		r.Error(err)
	})

	t.Run("addresses can be deallocated", func(t *testing.T) {
		r := require.New(t)

		var pool IPPool

		err := pool.Init("172.16.8.0/30", true)
		r.NoError(err)

		r.Equal("172.16.8.1", pool.Router().String())

		addr, err := pool.Allocate()
		r.NoError(err)

		r.Equal("172.16.8.2/30", addr.String())

		err = pool.Deallocate(addr)
		r.NoError(err)

		addr2, err := pool.Allocate()
		r.NoError(err)

		r.Equal(addr, addr2)

		_, err = pool.Allocate()
		r.Error(err)
	})

	t.Run("can save and restore the pool state", func(t *testing.T) {
		r := require.New(t)

		var pool IPPool

		err := pool.Init("172.16.8.0/24", true)
		r.NoError(err)

		r.Equal("172.16.8.1", pool.Router().String())

		addr, err := pool.Allocate()
		r.NoError(err)

		data, err := pool.MarshalBinary()
		r.NoError(err)

		var pool2 IPPool

		err = pool2.UnmarshalBinary(data)
		r.NoError(err)

		r.Equal(pool, pool2)

		p3, err := pool2.MarshalBinary()
		r.NoError(err)

		r.Equal(data, p3)

		addr2, err := pool2.Allocate()
		r.NoError(err)

		r.Equal(addr.Addr().Next(), addr2.Addr())
	})

	t.Run("handles ipv6", func(t *testing.T) {
		r := require.New(t)

		var pool IPPool

		err := pool.Init("fd01::/126", true)
		r.NoError(err)

		addr, err := pool.Allocate()
		r.NoError(err)

		r.Equal("fd01::2/126", addr.String())

		addr, err = pool.Allocate()
		r.NoError(err)

		r.Equal("fd01::3/126", addr.String())

		_, err = pool.Allocate()
		r.Error(err)
	})
}
