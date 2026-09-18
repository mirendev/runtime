package metrics

import (
	"context"
	"log/slog"
	"time"

	"miren.dev/runtime/version"
)

// processStart is evaluated during package initialization, before main runs,
// so it lands within milliseconds of exec. That is close enough to the true
// process start for restart detection, and unlike reading /proc/self/stat it
// needs no clock-tick arithmetic and works on every platform.
var processStart = time.Now()

// ProcessInfo publishes the identity of the miren control process: when it
// started and which build it is. Both are constants for the life of the
// process, and that is the point. A restart shows up as a step in
// process_start_time_seconds, an upgrade as a change in the commit label of
// miren_build_info, and either one explains a heap curve that resets or a
// goroutine count that gaps without anyone having to line the gap up against
// the commit log.
//
// The series carry the same entity label as RuntimeMemory so the two can be
// joined, and pick up cluster and runner identity at the shipping layer like
// every other operational series.
type ProcessInfo struct {
	Log    *slog.Logger
	Writer PointWriter

	// Entity is the value of the "entity" label on every emitted series.
	Entity string

	// StartTime is what process_start_time_seconds reports.
	StartTime time.Time

	// Version, Commit and Channel are the labels on miren_build_info. Version
	// and Commit are emitted as-is: a dev build reports "unknown", which is
	// worth seeing in the store, since an unknown build in a fleet is skew.
	// Channel is omitted when the build has none.
	Version string
	Commit  string
	Channel string
}

// Constants only need re-pushing often enough that the store keeps the series
// live for instant queries and changes(); a minute is plenty for that, and
// there is nothing to gain from matching the memory collector's cadence.
const defaultProcessInfoInterval = time.Minute

// NewProcessInfo creates a ProcessInfo collector describing this binary.
// Writer may be nil for environments without metrics collection, in which
// case Monitor is a no-op.
func NewProcessInfo(log *slog.Logger, writer PointWriter) *ProcessInfo {
	return &ProcessInfo{
		Log:       log,
		Writer:    writer,
		Entity:    "miren/control",
		StartTime: processStart,
		Version:   version.Version,
		Commit:    version.Commit,
		Channel:   version.Branch(),
	}
}

// Monitor pushes the identity series immediately and then once per
// defaultProcessInfoInterval until ctx is cancelled. The first push is not
// gated on the ticker so a process that restarts faster than the interval
// still records the new start time in the embedded store. The sink that
// ships off-cluster attaches later in boot and does not see that push; the
// boot stage that attaches it calls Emit so the central store gets the same
// coverage.
func (p *ProcessInfo) Monitor(ctx context.Context) {
	if p.Writer == nil {
		return
	}

	p.Log.Info("control-process identity metrics started",
		"entity", p.Entity, "version", p.Version, "commit", p.Commit, "channel", p.Channel,
		"started", p.StartTime.UTC().Format(time.RFC3339), "interval", defaultProcessInfoInterval)

	ticker := time.NewTicker(defaultProcessInfoInterval)
	defer ticker.Stop()

	for {
		if err := p.Emit(ctx); err != nil {
			p.Log.Error("failed to record control-process identity", "err", err)
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			return
		}
	}
}

// Emit pushes one sample of each identity series. Both are constants, so an
// extra push between ticks is harmless; callers use it to prime a sink that
// attached after the last one. A nil Writer is a no-op.
func (p *ProcessInfo) Emit(ctx context.Context) error {
	if p == nil || p.Writer == nil {
		return nil
	}
	ts := time.Now()

	// The *_build_info idiom: an always-1 gauge whose labels are the payload,
	// so queries join against it rather than read its value.
	buildLabels := map[string]string{
		"entity":  p.Entity,
		"version": p.Version,
		"commit":  p.Commit,
	}
	if p.Channel != "" {
		buildLabels["channel"] = p.Channel
	}

	points := []MetricPoint{
		{
			Name:      "process_start_time_seconds",
			Labels:    map[string]string{"entity": p.Entity},
			Value:     float64(p.StartTime.Unix()),
			Timestamp: ts,
		},
		{
			Name:      "miren_build_info",
			Labels:    buildLabels,
			Value:     1,
			Timestamp: ts,
		},
	}
	return p.Writer.WritePoints(ctx, points)
}
