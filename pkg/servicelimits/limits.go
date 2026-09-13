// Package servicelimits generates the systemd drop-in that bounds the memory a
// miren daemon may use, so a runaway control process degrades miren instead of
// taking the whole machine down with it.
//
// # What the limit covers
//
// The miren.service cgroup holds the coordinator process, the containerd daemon
// it manages, and one containerd-shim per container. It does NOT hold the
// containers themselves: sandboxes are created at /miren/sandbox-<id> and every
// other containerd workload at /<namespace>/<id>, both absolute paths that runc
// resolves against the cgroup root. So /miren is a top-level cgroup, a sibling
// of system.slice, holding app payloads, addon databases, etcd, buildkit and the
// two Victoria services.
//
// Capping miren.service therefore cannot kill user workloads, which is what
// makes this safe to apply by default.
//
// # What the limit is sized against
//
// Not the coordinator. A production coordinator holds 489 MB of anonymous
// memory against 39 GB of page cache, because memory.max applies to
// memory.current and containerd charges every image layer it touches to this
// cgroup. Cache is reclaimable and anon is not, so the part a leak grows is the
// small one — but the limit governs the total.
//
// That cuts both ways. The limit has to sit far enough above normal cache to
// avoid trimming it for no reason, and far enough below the host to stop a
// runaway before the machine dies. See memoryFraction.
//
// # Why a drop-in rather than the unit itself
//
// The base unit is written only when it is absent or --force is passed, so a
// change to the unit template never reaches a host that already has miren
// installed — precisely the hosts at risk. A drop-in can be rewritten
// unconditionally on install and on upgrade, leaves operator edits to the base
// unit alone, and keeps `systemctl edit` free as an override: that writes
// override.conf, which sorts after the 10- prefix used here and therefore wins.
package servicelimits

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	mib = 1024 * 1024
	gib = 1024 * 1024 * 1024

	// memoryFraction is the share of host RAM the cgroup may use.
	//
	// The limit has to cover far more than the coordinator. memory.max applies
	// to memory.current, which includes page cache, and containerd charges every
	// image layer it reads or writes to this cgroup. On a production coordinator
	// the split is stark: 489 MB anon against 39 GB of file cache. The
	// non-reclaimable part — the part a leak grows and the part that actually
	// kills a host — is the small one.
	//
	// So the fraction is not sized to what a control plane needs. It is sized to
	// leave page cache room to do its job while still bounding a runaway well
	// short of the machine. A quarter does both: it is many times the real
	// resident footprint, and it leaves three quarters of the host for
	// everything else.
	//
	// Deliberately no ceiling. An earlier version capped at 16 GiB, reasoning
	// about what a coordinator needs. On a 629 GB host that would have trimmed
	// containerd's cache from 39 GB to 15 GB — a pure loss, since the memory was
	// abundant and the cache is free. Page cache scales with the host and the
	// workload, so the limit has to as well.
	memoryFraction = 4

	// memoryFloorBytes keeps the limit workable on a 4 GB host — the documented
	// minimum — where a flat quarter would leave too little headroom for a
	// build or a burst of shims.
	memoryFloorBytes = 2 * gib

	// memoryHeadroomPercent bounds the limit against physical RAM, so a tiny
	// machine that falls under memoryFloorBytes still leaves the rest of the
	// system something to run in.
	memoryHeadroomPercent = 90

	// memoryHighPercent sets MemoryHigh relative to MemoryMax. Crossing it puts
	// the cgroup under reclaim pressure and throttles it rather than killing it
	// outright.
	//
	// Be clear about what that does and doesn't buy. The kernel can reclaim page
	// cache — containerd's image pulls, mmap'd files — but it cannot shrink a Go
	// heap, and nothing sets GOMEMLIMIT in the coordinator today. So a runaway in
	// Go memory is slowed between MemoryHigh and MemoryMax, not healed: it still
	// converges on the kill. The value of the band is the slowdown and the
	// signal, not a recovery. Making the grace period real means having the
	// server read its own cgroup's memory.high at boot and calling
	// debug.SetMemoryLimit, which is follow-up work.
	memoryHighPercent = 85
)

// DropInFileName is the drop-in this package manages. The numeric prefix orders
// it before `systemctl edit`'s override.conf, so an operator override wins.
const DropInFileName = "10-miren-resources.conf"

// unitDir is the systemd unit directory. A variable so tests can redirect it.
var unitDir = "/etc/systemd/system"

// binPath is the installed miren binary, used for the ExecStopPost hook.
const binPath = "/var/lib/miren/release/miren"

