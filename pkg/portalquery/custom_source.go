package query

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
)

// CustomSource defines an integration's source. Fields are the flat keys in
// Event.Fields; NumericFields is the subset usable in numeric aggregations and
// comparisons. Events and Snapshots independently enable streaming/aggregation
// and one-shot snapshots. Snapshot collectors apply Filters themselves.
// Configure Sources before using an Engine and do not mutate it while in use.
type CustomSource struct {
	Fields        []string
	NumericFields []string
	Events        EventSource
	Snapshots     SnapshotSource
}

// SourceFilter is an equality or membership predicate for a custom field.
// Values are textual DSL values, compared to fmt.Sprint of the event field.
type SourceFilter struct {
	Field  string   `json:"field"`
	Values []string `json:"values"`
}

func (e Engine) bindSources(r MonitorRequest) MonitorRequest {
	r.customSource = nil
	if source, ok := e.Sources[r.Source]; ok && !builtInSource(r.Source) {
		r.customSource = &source
	}
	r.Probes = slices.Clone(r.Probes)
	for i := range r.Probes {
		r.Probes[i] = e.bindSources(r.Probes[i])
	}
	if r.Using != nil {
		using := *r.Using
		using.Inventory = e.bindSources(using.Inventory)
		r.Using = &using
	}
	return r
}

func builtInSource(name string) bool {
	switch name {
	case "script", "syscalls", "packets", "process", "disk", "tracepoint", "cpu", "memory", "network", "kernel", "sensors", "containers", "cgroups", "gpu", "capabilities", "symbols":
		return true
	}
	return false
}

func (r MonitorRequest) validateCustomSource() error {
	s := r.customSource
	selection := r
	selection.Source, selection.Mode, selection.Aggregation = "", "", nil
	selection.Filters, selection.Comparisons, selection.customSource = nil, nil, nil
	if !reflect.DeepEqual(selection, MonitorRequest{}) {
		return errors.New("custom sources do not support built-in filters or probes")
	}
	if r.Mode == "snapshot" {
		if s.Snapshots == nil || len(r.Comparisons) != 0 {
			return errors.New("custom snapshot source unavailable or has event comparisons")
		}
	} else if s.Events == nil {
		return errors.New("custom event source unavailable")
	}
	for _, field := range s.NumericFields {
		if !slices.Contains(s.Fields, field) {
			return fmt.Errorf("numeric field %q is not declared in source fields", field)
		}
	}
	seen := make(map[string]bool)
	for _, filter := range r.Filters {
		if !slices.Contains(s.Fields, filter.Field) || len(filter.Values) == 0 || seen[filter.Field] {
			return fmt.Errorf("invalid or duplicate custom filter %q", filter.Field)
		}
		seen[filter.Field] = true
	}
	return nil
}
