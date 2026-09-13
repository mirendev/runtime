package metrics

import (
	"context"
	"errors"
	"sync"
)

// PointWriter is the sink the runtime's operational collectors emit to. The
// collectors only ever batch-write, so this is the whole surface they need;
// keeping it this narrow is what lets a collector be pointed at one store, a
// fan-out of several, or a labeling wrapper without knowing which.
type PointWriter interface {
	WritePoints(ctx context.Context, points []MetricPoint) error
}

// Fanout delivers every batch to each attached sink.
//
// Sinks can be attached after collectors have started emitting. The runtime's
// operational gauges (control-process memory, etcd health, node usage) start
// early in boot and write to the embedded VictoriaMetrics from their first
// tick, while the sink that ships them off the cluster only exists once the
// managed-metrics vmagent is up, several boot stages later. Attaching late is
// how that sink joins without the collectors being restarted or made aware of
// boot order.
type Fanout struct {
	mu    sync.RWMutex
	sinks []PointWriter
}

// NewFanout creates a Fanout that starts with the given sinks.
func NewFanout(sinks ...PointWriter) *Fanout {
	f := &Fanout{}
	for _, sink := range sinks {
		f.Attach(sink)
	}
	return f
}

// Attach adds a sink. Batches written after Attach returns reach it; a batch
// already in flight may not. A nil sink is ignored.
func (f *Fanout) Attach(sink PointWriter) {
	if sink == nil {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sinks = append(f.sinks, sink)
}

// Detach removes a sink. It takes the write lock, and WritePoints holds the
// read lock for the whole delivery, so once Detach returns no batch is still
// being handed to the removed sink; the caller can close it without a late
// write landing after its final flush. Detaching a sink that was never
// attached is a no-op.
func (f *Fanout) Detach(sink PointWriter) {
	f.mu.Lock()
	defer f.mu.Unlock()
	remaining := make([]PointWriter, 0, len(f.sinks))
	for _, existing := range f.sinks {
		if existing != sink {
			remaining = append(remaining, existing)
		}
	}
	f.sinks = remaining
}

// WritePoints hands the batch to every sink. One sink failing does not stop
// the others from receiving the batch; all failures are returned joined.
//
// The read lock is held across delivery, not just while reading the sink
// list. Sinks only buffer (the writers flush on their own goroutines), so the
// hold is brief, and it is what gives Detach its guarantee. The corollary is
// that a sink must not call Attach or Detach from inside its own WritePoints;
// those are lifecycle operations for whoever owns the sink, and a sink that
// tried to detach itself mid-delivery would be waiting on its own read lock.
func (f *Fanout) WritePoints(ctx context.Context, points []MetricPoint) error {
	if f == nil {
		return nil
	}
	f.mu.RLock()
	defer f.mu.RUnlock()

	var errs []error
	for _, sink := range f.sinks {
		if err := sink.WritePoints(ctx, points); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// Labeled stamps constant labels onto every point before passing it on.
//
// This is how series pooled from many clusters into one store stay distinct.
// The collectors themselves carry no cluster identity, because inside a
// cluster's own store the cluster is implicit; the identity belongs to the
// shipping path, so it is applied by the sink that ships rather than by every
// emitter. A label the point already carries is kept, so a collector that
// knows its own runner is not overwritten by a process-wide default.
//
// Label names are compared after the same sanitization the writer applies when
// formatting, so a collector's "miren.runner" and a constant "miren_runner" are
// recognized as the same label rather than emitted twice.
type Labeled struct {
	Sink   PointWriter
	Labels map[string]string
}

// WritePoints copies each point's labels, merging in the constant labels, and
// writes the result to the wrapped sink. The caller's points are not mutated;
// collectors share one label map across every point in a batch.
func (l *Labeled) WritePoints(ctx context.Context, points []MetricPoint) error {
	if l == nil || l.Sink == nil {
		return nil
	}
	if len(l.Labels) == 0 {
		return l.Sink.WritePoints(ctx, points)
	}

	labeled := make([]MetricPoint, len(points))
	for i, point := range points {
		merged := make(map[string]string, len(point.Labels)+len(l.Labels))
		for name, value := range point.Labels {
			merged[sanitizeLabelName(name)] = value
		}
		for name, value := range l.Labels {
			name = sanitizeLabelName(name)
			if _, present := merged[name]; !present {
				merged[name] = value
			}
		}
		point.Labels = merged
		labeled[i] = point
	}
	return l.Sink.WritePoints(ctx, labeled)
}
