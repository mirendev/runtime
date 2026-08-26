package commands

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/pkg/ui"
)

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;:]*m`)

func stripANSI(s string) string { return ansiRe.ReplaceAllString(s, "") }

// TestClusterPickerRendersUnreachable drives the real ui.PickerModel with the
// same disabled wiring selectClusterFromList uses, and asserts the rendered
// view shows the unreachable cluster with its reason (rather than hiding it)
// and surfaces the remediation when the cursor lands on it. This is the UX
// MIR-1316 is about, so verify it end-to-end against the actual picker.
func TestClusterPickerRendersUnreachable(t *testing.T) {
	clusters := []ClusterResponse{
		{Name: "club", OrganizationName: "Miren Club", APIAddresses: []string{"34.27.122.56:8443"}},
		{Name: "oh-data", OrganizationName: "oh-data"}, // firewalled: no address
	}

	items, _, disabled, reachableCount := buildClusterPickerItems(clusters, nil)
	require.Equal(t, 1, reachableCount)

	m := ui.NewPicker(items,
		ui.WithTitle("Select a cluster to bind:"),
		ui.WithHeaders([]string{"NAME", "ORGANIZATION", "ADDRESS"}),
		ui.WithDisabledCheck(func(item ui.PickerItem) bool {
			return disabled[item.ID()]
		}, unreachableAddressHelp),
	)

	// Cursor on the reachable row: both clusters visible, no warning shown yet.
	view := stripANSI(m.View())
	assert.Contains(t, view, "club")
	assert.Contains(t, view, "oh-data")
	assert.Contains(t, view, unreachableAddressNote)
	assert.NotContains(t, view, unreachableAddressHelp)

	// Move the cursor onto the disabled (unreachable) row: the remediation
	// message appears.
	m.SetCursor(1)
	view = stripANSI(m.View())
	assert.Contains(t, view, unreachableAddressHelp)
}
