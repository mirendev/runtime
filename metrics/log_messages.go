package metrics

import (
	"context"
	"log/slog"
	"time"

	"miren.dev/runtime/pkg/logcount"
)

// LogMessages publishes how many log lines this process has printed, as
// miren_log_messages_total{level, source}, so the error rate of every cluster
// can be seen and alerted on from the central store, whether or not anything
// collects its journal. It is the runtime's counterpart to VictoriaMetrics'
// own vm_log_messages_total.
//
// source is "miren" for the process's own lines, or the name of the child
// process whose output was relayed (vmagent, etcd, buildkit, ...). A relayed
// line counts at the child's own level, which is often higher than the one it
// was printed at, since relays clamp children to Info for display.
//
// Only printed lines are counted, so the debug count moves with -v and says
// nothing on its own. Warn and error are always printed by a daemon at its
// default level, and they are what the alert rules read.
type LogMessages struct {
	Log    *slog.Logger
	Writer PointWriter
	Counts *logcount.Counts

	// Entity is the value of the "entity" label on every emitted series.
	Entity string
}

const defaultLogMessagesInterval = 10 * time.Second

// NewLogMessages creates a collector for the process-wide logcount.Default.
// Writer may be nil for environments without metrics collection, in which
// case Monitor is a no-op.
func NewLogMessages(log *slog.Logger, writer PointWriter) *LogMessages {
	return &LogMessages{
		Log:    log,
		Writer: writer,
		Counts: logcount.Default,
		Entity: EntityControl,
	}
}

// Monitor pushes the counters every defaultLogMessagesInterval until ctx is
// cancelled.
func (l *LogMessages) Monitor(ctx context.Context) {
	if l.Writer == nil {
		return
	}

	ticker := time.NewTicker(defaultLogMessagesInterval)
	defer ticker.Stop()

	for {
		if err := l.Emit(ctx, time.Now()); err != nil {
			l.Log.Debug("failed to record log message counts", "entity", l.Entity, "err", err)
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			return
		}
	}
}

// Emit pushes one sample of every counter worth reporting. A source's four
// levels are sent together, zeros included, so an error series usually has a
// zero baseline before its first error; otherwise an error after a restart
// would continue the old series at the same value and increase() would miss
// it. The baseline can't cover errors logged before the first push, which
// waits on the embedded VictoriaMetrics: those land in the first sample, so
// increase() undercounts boot-time errors across a restart. miren is always
// sent. A relayed source is sent once it has printed something, which
// children do at startup, and which keeps a runner without etcd or vmagent
// from carrying their empty series.
func (l *LogMessages) Emit(ctx context.Context, now time.Time) error {
	var points []MetricPoint
	for s := range logcount.NumSources {
		source := logcount.Source(s)
		if source != logcount.SourceMiren && !l.active(source) {
			continue
		}
		for level, name := range logcount.Levels {
			n := l.Counts.Load(source, level)
			points = append(points, MetricPoint{
				Name: "miren_log_messages_total",
				Labels: map[string]string{
					"entity": l.Entity,
					"level":  name,
					"source": source.String(),
				},
				Value:     float64(n),
				Timestamp: now,
			})
		}
	}
	return l.Writer.WritePoints(ctx, points)
}

func (l *LogMessages) active(source logcount.Source) bool {
	for level := range logcount.Levels {
		if l.Counts.Load(source, level) > 0 {
			return true
		}
	}
	return false
}
