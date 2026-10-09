package query

import (
	"context"
	"errors"
	"runtime"
	"time"
)

// EventSource supplies raw events for an event-mode request.
type EventSource func(context.Context, MonitorRequest, func(Event) error) error

// SnapshotSource collects one snapshot for a snapshot-mode request.
type SnapshotSource func(context.Context, MonitorRequest) (Snapshot, error)

// Engine executes local queries. Nil sources use the built-in collectors.
type Engine struct {
	Events    EventSource
	Snapshots SnapshotSource
	Sources   map[string]CustomSource // engine-local names; built-in names are reserved
}

func (e Engine) eventSource() EventSource {
	return func(ctx context.Context, r MonitorRequest, emit func(Event) error) error {
		if r.customSource != nil {
			return r.customSource.Events(ctx, r, emit)
		}
		if e.Events != nil {
			return e.Events(ctx, r, emit)
		}
		return CollectEvents(ctx, r, emit)
	}
}

func (e Engine) snapshotSource() SnapshotSource {
	return func(ctx context.Context, r MonitorRequest) (Snapshot, error) {
		if r.customSource != nil {
			return r.customSource.Snapshots(ctx, r)
		}
		if e.Snapshots != nil {
			return e.Snapshots(ctx, r)
		}
		return querySnapshot(ctx, r)
	}
}

// ParseMonitorQuery compiles the DSL with this engine's custom sources.
func (e Engine) ParseMonitorQuery(text string) (MonitorRequest, error) {
	return parseMonitorQuery(text, e.Sources)
}

// Validate validates a request against this engine's registered sources.
func (e Engine) Validate(request MonitorRequest) error {
	return e.bindSources(request).Validate()
}

// Metadata returns field metadata including this engine's custom sources.
func (e Engine) Metadata(request MonitorRequest) (SourceMetadata, error) {
	return Metadata(e.bindSources(request))
}

// Query normalizes query mode, resolves syscall names for the native ABI, and
// dispatches snapshots, sampled aggregates, and event aggregates.
func (e Engine) Query(ctx context.Context, request MonitorRequest) (Snapshot, error) {
	request = e.bindSources(request)
	if request.Mode != "aggregate" {
		request.Mode = "snapshot"
	}
	if request.Aggregation != nil {
		request.Mode = "aggregate"
	}
	if err := request.Validate(); err != nil {
		return Snapshot{}, err
	}
	request, err := ResolveSyscallNames(request, runtime.GOARCH)
	if err != nil {
		return Snapshot{}, err
	}
	if request.Source == "script" {
		return aggregateScript(ctx, request, e.eventSource(), e.snapshotSource())
	}
	if request.Mode == "snapshot" {
		collect, err := correlatedCollector(ctx, request, e.snapshotSource())
		if err != nil {
			return Snapshot{}, err
		}
		return collect(ctx, request)
	}
	if sampledAggregation(request) {
		return aggregateSnapshots(ctx, request, e.snapshotSource())
	}
	return aggregateEvents(ctx, request, e.eventSource())
}

// Monitor validates and streams filtered, timestamped events.
func (e Engine) Monitor(ctx context.Context, request MonitorRequest, onEvent func(Event) error) error {
	request = e.bindSources(request)
	if request.Mode != "" {
		return errors.New("use Engine.Query for query modes")
	}
	if onEvent == nil {
		return errors.New("event callback required")
	}
	if err := request.Validate(); err != nil {
		return err
	}
	request, err := ResolveSyscallNames(request, runtime.GOARCH)
	if err != nil {
		return err
	}
	var last time.Time
	return e.eventSource()(ctx, request, func(event Event) error {
		if !request.Matches(event) {
			return nil
		}
		StampEvent(&event, &last)
		return onEvent(event)
	})
}
