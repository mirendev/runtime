package commands

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"miren.dev/runtime/pkg/ui"
)

func TestHasReachableAddress(t *testing.T) {
	tests := []struct {
		name    string
		cluster ClusterResponse
		want    bool
	}{
		{
			name:    "has an address",
			cluster: ClusterResponse{APIAddresses: []string{"1.2.3.4:8443"}},
			want:    true,
		},
		{
			name:    "has multiple addresses",
			cluster: ClusterResponse{APIAddresses: []string{"1.2.3.4:8443", "[::1]:8443"}},
			want:    true,
		},
		{
			name:    "empty slice is unreachable",
			cluster: ClusterResponse{APIAddresses: []string{}},
			want:    false,
		},
		{
			name:    "nil slice is unreachable",
			cluster: ClusterResponse{},
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.cluster.hasReachableAddress())
		})
	}
}

func TestBuildClusterPickerItems(t *testing.T) {
	t.Run("mixes reachable and unreachable clusters", func(t *testing.T) {
		clusters := []ClusterResponse{
			{Name: "club", OrganizationName: "Miren Club", APIAddresses: []string{"34.27.122.56:8443"}},
			{Name: "oh-data", OrganizationName: "oh-data"}, // no address
		}

		items, clusterMap, disabled, reachableCount := buildClusterPickerItems(clusters, nil)

		// Every cluster gets a row — the unreachable one is shown, not hidden.
		require.Len(t, items, 2)
		assert.Equal(t, 1, reachableCount)

		// The reachable cluster is selectable and shows its address.
		reachableID := items[0].ID()
		assert.False(t, disabled[reachableID])
		assert.Contains(t, strings.Join(items[0].Row(), " "), "34.27.122.56")
		assert.Same(t, &clusters[0], clusterMap[reachableID])

		// The unreachable cluster is disabled with the reason inline.
		unreachableID := items[1].ID()
		assert.True(t, disabled[unreachableID])
		assert.Contains(t, items[1].Row(), unreachableAddressNote)
		assert.Same(t, &clusters[1], clusterMap[unreachableID])
	})

	t.Run("multiple addresses show a count suffix", func(t *testing.T) {
		clusters := []ClusterResponse{
			{Name: "multi", APIAddresses: []string{"1.1.1.1:8443", "2.2.2.2:8443"}},
		}

		items, _, disabled, reachableCount := buildClusterPickerItems(clusters, nil)

		require.Len(t, items, 1)
		assert.Equal(t, 1, reachableCount)
		assert.False(t, disabled[items[0].ID()])
		assert.Contains(t, strings.Join(items[0].Row(), " "), "(+1)")
	})

	t.Run("all reachable leaves nothing disabled", func(t *testing.T) {
		clusters := []ClusterResponse{
			{Name: "a", APIAddresses: []string{"1.1.1.1:8443"}},
			{Name: "b", APIAddresses: []string{"2.2.2.2:8443"}},
		}

		items, _, disabled, reachableCount := buildClusterPickerItems(clusters, nil)

		require.Len(t, items, 2)
		assert.Equal(t, 2, reachableCount)
		assert.Empty(t, disabled)
	})

	t.Run("all unreachable reports zero selectable", func(t *testing.T) {
		clusters := []ClusterResponse{
			{Name: "a"},
			{Name: "b"},
		}

		items, _, disabled, reachableCount := buildClusterPickerItems(clusters, nil)

		require.Len(t, items, 2)
		assert.Equal(t, 0, reachableCount)
		assert.Len(t, disabled, 2)
	})

	t.Run("empty input yields empty rows", func(t *testing.T) {
		items, clusterMap, disabled, reachableCount := buildClusterPickerItems(nil, nil)

		assert.Empty(t, items)
		assert.Empty(t, clusterMap)
		assert.Empty(t, disabled)
		assert.Equal(t, 0, reachableCount)
	})
}

// TestUnreachableRemediationMessaging guards the user-facing guidance against
// silent drift: the QUIC/UDP-8443 emphasis and the on-host diagnostic pointer
// are the whole point of MIR-1316, so keep them present.
func TestUnreachableRemediationMessaging(t *testing.T) {
	assert.Contains(t, unreachableAddressHelp, "UDP 8443")
	assert.Contains(t, unreachableAddressHelp, "additional_ips")
	assert.Contains(t, unreachableAddressHelp, "miren debug advertise")
	assert.NotEmpty(t, unreachableAddressNote)
}

// A cluster that advertises no address used to be a dead end, greyed out with
// advice about opening a port. That advice is right only when nothing can reach
// it. When cloud holds a link, the cluster works, and the picker has to offer it
// rather than tell the operator to reconfigure a firewall they may not control.
func TestBuildClusterPickerItemsOffersCloudRoutableClusters(t *testing.T) {
	clusters := []ClusterResponse{
		{Name: "club", XID: "cluster-1", OrganizationName: "Miren Club", APIAddresses: []string{"34.27.122.56:8443"}},
		{Name: "behind-nat", XID: "cluster-2", OrganizationName: "Home"},
		{Name: "really-gone", XID: "cluster-3", OrganizationName: "Home"},
	}

	// Cloud can reach the second one, and cannot reach the third.
	routable := map[string]bool{"cluster-2": true}

	items, _, disabled, reachableCount := buildClusterPickerItems(clusters, routable)

	require.Len(t, items, 3)

	// The dialable one and the cloud-routable one are both usable, which is what
	// the count is for.
	assert.Equal(t, 2, reachableCount)

	assert.False(t, disabled[items[0].ID()], "a dialable cluster stays selectable")
	assert.False(t, disabled[items[1].ID()], "a cloud-routable cluster must be selectable")
	assert.True(t, disabled[items[2].ID()], "a cluster nothing can reach stays disabled")

	// And it says which it is, so the choice is not a mystery.
	assert.Contains(t, rowText(items[1]), viaCloudNote)
	assert.Contains(t, rowText(items[2]), unreachableAddressNote)
}

// With no cloud routing available the old behaviour has to hold exactly, since
// that is still the right answer for a cluster nothing can reach.
func TestBuildClusterPickerItemsWithoutCloudRouting(t *testing.T) {
	clusters := []ClusterResponse{
		{Name: "behind-nat", XID: "cluster-2", OrganizationName: "Home"},
	}

	items, _, disabled, reachableCount := buildClusterPickerItems(clusters, nil)

	require.Len(t, items, 1)
	assert.Equal(t, 0, reachableCount)
	assert.True(t, disabled[items[0].ID()])
	assert.Contains(t, rowText(items[0]), unreachableAddressNote)
}

// rowText flattens a picker row so a test can assert on what it says.
func rowText(item ui.PickerItem) string {
	if row, ok := item.(ui.TablePickerItem); ok {
		return strings.Join(row.Columns, " ")
	}
	return item.ID()
}
