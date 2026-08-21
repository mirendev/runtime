package commands

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFindMissingCommands(t *testing.T) {
	names := []string{"iptables", "nft"}

	// lookupWith returns a fake exec.LookPath that resolves only the given
	// commands and reports the rest as missing.
	lookupWith := func(present ...string) func(string) (string, error) {
		set := make(map[string]bool, len(present))
		for _, p := range present {
			set[p] = true
		}
		return func(name string) (string, error) {
			if set[name] {
				return "/usr/sbin/" + name, nil
			}
			return "", fmt.Errorf("not found")
		}
	}

	assert.Empty(t, findMissingCommands(names, lookupWith("iptables", "nft")),
		"all present should report nothing missing")

	assert.Equal(t, []string{"nft"}, findMissingCommands(names, lookupWith("iptables")),
		"only the unresolved command should be reported")

	assert.Equal(t, []string{"iptables", "nft"}, findMissingCommands(names, lookupWith()),
		"none present should report all, in input order")
}

func TestMeetsThreshold(t *testing.T) {
	const gib = int64(1024 * 1024 * 1024)

	// Nominal cloud VM sizes report a bit under the round number but should
	// still count as meeting the threshold (MIR-1166).
	assert.True(t, meetsThreshold(97*8*gib/100, 8*gib), `"8 GB" VM reporting ~7.76 GB should meet 8 GB`)
	assert.True(t, meetsThreshold(93*100*gib/100, 100*gib), `"100 GB" disk reporting ~93.55 GB should meet 100 GB`)
	assert.True(t, meetsThreshold(95*4*gib/100, 4*gib), `"4 GB" VM reporting ~3.8 GB should meet 4 GB`)

	// At or above the threshold always meets.
	assert.True(t, meetsThreshold(4*gib, 4*gib))
	assert.True(t, meetsThreshold(16*gib, 8*gib))

	// Genuinely undersized hosts still fail — the tolerance is slack, not a
	// license to run on half the required resources.
	assert.False(t, meetsThreshold(2*gib, 4*gib), "2 GB is genuinely below a 4 GB minimum")
	assert.False(t, meetsThreshold(512*1024*1024, 4*gib), "512 MB is well below minimum")
	assert.False(t, meetsThreshold(89*100*gib/100, 100*gib), "89% of a threshold is past the tolerance")
}
