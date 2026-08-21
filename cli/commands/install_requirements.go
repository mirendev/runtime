package commands

// Shared system-requirement thresholds for the miren server, used by both the
// native install (server install, Linux-only) and the containerized install
// (server container install, cross-platform). They live here, untagged, so the
// cross-platform container path can reference them too — the native path's own
// file is Linux-tagged.
const (
	minMemoryBytes         = 4 * 1024 * 1024 * 1024 // 4 GB
	recommendedMemoryBytes = 8 * 1024 * 1024 * 1024 // 8 GB
)

// systemResourceTolerance is how far below a threshold we still treat as
// meeting it. Cloud VMs provisioned at a nominal size report a bit less than
// the round number — RAM loses a sliver to firmware/kernel reserve (a "8 GB"
// box shows ~7.76 GB), disks lose more to filesystem overhead (a "100 GB" disk
// shows ~93 GB) — so an exact comparison nags operators who did provision the
// right size. See MIR-1166.
const systemResourceTolerance = 0.90 // accept down to 90% of the threshold

// meetsThreshold reports whether an observed resource amount is at or above a
// threshold, allowing systemResourceTolerance of slack for the nominal-vs-actual
// gap on cloud VMs. A genuinely undersized host (well below the threshold) still
// fails it.
func meetsThreshold(observed, threshold int64) bool {
	return float64(observed) >= float64(threshold)*systemResourceTolerance
}

// requiredNetCommands lists the external commands the server and runner always
// shell out to for container networking: iptables for the bridge and sandbox
// NAT rules, and nft (nftables) for services and NodePorts. Neither ships in the
// release bundle, so a host missing them fails only later, when networking
// silently breaks — the install-time check catches it up front instead. Disk and
// SELinux tooling is deliberately excluded here: it's needed only for optional
// features and those paths already degrade gracefully.
func requiredNetCommands() []string {
	return []string{"iptables", "nft"}
}

// findMissingCommands returns the subset of names that lookup can't resolve on
// PATH, preserving input order. lookup is normally exec.LookPath; taking it as a
// parameter keeps this pure and testable.
func findMissingCommands(names []string, lookup func(string) (string, error)) []string {
	var missing []string
	for _, name := range names {
		if _, err := lookup(name); err != nil {
			missing = append(missing, name)
		}
	}
	return missing
}
