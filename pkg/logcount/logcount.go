// Package logcount counts the log lines a miren process prints, by level and
// by source, so the error rate can be shipped as a metric and alerted on
// instead of read out of a journal nobody collects.
//
// Counting happens in a slog.Handler that wraps the process's root handler,
// so it sees exactly the records that get printed. Output relayed from child
// processes (vmagent, etcd, buildkit and friends) passes through the same
// handler, but it is counted under the child's name and at the child's own
// level, which the relay may have clamped for display. Without that split an
// etcd error would count as a miren info line, and a burst of vmagent
// failures would be invisible.
package logcount

import (
	"context"
	"log/slog"
	"sync/atomic"
)

// Source is who wrote a counted line: miren itself, or a relayed child.
type Source int

// The relayed sources are a fixed set, so the metric's source label stays
// bounded no matter what a caller passes to SourceFor.
const (
	SourceMiren Source = iota
	SourceBuildkit
	SourceContainerd
	SourceEtcd
	SourceEtcdutl
	SourceVictoriaLogs
	SourceVictoriaMetrics
	SourceVMAgent
	SourceOther
	numSources
)

var sourceNames = [numSources]string{
	SourceMiren:           "miren",
	SourceBuildkit:        "buildkit",
	SourceContainerd:      "containerd",
	SourceEtcd:            "etcd",
	SourceEtcdutl:         "etcdutl",
	SourceVictoriaLogs:    "victorialogs",
	SourceVictoriaMetrics: "victoriametrics",
	SourceVMAgent:         "vmagent",
	SourceOther:           "other",
}

func (s Source) String() string { return sourceNames[s] }

// SourceFor maps a relayed child's name to its Source. Names outside the
// known set become SourceOther rather than a new label value.
func SourceFor(name string) Source {
	for s := SourceBuildkit; s < SourceOther; s++ {
		if sourceNames[s] == name {
			return s
		}
	}
	return SourceOther
}

const numLevels = 4

// Levels are the label values for the four buckets a counted line lands in.
var Levels = [numLevels]string{"debug", "info", "warn", "error"}

// levelIndex buckets any slog level into the four named ones, so Trace and
// custom levels in between still land somewhere sensible.
func levelIndex(l slog.Level) int {
	switch {
	case l < slog.LevelInfo:
		return 0
	case l < slog.LevelWarn:
		return 1
	case l < slog.LevelError:
		return 2
	default:
		return 3
	}
}

// Counts holds cumulative line counts for one process.
type Counts struct {
	cells [numSources][numLevels]atomic.Uint64
}

// Default is the process-wide set that the CLI's root logger counts into and
// the metrics collectors read from.
var Default = &Counts{}

func (c *Counts) add(s Source, l slog.Level) {
	c.cells[s][levelIndex(l)].Add(1)
}

// Load returns the count for one source at one of the four Levels.
func (c *Counts) Load(s Source, level int) uint64 {
	return c.cells[s][level].Load()
}

// NumSources is the number of distinct Source values, for iterating Counts.
const NumSources = int(numSources)

type relayKey struct{}

type relay struct {
	source Source
	level  slog.Level
}

// Relayed marks ctx so that a record logged with it is counted under source
// at level, rather than as miren's own line at whatever level it was logged.
// Relays use it to count a child's line at the child's level even when they
// clamp the level they print.
func Relayed(ctx context.Context, source Source, level slog.Level) context.Context {
	return context.WithValue(ctx, relayKey{}, relay{source: source, level: level})
}

// CountAt marks ctx so that a record logged with it is counted as miren's
// own line at level, whatever level it prints at. It's for lines that log
// loud only so they always print, like a log level change, and would
// otherwise count as errors.
func CountAt(ctx context.Context, level slog.Level) context.Context {
	return Relayed(ctx, SourceMiren, level)
}

// Handler counts every record it handles into Counts, then passes it on.
type Handler struct {
	inner  slog.Handler
	counts *Counts
}

// NewHandler wraps inner so that its records are counted into counts.
func NewHandler(inner slog.Handler, counts *Counts) *Handler {
	return &Handler{inner: inner, counts: counts}
}

// Enabled defers to the wrapped handler, so records below its level are never
// built and never counted. The counter reports what was printed.
func (h *Handler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h *Handler) Handle(ctx context.Context, r slog.Record) error {
	if rel, ok := ctx.Value(relayKey{}).(relay); ok {
		h.counts.add(rel.source, rel.level)
	} else {
		h.counts.add(SourceMiren, r.Level)
	}
	return h.inner.Handle(ctx, r)
}

func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &Handler{inner: h.inner.WithAttrs(attrs), counts: h.counts}
}

func (h *Handler) WithGroup(name string) slog.Handler {
	return &Handler{inner: h.inner.WithGroup(name), counts: h.counts}
}
