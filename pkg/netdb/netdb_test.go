package netdb

import (
	"fmt"
	"net/netip"
	"path/filepath"
	"sync"
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
		for i := range 3 {
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

	t.Run("reserves a specific address", func(t *testing.T) {
		r := require.New(t)

		n, err := New(filepath.Join(t.TempDir(), "net.db"))
		r.NoError(err)

		subnet, err := n.Subnet("172.16.8.0/24")
		r.NoError(err)

		// Reserve a specific IP
		addr, _ := netip.ParseAddr("172.16.8.50")
		err = subnet.ReserveSpecificAddr(addr)
		r.NoError(err)

		// Normal Reserve should skip the specifically reserved IP
		for range 48 {
			ip, err := subnet.Reserve()
			r.NoError(err)
			r.NotEqual("172.16.8.50/24", ip.String(), "should not allocate specifically reserved IP")
		}

		// Verify .50 is skipped (next Reserve after .49 should be .51)
		ip, err := subnet.Reserve()
		r.NoError(err)
		r.Equal("172.16.8.51/24", ip.String())
	})

	t.Run("re-reserves an already reserved address", func(t *testing.T) {
		r := require.New(t)

		n, err := New(filepath.Join(t.TempDir(), "net.db"))
		r.NoError(err)

		subnet, err := n.Subnet("172.16.8.0/24")
		r.NoError(err)

		// Reserve via normal path
		ip1, err := subnet.Reserve()
		r.NoError(err)
		r.Equal("172.16.8.2/24", ip1.String())

		// Re-reserve the same IP specifically (idempotent)
		addr, _ := netip.ParseAddr("172.16.8.2")
		err = subnet.ReserveSpecificAddr(addr)
		r.NoError(err)

		// Next Reserve should still give .3
		ip2, err := subnet.Reserve()
		r.NoError(err)
		r.Equal("172.16.8.3/24", ip2.String())
	})

	t.Run("re-reserves a released address", func(t *testing.T) {
		r := require.New(t)

		n, err := New(filepath.Join(t.TempDir(), "net.db"))
		r.NoError(err)

		subnet, err := n.Subnet("172.16.8.0/24")
		r.NoError(err)

		// Reserve and release
		ip1, err := subnet.Reserve()
		r.NoError(err)
		err = subnet.Release(ip1)
		r.NoError(err)

		// Re-reserve the released IP
		addr, _ := netip.ParseAddr("172.16.8.2")
		err = subnet.ReserveSpecificAddr(addr)
		r.NoError(err)

		// Normal Reserve should skip .2 (it's now reserved again)
		ip2, err := subnet.Reserve()
		r.NoError(err)
		r.Equal("172.16.8.3/24", ip2.String())
	})

	t.Run("rejects unusable host addresses", func(t *testing.T) {
		r := require.New(t)

		n, err := New(filepath.Join(t.TempDir(), "net.db"))
		r.NoError(err)

		subnet, err := n.Subnet("172.16.8.0/24")
		r.NoError(err)

		// Outside subnet
		addr, _ := netip.ParseAddr("10.0.0.1")
		err = subnet.ReserveSpecificAddr(addr)
		r.Error(err)
		r.Contains(err.Error(), "not a usable host address")

		// Network address (.0)
		addr, _ = netip.ParseAddr("172.16.8.0")
		err = subnet.ReserveSpecificAddr(addr)
		r.Error(err)

		// Gateway address (.1)
		addr, _ = netip.ParseAddr("172.16.8.1")
		err = subnet.ReserveSpecificAddr(addr)
		r.Error(err)

		// Broadcast address (.255)
		addr, _ = netip.ParseAddr("172.16.8.255")
		err = subnet.ReserveSpecificAddr(addr)
		r.Error(err)

		// Valid host address works
		addr, _ = netip.ParseAddr("172.16.8.2")
		err = subnet.ReserveSpecificAddr(addr)
		r.NoError(err)
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

	t.Run("ReservedAddrs returns only currently reserved addresses", func(t *testing.T) {
		r := require.New(t)

		n, err := New(filepath.Join(t.TempDir(), "net.db"))
		r.NoError(err)

		subnet, err := n.Subnet("172.16.8.0/24")
		r.NoError(err)

		ip1, err := subnet.Reserve()
		r.NoError(err)
		ip2, err := subnet.Reserve()
		r.NoError(err)
		ip3, err := subnet.Reserve()
		r.NoError(err)

		// Release the middle one; it should drop out of the reserved set.
		r.NoError(subnet.Release(ip2))

		reserved, err := subnet.ReservedAddrs()
		r.NoError(err)

		got := make(map[string]bool)
		for _, a := range reserved {
			got[a.String()] = true
		}
		r.True(got[ip1.Addr().String()])
		r.False(got[ip2.Addr().String()], "released address must not be reported as reserved")
		r.True(got[ip3.Addr().String()])
		r.Len(reserved, 2)
	})

	t.Run("concurrent Reserve never hands out a duplicate address", func(t *testing.T) {
		r := require.New(t)

		n, err := New(filepath.Join(t.TempDir(), "net.db"))
		r.NoError(err)

		// Each goroutine fetches its own Subnet handle from the same NetDB —
		// Subnet returns a fresh struct (and mutex) per call, so this exercises
		// the SQLite-level uniqueness guarantee, not just the per-handle mutex.
		const workers = 32

		var (
			mu   sync.Mutex
			seen = make(map[string]bool)
			dup  string
			wg   sync.WaitGroup
		)

		wg.Add(workers)
		for range workers {
			go func() {
				defer wg.Done()

				subnet, err := n.Subnet("172.16.8.0/24")
				if err != nil {
					return
				}
				// Retry past transient SQLite busy errors; the property under
				// test is uniqueness of successful reservations, not liveness.
				var ip netip.Prefix
				for range 100 {
					ip, err = subnet.Reserve()
					if err == nil {
						break
					}
				}
				if err != nil {
					return
				}

				mu.Lock()
				if seen[ip.Addr().String()] {
					dup = ip.Addr().String()
				}
				seen[ip.Addr().String()] = true
				mu.Unlock()
			}()
		}
		wg.Wait()

		r.Empty(dup, "the same address was reserved by two workers")
	})

	t.Run("persists and retrieves leased subnet", func(t *testing.T) {
		r := require.New(t)

		dbPath := filepath.Join(t.TempDir(), "net.db")

		n, err := New(dbPath)
		r.NoError(err)

		// No previous lease — returns invalid prefix with nil error
		prev, err := n.GetLeasedSubnet()
		r.NoError(err)
		r.False(prev.IsValid())

		// Save a lease
		subnet := netip.MustParsePrefix("10.8.95.0/24")
		err = n.SetLeasedSubnet(subnet)
		r.NoError(err)

		// Read it back
		got, err := n.GetLeasedSubnet()
		r.NoError(err)
		r.Equal(subnet, got)

		// Update to a different lease
		subnet2 := netip.MustParsePrefix("10.8.96.0/24")
		err = n.SetLeasedSubnet(subnet2)
		r.NoError(err)
		got2, err := n.GetLeasedSubnet()
		r.NoError(err)
		r.Equal(subnet2, got2)

		// Survives close and reopen
		n.Close()

		n2, err := New(dbPath)
		r.NoError(err)
		defer n2.Close()

		got3, err := n2.GetLeasedSubnet()
		r.NoError(err)
		r.Equal(subnet2, got3)
	})
}
