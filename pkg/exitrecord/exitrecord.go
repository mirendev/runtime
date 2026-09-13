// Package exitrecord persists why systemd last stopped a miren daemon, so the
// next run can tell the user about it.
//
// The information only exists for an instant. systemd exposes the reason a unit
// stopped as the unit's Result property, and resets it to "success" as soon as
// the replacement instance activates — so a process that looks at its own unit
// during startup always sees a healthy unit, however violently its predecessor
// died. The one place the reason is readable is an ExecStopPost hook, which runs
// after the main process is gone and has SERVICE_RESULT in its environment.
//
// So the hook writes a record here, and the next boot reads it. Without this, a
// server killed for exceeding its memory limit comes back up with no idea that
// it ever went down, and neither does the operator.
package exitrecord

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// FileName holds an exit that has not yet been reported.
	FileName = "last-exit.json"

	// ReportedFileName holds one that has. Renaming rather than deleting keeps
	// the detail available for later diagnosis while making sure the warning is
	// logged once per occurrence instead of on every boot.
	ReportedFileName = "last-exit.reported.json"

	// ResultSuccess is systemd's SERVICE_RESULT for a clean stop.
	ResultSuccess = "success"

	// ResultOOMKill is systemd's SERVICE_RESULT when the kernel's out-of-memory
	// killer took a process in the unit's cgroup.
	ResultOOMKill = "oom-kill"
)

// Record is one abnormal exit of a miren daemon.
type Record struct {
	At   time.Time `json:"at"`
	Unit string    `json:"unit"`

	// Result is systemd's SERVICE_RESULT, e.g. "oom-kill", "signal",
	// "exit-code", "timeout".
	Result string `json:"result"`

	// ExitCode is systemd's EXIT_CODE: "exited", "killed" or "dumped".
	ExitCode string `json:"exit_code,omitempty"`

	// ExitStatus is the numeric status or signal that goes with ExitCode.
	ExitStatus string `json:"exit_status,omitempty"`

	// MemoryPeak is the highest memory the unit's cgroup reached, and MemoryMax
	// the limit it was held to. Both are 0 when systemd didn't report them —
	// MemoryPeak needs systemd 254 or newer, and MemoryMax reads as "infinity"
	// when no limit is set.
	MemoryPeak int64 `json:"memory_peak,omitempty"`
	MemoryMax  int64 `json:"memory_max,omitempty"`

	// Restarts is the unit's NRestarts counter at the time of the exit.
	Restarts int `json:"restarts,omitempty"`
}

// OOMKilled reports whether this exit was the kernel reclaiming memory.
func (r Record) OOMKilled() bool { return r.Result == ResultOOMKill }

// LogTo writes the record as a single warning.
//
// Warn rather than Error: the platform did its job. The control process was
// contained and restarted instead of taking the host down with it, and an Error
// here would page someone about a system that worked as designed.
//
// Individual fields are named rather than logging the struct, per the log-level
// guidance in CLAUDE.md — "%+v" on a record renders its whole field tree inline.
func (r Record) LogTo(log *slog.Logger) {
	if r.OOMKilled() {
		log.Warn("previous miren server run was killed for exceeding its memory limit",
			"at", r.At,
			"memory_peak", r.MemoryPeak,
			"memory_max", r.MemoryMax,
			"restarts", r.Restarts,
			"hint", "raise the limit with `systemctl edit "+unitShortName(r.Unit)+"` if this was legitimate growth")
		return
	}

	log.Warn("previous miren server run exited abnormally",
		"at", r.At,
		"result", r.Result,
		"exit_code", r.ExitCode,
		"exit_status", r.ExitStatus,
		"restarts", r.Restarts)
}

// unitShortName is the name an operator types, e.g. "miren" for "miren.service".
// An empty unit falls back to the server, so a record written before the unit
// name was stored still yields a usable instruction.
//
// Duplicated from servicelimits deliberately: this package is read by the server
// at boot and has no business depending on the code that writes systemd units.
func unitShortName(unit string) string {
	if unit == "" {
		return "miren"
	}
	return strings.TrimSuffix(unit, ".service")
}

// Write stores a record in dir, replacing any previous one. The write is atomic
// so a crash mid-write can't leave a half-written record for the next boot to
// choke on.
func Write(dir string, r Record) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create state directory %s: %w", dir, err)
	}

	body, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return fmt.Errorf("encode exit record: %w", err)
	}
	body = append(body, '\n')

	path := filepath.Join(dir, FileName)
	tmp, err := os.CreateTemp(dir, FileName+".*")
	if err != nil {
		return fmt.Errorf("create temp exit record in %s: %w", dir, err)
	}
	defer os.Remove(tmp.Name())

	if _, err := tmp.Write(body); err != nil {
		tmp.Close()
		return fmt.Errorf("write exit record %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close exit record %s: %w", path, err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("install exit record %s: %w", path, err)
	}
	return nil
}

// Read returns the unreported record in dir. The boolean is false when there
// isn't one, which is the ordinary case: nothing went wrong last time.
func Read(dir string) (Record, bool, error) {
	body, err := os.ReadFile(filepath.Join(dir, FileName))
	if os.IsNotExist(err) {
		return Record{}, false, nil
	}
	if err != nil {
		return Record{}, false, fmt.Errorf("read exit record: %w", err)
	}

	var r Record
	if err := json.Unmarshal(body, &r); err != nil {
		return Record{}, false, fmt.Errorf("parse exit record: %w", err)
	}
	return r, true, nil
}

// Clear removes any stored record. Called after a clean stop, so the file's
// presence always means the previous run ended badly.
func Clear(dir string) error {
	if err := os.Remove(filepath.Join(dir, FileName)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("clear exit record: %w", err)
	}
	return nil
}

// MarkReported moves the record aside once it has been surfaced to the user.
func MarkReported(dir string) error {
	err := os.Rename(filepath.Join(dir, FileName), filepath.Join(dir, ReportedFileName))
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("mark exit record reported: %w", err)
	}
	return nil
}
