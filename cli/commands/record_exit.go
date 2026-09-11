//go:build linux

package commands

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"miren.dev/runtime/pkg/exitrecord"
)

// InternalRecordExit is the ExecStopPost hook in the managed resource-limit
// drop-in. systemd runs it after the main process is gone, which is the only
// moment the reason for the stop is readable — see pkg/exitrecord.
//
// It never returns an error. A failure here would make systemd report the unit
// as failing to stop, which is a worse outcome than losing one diagnostic
// record. Problems go to stderr instead, which the unit routes to the journal.
func InternalRecordExit(ctx *Context, opts struct {
	Unit   string `long:"unit" description:"systemd unit whose exit is being recorded" default:"miren.service"`
	Output string `long:"output" description:"Directory to write the exit record to" default:"/var/lib/miren/server"`
}) error {
	rec, abnormal := exitRecordFromEnv(opts.Unit, time.Now().UTC())

	// A clean stop leaves no record, so the file's presence on the next boot is
	// itself the signal that something went wrong.
	if !abnormal {
		if err := exitrecord.Clear(opts.Output); err != nil {
			fmt.Fprintf(os.Stderr, "could not clear exit record: %v\n", err)
		}
		return nil
	}

	rec.MemoryPeak, rec.MemoryMax, rec.Restarts = unitMemoryFacts(opts.Unit)

	if err := exitrecord.Write(opts.Output, rec); err != nil {
		fmt.Fprintf(os.Stderr, "could not record why %s stopped: %v\n", opts.Unit, err)
		return nil
	}

	ctx.Log.Info("recorded abnormal service exit",
		"unit", opts.Unit, "result", rec.Result, "exit_status", rec.ExitStatus)
	return nil
}

// exitRecordFromEnv builds a record from the variables systemd sets for an
// ExecStopPost hook. The boolean is false when the unit stopped cleanly and
// there is nothing to record.
func exitRecordFromEnv(unit string, now time.Time) (exitrecord.Record, bool) {
	result := strings.TrimSpace(os.Getenv("SERVICE_RESULT"))

	// An empty SERVICE_RESULT means the hook ran outside systemd, which is not
	// evidence that anything went wrong.
	if result == "" || result == exitrecord.ResultSuccess {
		return exitrecord.Record{}, false
	}

	return exitrecord.Record{
		At:         now,
		Unit:       unit,
		Result:     result,
		ExitCode:   strings.TrimSpace(os.Getenv("EXIT_CODE")),
		ExitStatus: strings.TrimSpace(os.Getenv("EXIT_STATUS")),
	}, true
}

// unitMemoryFacts reads the memory numbers that make an out-of-memory kill
// legible: how far the unit got, and what it was held to.
//
// Every value is optional. MemoryPeak needs systemd 254 or newer, MemoryMax
// reads as "infinity" when no limit is set, and the whole call fails on a host
// where systemd isn't reachable. A missing number is reported as 0 rather than
// blocking the record, which is the part that matters.
func unitMemoryFacts(unit string) (peak, limit int64, restarts int) {
	// Deliberately NOT --value. systemd prints properties in its own internal
	// order, not the order they were requested in, and --value strips the names
	// that would let us tell them apart. Asking for MemoryPeak, MemoryMax and
	// NRestarts comes back as NRestarts, MemoryPeak, MemoryMax, so reading the
	// lines positionally assigns every number to the wrong field.
	out, err := exec.Command("systemctl", "show",
		"-p", "MemoryPeak", "-p", "MemoryMax", "-p", "NRestarts", unit).Output()
	if err != nil {
		return 0, 0, 0
	}

	return memoryFactsFromShow(string(out))
}

// memoryFactsFromShow maps `systemctl show` output onto the record's fields. It
// is separate from unitMemoryFacts so a test can feed it real systemctl output
// and catch a regression to positional parsing.
func memoryFactsFromShow(out string) (peak, limit int64, restarts int) {
	props := parseUnitProperties(out)
	peak = parseUnitBytes(props["MemoryPeak"])
	limit = parseUnitBytes(props["MemoryMax"])
	restarts, _ = strconv.Atoi(strings.TrimSpace(props["NRestarts"]))
	return peak, limit, restarts
}

// parseUnitProperties reads `systemctl show` output into a map. Values may
// themselves contain "=", so only the first one separates name from value.
func parseUnitProperties(out string) map[string]string {
	props := map[string]string{}
	for line := range strings.SplitSeq(out, "\n") {
		name, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		props[strings.TrimSpace(name)] = value
	}
	return props
}

// parseUnitBytes reads a systemd byte-valued property, returning 0 for the
// values systemd uses to mean "no value": "infinity" for an unset limit, and
// "[not set]" on versions that don't track the property.
func parseUnitBytes(s string) int64 {
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return 0
	}
	return n
}