// Limits is the set of cgroup limits computed for a host.
type Limits struct {
	// SystemRAMBytes is the detected host memory, or 0 when it could not be
	// read.
	SystemRAMBytes int64

	// MemoryMaxBytes and MemoryHighBytes are 0 when host memory is unknown. In
	// that case the memory directives are omitted rather than guessed: a limit
	// picked blind could be far too tight and would turn a diagnostic gap into
	// an outage.
	MemoryMaxBytes  int64
	MemoryHighBytes int64
}

// Known returns whether a memory limit could be computed.
func (l Limits) Known() bool { return l.MemoryMaxBytes > 0 }

// Compute derives the cgroup limits for a host with ramBytes of memory. A
// ramBytes of 0 means the host's memory could not be determined.
func Compute(ramBytes int64) Limits {
	l := Limits{SystemRAMBytes: ramBytes}
	if ramBytes <= 0 {
		return l
	}

	limit := max(ramBytes/memoryFraction, memoryFloorBytes)
	limit = min(limit, ramBytes*memoryHeadroomPercent/100)

	// Round both down to whole MiB so they render as "11262M" rather than
	// "11809554432". A quarter of an arbitrary MemTotal rarely lands on a MiB
	// boundary — a real 44 GB host produced 11809554432 — and an operator
	// reading the drop-in should not have to divide to see what the cap is.
	limit = (limit / mib) * mib

	l.MemoryMaxBytes = limit
	l.MemoryHighBytes = (limit / mib) * memoryHighPercent / 100 * mib
	return l
}

// DetectSystemRAMBytes reads total system memory from /proc/meminfo, returning 0
// when it cannot be determined.
func DetectSystemRAMBytes() int64 {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		if !strings.HasPrefix(line, "MemTotal:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return 0
		}
		kb, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			return 0
		}
		return kb * 1024 // /proc/meminfo reports kB
	}
	return 0
}

// Render returns the drop-in body for a unit. statePath is where the
// ExecStopPost hook writes its record; an empty statePath omits the hook.
//
// OOMPolicy is deliberately left at its default. OOMPolicy=kill would take the
// containerd shims down along with the coordinator, disrupting apps that the
// cap is specifically meant to spare.
func Render(unit string, statePath string, l Limits) string {
	var b strings.Builder

	b.WriteString("# Managed by miren — regenerated on install and upgrade. Do not edit.\n")
	fmt.Fprintf(&b, "# To override, run `sudo systemctl edit %s` and set your own values\n", UnitShortName(unit))
	b.WriteString("# there; systemd applies that file after this one.\n")
	b.WriteString("[Service]\n")

	if l.Known() {
		fmt.Fprintf(&b, "MemoryHigh=%s\n", formatBytes(l.MemoryHighBytes))
		fmt.Fprintf(&b, "MemoryMax=%s\n", formatBytes(l.MemoryMaxBytes))
	} else {
		b.WriteString("# Host memory could not be determined, so no memory limit was set.\n")
	}

	// Swap thrash is what actually makes a host unresponsive. Without this the
	// cgroup drags the machine through swap long before it reaches MemoryMax.
	b.WriteString("MemorySwapMax=0\n")

	if statePath != "" {
		// The leading "-" makes systemd record the hook's exit status without
		// treating a failure as a failure of the unit. Without it, any stop
		// where the hook can't run cleanly leaves the unit in a failed state:
		// a rollback to a build with no `internal record-exit` subcommand, a
		// binary momentarily absent mid-upgrade, or the hook itself being
		// killed under the memory pressure this whole file exists for. Losing
		// one diagnostic record is always better than failing the stop.
		fmt.Fprintf(&b, "ExecStopPost=-%s internal record-exit --unit=%s --output=%s\n",
			binPath, unit, quoteExecArg(statePath))
	}

	return b.String()
}

// Write renders the drop-in for a unit and installs it, replacing any previous
// version. It creates the drop-in directory if needed and writes atomically, so
// a crash mid-write cannot leave systemd with a truncated file.
//
// It reports false when it deliberately left an existing drop-in in place: with
// unknown limits Render omits the memory directives, and overwriting a host's
// working limit with none would silently remove the protection it already had.
// Losing RAM detection is far more likely to be a passing oddity than a real
// change, so the existing file wins. A host with no drop-in yet still gets one,
// since even without a limit it carries the ExecStopPost hook.
func Write(unit string, statePath string, l Limits) (bool, error) {
	dir := DropInDir(unit)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false, fmt.Errorf("create drop-in directory %s: %w", dir, err)
	}

	path := filepath.Join(dir, DropInFileName)

	if !l.Known() {
		if _, err := os.Stat(path); err == nil {
			return false, nil
		}
	}
	tmp, err := os.CreateTemp(dir, DropInFileName+".*")
	if err != nil {
		return false, fmt.Errorf("create temp drop-in in %s: %w", dir, err)
	}
	defer os.Remove(tmp.Name())

	if _, err := tmp.WriteString(Render(unit, statePath, l)); err != nil {
		tmp.Close()
		return false, fmt.Errorf("write drop-in %s: %w", path, err)
	}
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return false, fmt.Errorf("chmod drop-in %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return false, fmt.Errorf("close drop-in %s: %w", path, err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return false, fmt.Errorf("install drop-in %s: %w", path, err)
	}
	return true, nil
}

