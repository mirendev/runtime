package network

import (
	"io"
	"log/slog"
	"net/netip"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"miren.dev/runtime/pkg/netdb"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestReserveConflictFree(t *testing.T) {
	t.Run("skips an address that is live on the bridge but free in netdb", func(t *testing.T) {
		r := require.New(t)

		n, err := netdb.New(filepath.Join(t.TempDir(), "net.db"))
		r.NoError(err)
		subnet, err := n.Subnet("10.8.64.0/24")
		r.NoError(err)

		// .2 is in use on the wire even though netdb thinks it is free.
		live := map[netip.Addr]bool{netip.MustParseAddr("10.8.64.2"): true}

		ep, err := reserveConflictFree(discardLogger(), "rt0", subnet, live)
		r.NoError(err)
		r.Equal("10.8.64.3/24", ep.String(), "should skip the live .2 and return .3")

		// .2 must stay reserved (quarantined), so it is never handed out later.
		ep2, err := subnet.Reserve()
		r.NoError(err)
		r.Equal("10.8.64.4/24", ep2.String())

		reserved, err := subnet.ReservedAddrs()
		r.NoError(err)
		got := make(map[string]bool)
		for _, a := range reserved {
			got[a.String()] = true
		}
		r.True(got["10.8.64.2"], "conflicting address must remain quarantined as reserved")
	})

	t.Run("errors when every candidate up to the attempt limit is live", func(t *testing.T) {
		r := require.New(t)

		n, err := netdb.New(filepath.Join(t.TempDir(), "net.db"))
		r.NoError(err)
		subnet, err := n.Subnet("10.8.64.0/24")
		r.NoError(err)

		// Mark the first allocBridgeMaxAttempts host addresses (.2 .. ) as live,
		// so every reservation the loop makes collides and it gives up.
		live := make(map[netip.Addr]bool)
		addr := netip.MustParseAddr("10.8.64.2")
		for range allocBridgeMaxAttempts {
			live[addr] = true
			addr = addr.Next()
		}

		ep, err := reserveConflictFree(discardLogger(), "rt0", subnet, live)
		r.Error(err, "should fail rather than return a live address")
		r.False(ep.IsValid())
		r.Contains(err.Error(), "no conflict-free address")
	})

	t.Run("returns the first free address when nothing is live", func(t *testing.T) {
		r := require.New(t)

		n, err := netdb.New(filepath.Join(t.TempDir(), "net.db"))
		r.NoError(err)
		subnet, err := n.Subnet("10.8.64.0/24")
		r.NoError(err)

		ep, err := reserveConflictFree(discardLogger(), "rt0", subnet, nil)
		r.NoError(err)
		r.Equal("10.8.64.2/24", ep.String())
	})
}
