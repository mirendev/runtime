package buildkit

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGenerateConfig(t *testing.T) {
	c := &Component{}
	config := c.generateConfig(10*1024*1024*1024, 86400, "registry.example.com:5000")

	// gcPolicyBlocks splits the config into its individual gcpolicy bodies
	// (text from one "[[worker.oci.gcpolicy]]" header to the next section).
	gcPolicyBlocks := func(cfg string) []string {
		var blocks []string
		const hdr = "[[worker.oci.gcpolicy]]"
		rest := cfg
		for {
			i := strings.Index(rest, hdr)
			if i < 0 {
				break
			}
			rest = rest[i+len(hdr):]
			end := len(rest)
			if n := strings.Index(rest, "[["); n >= 0 {
				end = n
			}
			if n := strings.Index(rest, "\n[registry"); n >= 0 && n < end {
				end = n
			}
			blocks = append(blocks, rest[:end])
		}
		return blocks
	}

	t.Run("enforces the byte cap with an age-less tier", func(t *testing.T) {
		r := require.New(t)

		blocks := gcPolicyBlocks(config)
		r.GreaterOrEqual(len(blocks), 2, "should have multiple graduated gcpolicy tiers")

		// MIR-1280: a keepDuration-only policy can never enforce the cap because
		// it pins recently-used records and their whole ancestry. At least one
		// tier must carry maxUsedSpace WITHOUT keepDuration so the ceiling binds
		// regardless of entry age.
		ageless := 0
		for _, b := range blocks {
			if strings.Contains(b, "maxUsedSpace") && !strings.Contains(b, "keepDuration") {
				ageless++
			}
		}
		r.GreaterOrEqual(ageless, 1, "at least one tier must enforce maxUsedSpace with no keepDuration")

		// The last-resort tier must be the age-less all=true ceiling, or
		// internal/frontend cache stays outside the enforced cap.
		last := blocks[len(blocks)-1]
		r.Contains(last, "all = true", "final tier should cover all cache types")
		r.Contains(last, "maxUsedSpace", "final tier should enforce the byte cap")
		r.NotContains(last, "keepDuration", "final tier must not be age-gated")
	})

	t.Run("uses maxUsedSpace as the cap, not deprecated keepBytes", func(t *testing.T) {
		r := require.New(t)
		r.Contains(config, "maxUsedSpace")
		r.NotContains(config, "keepBytes")
	})

	t.Run("source filters use OR (array) form, not comma-joined AND", func(t *testing.T) {
		r := require.New(t)
		// A single comma-joined filter string is AND'd and matches no record.
		r.NotContains(config, "type==source.local,type==exec.cachemount")
		r.Contains(config, `"type==source.local"`)
		r.Contains(config, `"type==exec.cachemount"`)
		r.Contains(config, `"type==source.git.checkout"`)
	})

	t.Run("uses provided registry host", func(t *testing.T) {
		r := require.New(t)
		r.Contains(config, `[registry."registry.example.com:5000"]`)
	})

	t.Run("uses default registry host when empty", func(t *testing.T) {
		r := require.New(t)
		defaultConfig := c.generateConfig(10*1024*1024*1024, 86400, "")
		r.Contains(defaultConfig, `[registry."cluster.local:5000"]`)
	})

	t.Run("does not hardcode DNS nameservers", func(t *testing.T) {
		r := require.New(t)
		// MIR-1643: DNS is delegated to buildkitd, which derives each build's
		// resolv.conf from the host. A hardcoded nameserver directive would
		// override that and break builds on hosts with an internal resolver.
		r.NotContains(config, "nameservers=")
		r.NotContains(config, "1.1.1.1")
		r.NotContains(config, "8.8.8.8")
	})
}
