//go:build linux

package commands

import (
	"log/slog"
	"path/filepath"

	"miren.dev/runtime/pkg/exitrecord"
)

// reportPreviousExitEarly warns about a recorded abnormal exit before the
// dependency graph starts.
//
// The durable report lives in a boot component that depends on observability,
// because only that logger tees into VictoriaLogs and so reaches
// `miren logs system`. But a server that is being killed for exceeding its
// memory limit may never get that far: each restart would overwrite the previous
// record and nothing would ever be logged, which is exactly the crashloop an
// operator most needs to see.
//
// So this runs first and writes to the journal, where it survives regardless of
// how far startup gets. It deliberately does NOT mark the record reported — the
// component does that once the warning has reached durable storage — so on a
// boot that succeeds the operator sees the line twice in the journal and once in
// the system logs. A duplicate line is a much smaller problem than a silent
// crashloop.
func reportPreviousExitEarly(dataPath string, log *slog.Logger) {
	dir := filepath.Join(dataPath, "server")

	rec, found, err := exitrecord.Read(dir)
	if err != nil {
		log.Warn("could not read the previous run's exit record", "err", err, "dir", dir)
		return
	}
	if !found {
		return
	}

	rec.LogTo(log)
}
