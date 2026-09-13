//go:build linux

package server

import (
	"context"
	"log/slog"
	"path/filepath"

	"miren.dev/runtime/pkg/boot"
	"miren.dev/runtime/pkg/exitrecord"
)

type exitReportBootInputs struct {
	dataPath string
}

type exitReportBoot struct {
	component *boot.Component
	inputs    exitReportBootInputs
}

func exitReportInputs(options StartOptions) exitReportBootInputs {
	return exitReportBootInputs{dataPath: options.Config.Server.GetDataPath()}
}

// newExitReportBoot reports why the previous run of this server ended, if it
// ended badly.
//
// It depends on observability for its logger rather than for any data. Only
// that logger tees into VictoriaLogs, which is what puts a line in
// `miren logs system` — the place an operator will actually look. A warning
// written any earlier in startup reaches the journal and nowhere else.
func newExitReportBoot(inputs exitReportBootInputs, observability boot.Output[observabilityBootOutput]) *exitReportBoot {
	b := &exitReportBoot{inputs: inputs}
	b.component = boot.Run1("exit-report", observability, b.start)
	return b
}

func (b *exitReportBoot) start(_ context.Context, observability observabilityBootOutput) error {
	reportPreviousExit(filepath.Join(b.inputs.dataPath, "server"), observability.log)
	return nil
}

// reportPreviousExit logs the recorded exit, then marks it reported so the
// warning fires once per occurrence rather than on every boot.
//
// This runs late — only a boot that reaches observability gets here — which is
// what puts the line in `miren logs system`. A crashing server may never make it
// this far, so cli/commands.ReportPreviousExitEarly logs the same record to the
// journal before startup begins. That one deliberately does not mark the record
// reported: retiring it here, once it has reached durable storage, is what keeps
// the system-logs line appearing on the first boot that actually works.
//
// The cost is one duplicated warning in the journal on a healthy boot after a
// crash. That is the right trade — a crash report that only survives the happy
// path is not a crash report.
//
// Nothing here can fail startup. A server that can't read its own crash record
// has a reporting gap, not a reason to stay down.
func reportPreviousExit(dir string, log *slog.Logger) {
	rec, found, err := exitrecord.Read(dir)
	if err != nil {
		log.Warn("could not read the previous run's exit record", "err", err, "dir", dir)
		return
	}
	if !found {
		return
	}

	rec.LogTo(log)

	if err := exitrecord.MarkReported(dir); err != nil {
		log.Warn("could not mark the exit record as reported", "err", err, "dir", dir)
	}
}
