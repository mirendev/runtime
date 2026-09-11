package metrics

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recordingSink struct {
	batches [][]MetricPoint
	err     error
}

func (s *recordingSink) WritePoints(_ context.Context, points []MetricPoint) error {
	s.batches = append(s.batches, points)
	return s.err
}

func TestFanoutDeliversToSinksAttachedLater(t *testing.T) {
	first := &recordingSink{}
	fanout := NewFanout(first)
	ctx := context.Background()

	require.NoError(t, fanout.WritePoints(ctx, []MetricPoint{{Name: "a", Value: 1}}))

	late := &recordingSink{}
	fanout.Attach(late)
	require.NoError(t, fanout.WritePoints(ctx, []MetricPoint{{Name: "b", Value: 2}}))

	require.Len(t, first.batches, 2, "original sink sees every batch")
	require.Len(t, late.batches, 1, "late sink sees only batches written after Attach")
	assert.Equal(t, "b", late.batches[0][0].Name)
}

func TestFanoutOneFailingSinkDoesNotStarveOthers(t *testing.T) {
	failing := &recordingSink{err: errors.New("remote down")}
	healthy := &recordingSink{}
	fanout := NewFanout(failing, healthy)

	err := fanout.WritePoints(context.Background(), []MetricPoint{{Name: "a", Value: 1}})
	require.ErrorContains(t, err, "remote down")
	require.Len(t, healthy.batches, 1)
}

// blockingSink parks inside WritePoints until released, standing in for a
// delivery that is mid-flight when Detach is called.
type blockingSink struct {
	entered chan struct{}
	release chan struct{}
	writes  int
}

func (s *blockingSink) WritePoints(context.Context, []MetricPoint) error {
	s.writes++
	close(s.entered)
	<-s.release
	return nil
}

func TestFanoutDetachWaitsForInFlightWrite(t *testing.T) {
	sink := &blockingSink{entered: make(chan struct{}), release: make(chan struct{})}
	fanout := NewFanout(sink)

	writeDone := make(chan struct{})
	go func() {
		defer close(writeDone)
		_ = fanout.WritePoints(context.Background(), []MetricPoint{{Name: "a", Value: 1}})
	}()
	<-sink.entered

	detached := make(chan struct{})
	go func() {
		defer close(detached)
		fanout.Detach(sink)
	}()

	select {
	case <-detached:
		t.Fatal("Detach returned while a write to the sink was still in flight")
	case <-time.After(50 * time.Millisecond):
	}

	close(sink.release)
	<-writeDone
	select {
	case <-detached:
	case <-time.After(time.Second):
		t.Fatal("Detach did not return after the in-flight write finished")
	}

	require.NoError(t, fanout.WritePoints(context.Background(), []MetricPoint{{Name: "b", Value: 2}}))
	assert.Equal(t, 1, sink.writes, "no batch reaches a sink after Detach returns")
}

func TestFanoutIgnoresNilSinks(t *testing.T) {
	fanout := NewFanout(nil)
	fanout.Attach(nil)
	require.NoError(t, fanout.WritePoints(context.Background(), []MetricPoint{{Name: "a", Value: 1}}))

	var none *Fanout
	require.NoError(t, none.WritePoints(context.Background(), nil))
}

func TestLabeledStampsConstantLabelsWithoutMutatingInput(t *testing.T) {
	sink := &recordingSink{}
	labeled := &Labeled{
		Sink:   sink,
		Labels: map[string]string{"miren_cluster": "c1", "miren_runner": "node-a"},
	}

	// Collectors share one label map across a batch; the shipped copy must not
	// leak constant labels back into it.
	shared := map[string]string{"entity": "miren/control"}
	ts := time.Now()
	points := []MetricPoint{
		{Name: "go_goroutines", Labels: shared, Value: 10, Timestamp: ts},
		{Name: "go_mem_sys_bytes", Labels: shared, Value: 20, Timestamp: ts},
		{Name: "etcd_db_size_bytes", Value: 30, Timestamp: ts},
	}
	require.NoError(t, labeled.WritePoints(context.Background(), points))

	require.Len(t, sink.batches, 1)
	got := sink.batches[0]
	require.Len(t, got, 3)
	for _, point := range got[:2] {
		assert.Equal(t, map[string]string{
			"entity":        "miren/control",
			"miren_cluster": "c1",
			"miren_runner":  "node-a",
		}, point.Labels)
	}
	assert.Equal(t, map[string]string{"miren_cluster": "c1", "miren_runner": "node-a"}, got[2].Labels)
	assert.Equal(t, 30.0, got[2].Value)
	assert.Equal(t, ts, got[2].Timestamp)

	assert.Equal(t, map[string]string{"entity": "miren/control"}, shared, "caller's labels are untouched")
	assert.Nil(t, points[2].Labels, "caller's nil labels stay nil")
}

func TestLabeledKeepsCollectorLabelsAcrossSanitization(t *testing.T) {
	sink := &recordingSink{}
	labeled := &Labeled{
		Sink:   sink,
		Labels: map[string]string{"miren_runner": "process-default", "miren_cluster": "c1"},
	}

	// NodeUsage emits "miren.runner", which the writer formats as
	// "miren_runner". The collector's value has to win and appear once.
	points := []MetricPoint{{
		Name:   "node_load1",
		Labels: map[string]string{"miren.node": "n1", "miren.runner": "node-a"},
		Value:  0.5,
	}}
	require.NoError(t, labeled.WritePoints(context.Background(), points))

	require.Len(t, sink.batches, 1)
	assert.Equal(t, map[string]string{
		"miren_node":    "n1",
		"miren_runner":  "node-a",
		"miren_cluster": "c1",
	}, sink.batches[0][0].Labels)
}

func TestLabeledWithoutLabelsPassesThrough(t *testing.T) {
	sink := &recordingSink{}
	labeled := &Labeled{Sink: sink}
	points := []MetricPoint{{Name: "a", Value: 1}}
	require.NoError(t, labeled.WritePoints(context.Background(), points))
	require.Len(t, sink.batches, 1)
	assert.Equal(t, points, sink.batches[0])

	var none *Labeled
	require.NoError(t, none.WritePoints(context.Background(), points))
	require.NoError(t, (&Labeled{}).WritePoints(context.Background(), points))
}