// Remove deletes the managed drop-in and, if it is then empty, the drop-in
// directory. A directory left behind by an operator override is kept.
func Remove(unit string) error {
	dir := DropInDir(unit)
	if err := os.Remove(filepath.Join(dir, DropInFileName)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove drop-in in %s: %w", dir, err)
	}
	// Ignore the error: a non-empty directory is the expected case when an
	// operator has their own override alongside ours.
	os.Remove(dir)
	return nil
}

// ExistingStatePath returns the record directory recorded in a unit's managed
// drop-in, if there is one.
//
// Install knows the effective data path (from the server's config, or the
// runner's --data-path). Upgrade does not, and recomputing it there would
// overwrite a correct custom path with the compiled-in default — the hook would
// then write records somewhere the daemon never reads, and restarts would go
// unreported with nothing to show for it. So upgrade reuses whatever install
// worked out, which also preserves a path an operator edited by hand.
func ExistingStatePath(unit string) (string, bool) {
	body, err := os.ReadFile(filepath.Join(DropInDir(unit), DropInFileName))
	if err != nil {
		return "", false
	}

	for line := range strings.SplitSeq(string(body), "\n") {
		if !strings.HasPrefix(line, "ExecStopPost=") {
			continue
		}
		_, arg, ok := strings.Cut(line, "--output=")
		if !ok {
			continue
		}
		if path := unquoteExecArg(strings.TrimSpace(arg)); path != "" {
			return path, true
		}
	}
	return "", false
}

// DropInDir returns the systemd drop-in directory for a unit.
func DropInDir(unit string) string {
	return filepath.Join(unitDir, unit+".d")
}

// UnitShortName is the name an operator types, e.g. "miren" for "miren.service".
// An empty unit falls back to the server, so a record written before the unit
// name was stored still yields a usable instruction.
func UnitShortName(unit string) string {
	if unit == "" {
		return "miren"
	}
	return strings.TrimSuffix(unit, ".service")
}

// quoteExecArg renders a value safe to embed in a systemd Exec= line.
//
// The runner's state path comes from `miren runner install --data-path`, so it
// is operator-supplied. Left raw, a path containing a space splits into extra
// arguments and the hook silently stops recording — the failure mode being a
// missing crash report, with nothing to point at the cause. A newline would end
// the directive early and let the rest of the value inject arbitrary settings
// into the drop-in.
//
// systemd applies shell-like unquoting to Exec arguments, so a double-quoted
// string with backslash-escaped specials is understood the way it looks.
func quoteExecArg(s string) string {
	// "%" introduces a unit-file specifier (%n is the unit name, %t the runtime
	// directory) and "$" a variable reference. Both are interpreted whether or
	// not the value is quoted, and each has its own documented escape — "%%"
	// and "$$" — so they are doubled before any quoting decision. Backslash
	// does not escape either of them.
	s = strings.ReplaceAll(s, "%", "%%")
	s = strings.ReplaceAll(s, "$", "$$")

	if !strings.ContainsAny(s, " \t\n\r\"'\\;") {
		return s
	}

	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"', '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// unquoteExecArg reverses quoteExecArg, so a path this package wrote can be read
// back out of a drop-in.
func unquoteExecArg(s string) string {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		var b strings.Builder
		body := []rune(s[1 : len(s)-1])
		for i := 0; i < len(body); i++ {
			if body[i] != '\\' || i+1 >= len(body) {
				b.WriteRune(body[i])
				continue
			}
			i++
			switch body[i] {
			case 'n':
				b.WriteByte('\n')
			case 'r':
				b.WriteByte('\r')
			case 't':
				b.WriteByte('\t')
			default:
				b.WriteRune(body[i])
			}
		}
		s = b.String()
	}

	// Undo the specifier and variable doubling, which quoteExecArg applies
	// outside the quoting decision.
	s = strings.ReplaceAll(s, "$$", "$")
	s = strings.ReplaceAll(s, "%%", "%")
	return s
}

// formatBytes renders a byte count in the largest unit that divides it exactly,
// so limits read as "4G" rather than "4294967296".
func formatBytes(n int64) string {
	switch {
	case n%gib == 0:
		return strconv.FormatInt(n/gib, 10) + "G"
	case n%mib == 0:
		return strconv.FormatInt(n/mib, 10) + "M"
	default:
		return strconv.FormatInt(n, 10)
	}
}
