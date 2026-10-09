package query

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"path"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
)

const maxAggregateGroups = 4096
const maxAggregateValues = 65536
const maxAggregateMetrics = 8

// AggregateMetric selects one reduction in a shared window and grouping.
type AggregateMetric struct {
	Name       string  `json:"name,omitempty"`
	Function   string  `json:"function"`
	Field      string  `json:"field,omitempty"`
	Percentile float64 `json:"percentile,omitempty"`
}

// AggregationRequest reduces matching events or sampled snapshots during a new window.
// An omitted Function selects count for compatibility.
type AggregationRequest struct {
	Window       time.Duration     `json:"window"` // nanoseconds
	GroupBy      []string          `json:"group_by,omitempty"`
	Function     string            `json:"function,omitempty"` // count, sum, avg, min, max, count_distinct, percentile, hist, rate
	Field        string            `json:"field,omitempty"`
	Percentile   float64           `json:"percentile,omitempty"` // 0–100, only for percentile
	Every        time.Duration     `json:"every,omitempty"`      // Snapshot sampling interval; zero defaults to 1s for snapshot-only sources.
	Metrics      []AggregateMetric `json:"metrics,omitempty"`    // Alternative to Function/Field/Percentile; all share Window/Every/GroupBy.
	Compact      bool              `json:"compact,omitempty"`
	Limit        int               `json:"limit,omitempty"`       // first rows in SortMetric order; zero is unlimited
	Nonzero      bool              `json:"nonzero,omitempty"`     // omit rows whose every metric is numeric zero
	SortMetric   int               `json:"sort_metric,omitempty"` // zero-based metric index, default first
	Ascending    bool              `json:"ascending,omitempty"`   // compact rows sorted low-to-high; default descending, nulls always last
	Table        string            `json:"table,omitempty"`
	GroupAliases map[string]string `json:"group_aliases,omitempty"`
	ReportEvery  time.Duration     `json:"report_every,omitempty"` // finite tumbling event buckets; Window is total observation duration
}

type AggregateCount struct {
	Group map[string]json.RawMessage `json:"group"`
	Count uint64                     `json:"count"`
}

type AggregateValue struct {
	Group map[string]json.RawMessage `json:"group"`
	Value json.RawMessage            `json:"value"` // JSON number, histogram object, or null
}

// Histogram bounds are in the field's native units, lower inclusive/upper exclusive.
// Zero is counted separately; only occupied buckets are returned.
type HistogramBucket struct {
	Lower json.Number `json:"lower"`
	Upper json.Number `json:"upper"`
	Count uint64      `json:"count"`
}

type Histogram struct {
	Count     uint64            `json:"count"`
	ZeroCount uint64            `json:"zero_count"`
	Buckets   []HistogramBucket `json:"buckets"`
}

type AggregateRow struct {
	Group  map[string]json.RawMessage `json:"group"`
	Values []json.RawMessage          `json:"values"` // Go values align with Columns; named table JSON uses a metric-name object, legacy JSON an array
}

// AggregationResult describes a half-open server ingestion window [Start, End).
type AggregationResult struct {
	Table                string                          `json:"table,omitempty"`
	Join                 *AggregateReport                `json:"join,omitempty"`
	Start                time.Time                       `json:"start"`
	End                  time.Time                       `json:"end"`
	GroupBy              []string                        `json:"group_by"`
	Counts               []AggregateCount                `json:"counts,omitempty"`
	Function             string                          `json:"function,omitempty"`
	Field                string                          `json:"field,omitempty"`
	Percentile           float64                         `json:"percentile,omitempty"`
	Values               []AggregateValue                `json:"values,omitempty"`
	Every                time.Duration                   `json:"every,omitempty"`
	Collection           *CollectionStats                `json:"collection,omitempty"`
	Collections          map[string]*CollectionStats     `json:"collections,omitempty"` // join: separate source diagnostics, never summed
	StackCoverage        *StackCoverageReport            `json:"stack_coverage,omitempty"`
	StackCoverageByTable map[string]*StackCoverageReport `json:"stack_coverage_by_table,omitempty"` // join: preserve each source's received-event coverage
	Metrics              []*AggregationResult            `json:"metrics,omitempty"`                 // In request order; each has the same window/grouping. Single queries retain their old shape.
	Columns              []AggregateMetric               `json:"columns,omitempty"`
	Rows                 []AggregateRow                  `json:"rows,omitempty"`
	TotalGroups          int                             `json:"total_groups,omitempty"`
	OmittedZeroGroups    int                             `json:"omitted_zero_groups,omitempty"`
	rollups              map[string]*AggregationResult   // execution-only coarse reductions; never sent on the wire
}

// Keep the selected result collection visible even for an empty grouped window.
func (a AggregationResult) MarshalJSON() ([]byte, error) {
	type fields AggregationResult
	if a.Columns != nil {
		var rows any = a.Rows
		if a.Table != "" {
			named := make([]map[string]any, 0, len(a.Rows))
			for _, row := range a.Rows {
				if len(row.Values) != len(a.Columns) {
					return nil, errors.New("aggregate row values must align with columns")
				}
				values := make(map[string]any, len(a.Columns))
				for i, column := range a.Columns {
					if a.Join == nil || column.Function == "computed" {
						values[column.Name] = row.Values[i]
					} else {
						table, metric, _ := strings.Cut(column.Name, ".")
						if values[table] == nil {
							values[table] = make(map[string]json.RawMessage)
						}
						values[table].(map[string]json.RawMessage)[metric] = row.Values[i]
					}
				}
				named = append(named, map[string]any{"group": row.Group, "values": values})
			}
			rows = named
		}
		return json.Marshal(struct {
			Table                string                          `json:"table,omitempty"`
			Join                 *AggregateReport                `json:"join,omitempty"`
			Start                time.Time                       `json:"start"`
			End                  time.Time                       `json:"end"`
			GroupBy              []string                        `json:"group_by"`
			Every                time.Duration                   `json:"every,omitempty"`
			Collection           *CollectionStats                `json:"collection,omitempty"`
			Collections          map[string]*CollectionStats     `json:"collections,omitempty"`
			StackCoverage        *StackCoverageReport            `json:"stack_coverage,omitempty"`
			StackCoverageByTable map[string]*StackCoverageReport `json:"stack_coverage_by_table,omitempty"`
			Columns              []AggregateMetric               `json:"columns"`
			Rows                 any                             `json:"rows"`
			TotalGroups          int                             `json:"total_groups"`
			OmittedZeroGroups    int                             `json:"omitted_zero_groups,omitempty"`
		}{a.Table, a.Join, a.Start, a.End, a.GroupBy, a.Every, a.Collection, a.Collections, a.StackCoverage, a.StackCoverageByTable, a.Columns, rows, a.TotalGroups, a.OmittedZeroGroups})
	}
	if len(a.Metrics) != 0 {
		return json.Marshal(fields(a))
	}
	if a.Function == "" || a.Function == "count" {
		return json.Marshal(struct {
			fields
			Counts []AggregateCount `json:"counts"`
		}{fields(a), a.Counts})
	}
	return json.Marshal(struct {
		fields
		Values []AggregateValue `json:"values"`
	}{fields(a), a.Values})
}

// Decode both row wire formats into the same column-aligned Go representation.
func (a *AggregationResult) UnmarshalJSON(data []byte) error {
	type fields AggregationResult
	var result fields
	var wire struct {
		*fields
		Rows []struct {
			Group  map[string]json.RawMessage `json:"group"`
			Values json.RawMessage            `json:"values"`
		} `json:"rows"`
	}
	wire.fields = &result
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	if wire.Rows != nil {
		result.Rows = make([]AggregateRow, 0, len(wire.Rows))
	}
	for _, raw := range wire.Rows {
		row := AggregateRow{Group: raw.Group}
		if len(raw.Values) != 0 && raw.Values[0] == '{' {
			var named map[string]json.RawMessage
			if err := json.Unmarshal(raw.Values, &named); err != nil {
				return err
			}
			if result.Join != nil {
				flat := make(map[string]json.RawMessage)
				for table, data := range named {
					if slices.ContainsFunc(result.Columns, func(c AggregateMetric) bool { return c.Name == table && c.Function == "computed" }) {
						flat[table] = data
						continue
					}
					var metrics map[string]json.RawMessage
					if err := json.Unmarshal(data, &metrics); err != nil {
						return err
					}
					for metric, value := range metrics {
						flat[table+"."+metric] = value
					}
				}
				named = flat
			}
			if len(named) != len(result.Columns) {
				return errors.New("named aggregate row must match columns")
			}
			row.Values = make([]json.RawMessage, len(result.Columns))
			for i, column := range result.Columns {
				value, ok := named[column.Name]
				if !ok {
					return fmt.Errorf("aggregate row missing metric %q", column.Name)
				}
				row.Values[i] = value
			}
		} else if err := json.Unmarshal(raw.Values, &row.Values); err != nil {
			return err
		}
		result.Rows = append(result.Rows, row)
	}
	*a = AggregationResult(result)
	return nil
}

func aggregateFields(r MonitorRequest) (fields, numeric []string, err error) {
	if r.customSource != nil {
		return slices.Clone(r.customSource.Fields), slices.Clone(r.customSource.NumericFields), nil
	}
	if sampledAggregation(r) {
		fields, numeric = sampledFields(r.Source)
		if r.Using != nil && r.Using.Inventory.customSource != nil {
			for _, field := range r.Using.Inventory.customSource.Fields {
				fields = append(fields, "inventory."+field)
			}
		}
		return
	}
	switch r.Source {
	case "syscalls":
		fields = []string{"pid", "tid", "syscall"}
		numeric = append([]string{}, fields...)
		if r.Phase == "completion" {
			fields = append(fields, "duration_ns", "return_value")
			numeric = append(numeric, "duration_ns", "return_value")
		}
		if r.Paths {
			fields = append(fields, "file.fd", "file.path", "file.dir", "file.error")
			numeric = append(numeric, "file.fd")
		}
	case "process":
		fields = []string{"pid", "name", "action"}
		numeric = []string{"pid"}
	case "packets":
		fields = []string{"protocol", "direction", "src.ip", "dst.ip", "src.port", "dst.port", "length"}
		numeric = []string{"src.port", "dst.port", "length"}
	case "disk":
		fields = []string{"device", "device_name", "operation", "rwbs", "sector", "sectors", "io.cgroup.id", "io.cgroup.path", "io.cgroup.error", "request_flags"}
		numeric = []string{"device", "sector", "sectors", "io.cgroup.id", "request_flags"}
		if r.Phase == "completion" {
			fields = append(fields, "duration_ns", "status")
			numeric = append(numeric, "duration_ns", "status")
		}
	case "tracepoint":
		if r.Tracepoint != nil {
			for _, name := range r.Tracepoint.Fields {
				fields = append(fields, "field."+name)
			}
		}
		numeric = fields
	default:
		err = errors.New("aggregation requires an event source")
	}
	if r.Source == "syscalls" || r.Source == "disk" || r.Source == "tracepoint" {
		if r.Source != "syscalls" {
			fields = append(fields, "pid", "tid")
			numeric = append(append([]string{}, numeric...), "pid", "tid")
		}
		fields = append(fields, "name", "process_name", "name_group", "cgroup.path")
	}
	if r.Stacks != nil {
		if r.Stacks.User {
			fields = append(fields, "user.stack")
		}
		if r.Stacks.Kernel {
			fields = append(fields, "kernel.stack")
		}
	}
	return
}

func (a AggregationRequest) validate(r MonitorRequest) error {
	if a.Table != "" && (!tracepointIdentifier.MatchString(a.Table) || len(a.Table) > 64 || !a.Compact) {
		return errors.New("named tables require compact output and an identifier up to 64 bytes")
	}
	if a.ReportEvery != 0 {
		switch {
		case sampledAggregation(r):
			return fmt.Errorf("periodic reports require an event source; %s is a sampled snapshot source", r.Source)
		case a.ReportEvery < 100*time.Millisecond:
			return errors.New("periodic reporting interval must be at least 100ms")
		case a.ReportEvery > a.Window:
			return errors.New("periodic reporting interval cannot exceed the observation window")
		case (a.Window-1)/a.ReportEvery+1 > 64:
			return errors.New("periodic reports exceed the maximum of 64 buckets")
		}
	}
	names := make(map[string]bool)
	for _, field := range a.GroupBy {
		name := field
		if alias, ok := a.GroupAliases[field]; ok {
			if !tracepointIdentifier.MatchString(alias) || len(alias) > 64 {
				return errors.New("invalid group alias")
			}
			name = alias
		}
		if names[name] {
			return errors.New("duplicate output grouping name")
		}
		names[name] = true
	}
	for field := range a.GroupAliases {
		if !slices.Contains(a.GroupBy, field) {
			return errors.New("group alias must name a selected grouping field")
		}
	}
	if a.Limit < 0 || a.Limit > maxAggregateGroups || a.SortMetric < 0 || a.SortMetric >= max(1, len(a.Metrics)) {
		return errors.New("result limit must be 0–4096 and sort_metric must select an existing zero-based metric")
	}
	if len(a.Metrics) != 0 {
		if (a.Limit != 0 || a.Ascending || a.SortMetric != 0) && a.Metrics[a.SortMetric].Function == "hist" {
			return errors.New("histograms cannot be used as a sort metric; select a numeric metric")
		}
		if len(a.Metrics) > maxAggregateMetrics {
			return errors.New("aggregation supports at most 8 metrics")
		}
		if a.Function != "" || a.Field != "" || a.Percentile != 0 {
			return errors.New("metrics cannot be combined with function, field or percentile")
		}
		seen := make(map[AggregateMetric]bool)
		metricNames := make(map[string]bool)
		for _, metric := range a.Metrics {
			if a.Table != "" && metric.Name == "" {
				return errors.New("named tables require metric names")
			}
			if metric.Name != "" {
				if !tracepointIdentifier.MatchString(metric.Name) || len(metric.Name) > 64 || metricNames[metric.Name] {
					return errors.New("invalid or duplicate metric name")
				}
				metricNames[metric.Name] = true
			}
			if metric.Function == "" {
				metric.Function = "count"
			}
			// Column aliases do not make identical reductions distinct.
			metric.Name = ""
			if seen[metric] {
				return errors.New("duplicate aggregate metric")
			}
			seen[metric] = true
			one := a
			one.Metrics = nil
			one.SortMetric, one.Limit, one.Ascending = 0, 0, false
			one.Function, one.Field, one.Percentile = metric.Function, metric.Field, metric.Percentile
			if err := one.validate(r); err != nil {
				return err
			}
		}
		return nil
	}
	if a.Window <= 0 || a.Window > time.Hour {
		return errors.New("aggregation window must be positive and at most 1h")
	}
	if len(a.GroupBy) > 4 {
		return errors.New("aggregation supports at most 4 grouping fields")
	}
	if sampledAggregation(r) {
		interval := a.Every
		if interval == 0 {
			interval = DefaultSampleInterval
		}
		if interval < MinSampleInterval || interval > a.Window {
			return errors.New("sampling interval must be at least 100ms and no greater than the window")
		}
		for _, field := range snapshotSampleFields(r.Source) {
			if (field.Path == a.Field || slices.Contains(a.GroupBy, field.Path)) && (field.Semantics == "rate" || field.Semantics == "utilization" || a.Function == "rate") && interval >= a.Window {
				return errors.New("derived metrics require a window longer than the sampling interval")
			}
		}
	} else if a.Every != 0 {
		return errors.New("every requires a snapshot source")
	}
	fields, numeric, err := aggregateFields(r)
	if err != nil {
		return err
	}
	switch a.Function {
	case "", "count":
		if a.Field != "" {
			return errors.New("count does not accept a field")
		}
	case "count_distinct":
		if !slices.Contains(fields, a.Field) {
			return fmt.Errorf("invalid distinct field %q", a.Field)
		}
	case "rate":
		if !sampledAggregation(r) || !slices.ContainsFunc(snapshotSampleFields(r.Source), func(f SampleField) bool { return f.Path == a.Field && f.Semantics == "counter" }) {
			return fmt.Errorf("rate requires a cumulative counter on a sampled source, got %q", a.Field)
		}
	case "sum", "avg", "min", "max", "percentile", "hist":
		if !slices.Contains(numeric, a.Field) {
			return fmt.Errorf("aggregation requires a numeric gauge or derived field, got %q", a.Field)
		}
		if a.Function == "hist" && (a.Limit != 0 || a.Ascending) {
			return errors.New("histograms cannot be used as a sort metric; select a numeric metric")
		}
	default:
		return fmt.Errorf("unsupported aggregation function %q", a.Function)
	}
	if math.IsNaN(a.Percentile) || math.IsInf(a.Percentile, 0) || a.Percentile < 0 || a.Percentile > 100 || (a.Function != "percentile" && a.Percentile != 0) {
		return errors.New("percentile must be between 0 and 100 and used only with percentile")
	}
	seen := make(map[string]bool)
	for _, field := range a.GroupBy {
		if seen[field] || !slices.Contains(fields, field) {
			return fmt.Errorf("invalid or duplicate aggregation field %q for %s", field, r.Source)
		}
		seen[field] = true
	}
	return nil
}

func eventGroupFields(event Event, stacks *StackCapture) map[string]any {
	if event.Fields != nil {
		return event.Fields
	}
	var fields map[string]any
	switch {
	case event.Process != nil:
		fields = map[string]any{"pid": event.PID, "name": event.Process.Name, "action": event.Process.Action}
	case event.Packet != nil:
		p := event.Packet
		fields = map[string]any{"protocol": p.Protocol, "direction": p.Direction, "src.ip": p.SourceIP, "dst.ip": p.DestinationIP, "src.port": p.SourcePort, "dst.port": p.DestinationPort, "length": p.Length}
	case event.Disk != nil:
		fields = map[string]any{"device": event.Disk.Device, "operation": event.Disk.Operation, "rwbs": event.Disk.RWBS, "sector": event.Disk.Sector, "sectors": event.Disk.Sectors}
		if event.Disk.DeviceName != "" {
			fields["device_name"] = event.Disk.DeviceName
		}
		if event.Disk.IOCgroupID != nil {
			fields["io.cgroup.id"] = *event.Disk.IOCgroupID
		}
		if event.Disk.IOCgroupPath != "" {
			fields["io.cgroup.path"] = event.Disk.IOCgroupPath
		}
		if event.Disk.IOCgroupError != "" {
			fields["io.cgroup.error"] = event.Disk.IOCgroupError
		}
		if event.Disk.RequestFlags != nil {
			fields["request_flags"] = *event.Disk.RequestFlags
		}
		if event.Disk.DurationNS != nil {
			fields["duration_ns"] = *event.Disk.DurationNS
		}
		if event.Disk.Status != nil {
			fields["status"] = *event.Disk.Status
		}
	case event.Tracepoint != nil:
		fields = make(map[string]any, len(event.Tracepoint.Fields))
		for name, value := range event.Tracepoint.Fields {
			fields["field."+name] = value
		}
	default:
		fields = map[string]any{"pid": event.PID, "tid": event.TID, "syscall": event.Syscall}
	}
	if event.UserStack != nil {
		var shape *StackShape
		if stacks != nil {
			shape = stacks.UserShape
		}
		fields["user.stack"] = event.UserStack.key(shape)
	}
	if event.KernelStack != nil {
		var shape *StackShape
		if stacks != nil {
			shape = stacks.KernelShape
		}
		fields["kernel.stack"] = event.KernelStack.key(shape)
	}
	if event.Process == nil && event.Packet == nil {
		fields["pid"], fields["tid"], fields["name"] = event.PID, event.TID, event.Name
		if event.ProcessName != "" {
			fields["process_name"] = event.ProcessName
		}
		if event.CgroupPath != "" {
			fields["cgroup.path"] = event.CgroupPath
		}
		fields["name_group"] = event.NameGroup
		if event.NameGroup == "" {
			fields["name_group"] = event.Name
		}
	}
	if event.DurationNS != nil {
		fields["duration_ns"] = *event.DurationNS
	}
	if event.ReturnValue != nil {
		fields["return_value"] = *event.ReturnValue
	}
	if event.File != nil {
		fields["file.fd"] = event.File.FD
		if event.File.Path != "" {
			fields["file.path"] = event.File.Path
			fields["file.dir"] = path.Dir(event.File.Path)
		}
		if event.File.Error != "" {
			fields["file.error"] = event.File.Error
		}
	}
	return fields
}

type aggregateAccumulator struct {
	AggregateCount
	sum         big.Rat
	min, max    *big.Rat
	distinct    map[string]struct{}
	samples     []*big.Rat
	histogram   map[string]*HistogramBucket
	zeroCount   uint64
	rateEnd     time.Time
	rateCovered time.Duration
}

func (e *aggregateAccumulator) add(a AggregationRequest, value any, retained *int) error {
	if a.Function == "" || a.Function == "count" {
		e.Count++
		return nil
	}
	if a.Function == "rate" {
		interval := value.(counterInterval)
		e.sum.Add(&e.sum, interval.delta)
		// Records arrive in observation order. Merge overlapping intervals so
		// multiple counters grouped together contribute a total, not a mean.
		if interval.end.After(e.rateEnd) {
			start := interval.start
			if e.rateEnd.After(start) {
				start = e.rateEnd
			}
			e.rateCovered += interval.end.Sub(start)
			e.rateEnd = interval.end
		}
		e.Count++
		return nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if a.Function == "count_distinct" {
		if e.distinct == nil {
			e.distinct = make(map[string]struct{})
		}
		key := string(data)
		if _, ok := e.distinct[key]; !ok {
			if *retained >= maxAggregateValues {
				return errors.New("aggregation exceeds 65536 retained values")
			}
			e.distinct[key] = struct{}{}
			*retained += 1
		}
	} else {
		n, ok := new(big.Rat).SetString(string(data))
		if !ok {
			return fmt.Errorf("aggregation field %q is not numeric", a.Field)
		}
		switch a.Function {
		case "sum", "avg":
			e.sum.Add(&e.sum, n)
		case "min", "max":
			if e.min == nil || n.Cmp(e.min) < 0 {
				e.min = n
			}
			if e.max == nil || n.Cmp(e.max) > 0 {
				e.max = n
			}
		case "percentile":
			if *retained >= maxAggregateValues {
				return errors.New("aggregation exceeds 65536 retained values")
			}
			e.samples = append(e.samples, n)
			*retained += 1
		case "hist":
			if n.Sign() == 0 {
				e.zeroCount++
				break
			}
			bucket := histogramBucket(n)
			if e.histogram == nil {
				e.histogram = make(map[string]*HistogramBucket)
			}
			key := bucket.Lower.String()
			if e.histogram[key] == nil {
				if *retained >= maxAggregateValues {
					return errors.New("aggregation exceeds 65536 retained values")
				}
				e.histogram[key] = &bucket
				*retained += 1
			}
			e.histogram[key].Count++
		}
	}
	e.Count++
	return nil
}

func aggregateNumber(n *big.Rat) string {
	if n.IsInt() {
		return n.Num().String()
	}
	value := strings.TrimRight(strings.TrimRight(n.FloatString(18), "0"), ".")
	if value == "-0" {
		value = "0"
	}
	return value
}

func (e *aggregateAccumulator) value(a AggregationRequest) json.RawMessage {
	number := aggregateNumber
	var value string
	switch a.Function {
	case "sum":
		value = number(&e.sum)
	case "count_distinct":
		value = fmt.Sprint(len(e.distinct))
	case "hist":
		h := Histogram{Count: e.Count, ZeroCount: e.zeroCount, Buckets: make([]HistogramBucket, 0, len(e.histogram))}
		for _, bucket := range e.histogram {
			h.Buckets = append(h.Buckets, *bucket)
		}
		slices.SortFunc(h.Buckets, func(x, y HistogramBucket) int {
			a, _ := new(big.Rat).SetString(x.Lower.String())
			b, _ := new(big.Rat).SetString(y.Lower.String())
			return a.Cmp(b)
		})
		data, _ := json.Marshal(h)
		return data
	case "rate":
		if e.rateCovered == 0 {
			return json.RawMessage("null")
		}
		seconds := new(big.Rat).SetFrac(big.NewInt(int64(e.rateCovered)), big.NewInt(int64(time.Second)))
		value = number(new(big.Rat).Quo(&e.sum, seconds))
	default:
		if e.Count == 0 {
			return json.RawMessage("null")
		}
		switch a.Function {
		case "avg":
			mean := new(big.Rat).Quo(&e.sum, new(big.Rat).SetInt(new(big.Int).SetUint64(e.Count)))
			value = number(mean)
		case "min":
			value = number(e.min)
		case "max":
			value = number(e.max)
		case "percentile":
			slices.SortFunc(e.samples, func(a, b *big.Rat) int { return a.Cmp(b) })
			// Use a decimal rational to avoid floating-point rank errors at boundaries.
			p, _ := new(big.Rat).SetString(fmt.Sprint(a.Percentile))
			p.Mul(p, big.NewRat(int64(len(e.samples)), 100))
			rank, remainder := new(big.Int), new(big.Int)
			rank.QuoRem(p.Num(), p.Denom(), remainder)
			if remainder.Sign() != 0 {
				rank.Add(rank, big.NewInt(1))
			}
			index := max(0, int(rank.Int64())-1)
			value = number(e.samples[index])
		}
	}
	return json.RawMessage(value)
}

// Four equal sub-buckets per power-of-two range, including fractions and
// negative values. Bounds are exact dyadic rationals, never float approximations.
func histogramBucket(n *big.Rat) HistogramBucket {
	abs := new(big.Rat).Abs(n)
	exponent := abs.Num().BitLen() - abs.Denom().BitLen()
	power := new(big.Rat)
	if exponent >= 0 {
		power.SetInt(new(big.Int).Lsh(big.NewInt(1), uint(exponent)))
	} else {
		power.SetFrac(big.NewInt(1), new(big.Int).Lsh(big.NewInt(1), uint(-exponent)))
	}
	if abs.Cmp(power) < 0 {
		power.Quo(power, big.NewRat(2, 1))
	}
	position := new(big.Rat).Quo(abs, power)
	position.Sub(position, big.NewRat(1, 1)).Mul(position, big.NewRat(4, 1))
	index := new(big.Int).Quo(position.Num(), position.Denom()).Int64()
	if n.Sign() < 0 && position.IsInt() {
		index-- // Negative boundaries belong to the bucket on their right.
		if index < 0 {
			power.Quo(power, big.NewRat(2, 1))
			index = 3
		}
	}
	lower := new(big.Rat).Mul(power, big.NewRat(4+index, 4))
	upper := new(big.Rat).Mul(power, big.NewRat(5+index, 4))
	if n.Sign() < 0 {
		lower, upper = new(big.Rat).Neg(upper), new(big.Rat).Neg(lower)
	}
	return HistogramBucket{Lower: json.Number(lower.FloatString(lower.Denom().BitLen() - 1)), Upper: json.Number(upper.FloatString(upper.Denom().BitLen() - 1))}
}

// aggregateReduction is shared by event windows and sampled snapshots.
type aggregateReduction struct {
	request       AggregationRequest
	groups        map[string]*aggregateAccumulator
	retained      *int // Shared across metrics: the storage cap is per query, not per function.
	metrics       []*aggregateReduction
	seriesGroups  *int // periodic queries share a bounded group budget across buckets/metrics
	stackCoverage *StackCoverageReport
	rollups       map[string]*aggregateReduction
}

func newAggregateReduction(a AggregationRequest) *aggregateReduction {
	r := &aggregateReduction{request: a, groups: make(map[string]*aggregateAccumulator), retained: new(int)}
	if len(a.Metrics) != 0 {
		for _, metric := range a.Metrics {
			one := a
			one.Metrics = nil
			one.Compact, one.Nonzero, one.Limit, one.SortMetric, one.Ascending = false, false, 0, 0, false
			one.Function, one.Field, one.Percentile = metric.Function, metric.Field, metric.Percentile
			if one.Function == "" {
				one.Function = "count"
			}
			child := newAggregateReduction(one)
			child.retained = r.retained
			r.metrics = append(r.metrics, child)
		}
		return r
	}
	if len(a.GroupBy) == 0 {
		r.groups[""] = &aggregateAccumulator{AggregateCount: AggregateCount{Group: map[string]json.RawMessage{}}}
	}
	return r
}

func (r *aggregateReduction) add(fields map[string]any) error {
	for _, coarse := range r.rollups {
		if err := coarse.add(fields); err != nil {
			return err
		}
	}
	if len(r.metrics) != 0 {
		for _, metric := range r.metrics {
			if err := metric.add(fields); err != nil {
				return err
			}
		}
		return nil
	}
	var value any
	if r.request.Field != "" {
		var ok bool
		field := r.request.Field
		if r.request.Function == "rate" {
			field = "__counter_delta." + field
		}
		value, ok = fields[field]
		// Optional metrics use available observations, just like sampled
		// snapshots. Do not turn a missing numeric value into a zero.
		if !ok || value == nil {
			return nil
		}
	}
	group := make(map[string]json.RawMessage, len(r.request.GroupBy))
	var key strings.Builder
	for _, field := range r.request.GroupBy {
		value := fields[field] // absent optional grouping fields form a null bucket
		data, err := json.Marshal(value)
		if err != nil {
			return err
		}
		name := field
		if alias := r.request.GroupAliases[field]; alias != "" {
			name = alias
		}
		group[name] = data
		fmt.Fprintf(&key, "%d:%s", len(data), data)
	}
	entry := r.groups[key.String()]
	if entry == nil {
		if r.seriesGroups != nil {
			if *r.seriesGroups >= maxAggregateGroups {
				return errors.New("periodic reports exceed 4096 retained metric groups")
			}
			*r.seriesGroups++
		}
		if len(r.groups) >= maxAggregateGroups {
			return errors.New("aggregation exceeds 4096 groups")
		}
		entry = &aggregateAccumulator{AggregateCount: AggregateCount{Group: group}}
		r.groups[key.String()] = entry
	}
	return entry.add(r.request, value, r.retained)
}

func (r *aggregateReduction) result(source string, start, end time.Time) Snapshot {
	groupBy := append([]string{}, r.request.GroupBy...)
	for i, field := range groupBy {
		if alias := r.request.GroupAliases[field]; alias != "" {
			groupBy[i] = alias
		}
	}
	if len(r.metrics) != 0 {
		result := &AggregationResult{Table: r.request.Table, Start: start.UTC(), End: end.UTC(), GroupBy: groupBy, Every: r.request.Every}
		for _, metric := range r.metrics {
			result.Metrics = append(result.Metrics, metric.result(source, start, end).Aggregation)
		}
		r.formatRows(result)
		r.attachRollups(result, source)
		return Snapshot{Source: source, Time: end.UTC(), Aggregation: result}
	}
	keys := make([]string, 0, len(r.groups))
	for key := range r.groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	a := r.request
	result := &AggregationResult{Table: a.Table, Start: start.UTC(), End: end.UTC(), GroupBy: groupBy, Function: a.Function, Field: a.Field, Percentile: a.Percentile, Every: a.Every}
	if a.Function == "" || a.Function == "count" {
		result.Counts = make([]AggregateCount, 0, len(keys))
	} else {
		result.Values = make([]AggregateValue, 0, len(keys))
	}
	for _, key := range keys {
		if a.Function == "" || a.Function == "count" {
			result.Counts = append(result.Counts, r.groups[key].AggregateCount)
		} else {
			result.Values = append(result.Values, AggregateValue{Group: r.groups[key].Group, Value: r.groups[key].value(a)})
		}
	}
	r.formatRows(result)
	r.attachRollups(result, source)
	return Snapshot{Source: source, Time: end.UTC(), Aggregation: result}
}

// Join on the group, not positional row indexes: sampled metrics can have
// different available groups. Rendering controls never change collection.
func (r *aggregateReduction) formatRows(a *AggregationResult) {
	if !r.request.Compact && !r.request.Nonzero && r.request.Limit == 0 && r.request.SortMetric == 0 && !r.request.Ascending {
		return
	}
	metrics := a.Metrics
	if len(metrics) == 0 {
		metrics = []*AggregationResult{a}
	}
	rows := make(map[string]*AggregateRow)
	for index, metric := range metrics {
		function := metric.Function
		if function == "" {
			function = "count"
		}
		column := AggregateMetric{Function: function, Field: metric.Field, Percentile: metric.Percentile}
		if len(r.request.Metrics) != 0 {
			column.Name = r.request.Metrics[index].Name
		}
		if a.Table != "" && column.Name == "" {
			column.Name = "value"
		}
		a.Columns = append(a.Columns, column)
		add := func(group map[string]json.RawMessage, value json.RawMessage) {
			data, _ := json.Marshal(group)
			key := string(data)
			if rows[key] == nil {
				values := make([]json.RawMessage, len(metrics))
				for i := range values {
					values[i] = json.RawMessage("null")
				}
				rows[key] = &AggregateRow{Group: group, Values: values}
			}
			rows[key].Values[index] = value
		}
		if function == "count" {
			for _, row := range metric.Counts {
				add(row.Group, json.RawMessage(fmt.Sprint(row.Count)))
			}
		} else {
			for _, row := range metric.Values {
				add(row.Group, row.Value)
			}
		}
	}
	a.TotalGroups = len(rows)
	keys := make([]string, 0, len(rows))
	for key, row := range rows {
		allZero := true
		for _, value := range row.Values {
			n, ok := new(big.Rat).SetString(string(value))
			if !ok || n.Sign() != 0 {
				allZero = false
				break
			}
		}
		if r.request.Nonzero && allZero {
			a.OmittedZeroGroups++
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	sort.SliceStable(keys, func(i, j int) bool {
		x, xok := new(big.Rat).SetString(string(rows[keys[i]].Values[r.request.SortMetric]))
		y, yok := new(big.Rat).SetString(string(rows[keys[j]].Values[r.request.SortMetric]))
		if !xok {
			return false
		}
		if !yok {
			return true
		}
		if r.request.Ascending {
			return x.Cmp(y) < 0
		}
		return x.Cmp(y) > 0
	})
	if r.request.Limit > 0 && len(keys) > r.request.Limit {
		keys = keys[:r.request.Limit]
	}
	a.Rows = make([]AggregateRow, 0, len(keys))
	for _, key := range keys {
		a.Rows = append(a.Rows, *rows[key])
	}
}

// Each selector has its own subscription/sampler and reduction. The shared
// clock aligns reporting boundaries, not individual kernel attachment times.
func aggregateScript(ctx context.Context, request MonitorRequest, source EventSource, collect func(context.Context, MonitorRequest) (Snapshot, error)) (Snapshot, error) {
	if err := request.Validate(); err != nil {
		return Snapshot{}, err
	}
	if request.Source != "script" {
		return Snapshot{}, errors.New("script query required")
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	start := time.Now()
	type completed struct {
		index    int
		snapshot Snapshot
		err      error
	}
	done := make(chan completed, len(request.Probes))
	for i, probe := range request.Probes {
		go func() {
			var snapshot Snapshot
			var err error
			var rollups [][]string
			for _, report := range request.Reports {
				if report.Left == probe.Aggregation.Table && len(report.LeftRollup) != 0 {
					rollups = append(rollups, report.LeftRollup)
				}
				if report.Right == probe.Aggregation.Table && len(report.RightRollup) != 0 {
					rollups = append(rollups, report.RightRollup)
				}
			}
			if sampledAggregation(probe) {
				snapshot, err = aggregateSnapshotsAt(ctx, probe, collect, start, rollups...)
			} else {
				snapshot, err = aggregateEventsAt(ctx, probe, source, start, rollups...)
			}
			done <- completed{i, snapshot, err}
		}()
	}
	result := Snapshot{Source: "script", Time: start.Add(request.Aggregation.Window).UTC(), Tables: make([]Snapshot, len(request.Probes))}
	var failure error
	for range request.Probes {
		item := <-done
		if item.err != nil && failure == nil {
			failure = fmt.Errorf("table @%s (%s): %w", request.Probes[item.index].Aggregation.Table, request.Probes[item.index].Source, item.err)
			cancel()
		}
		result.Tables[item.index] = item.snapshot
	}
	if failure != nil {
		return Snapshot{}, failure
	}
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	if len(request.Reports) != 0 {
		var err error
		result.Tables, err = reportScriptTables(request, result.Tables)
		if err != nil {
			return Snapshot{}, err
		}
	}
	if data, err := json.Marshal(result); err != nil {
		return Snapshot{}, err
	} else if len(data) > 7<<20 {
		return Snapshot{}, errors.New("script output exceeds 7 MiB; narrow groups or limit output")
	}
	return result, nil
}

func aggregateEvents(ctx context.Context, request MonitorRequest, source EventSource) (Snapshot, error) {
	return aggregateEventsAt(ctx, request, source, time.Now())
}

func aggregateEventsAt(ctx context.Context, request MonitorRequest, source EventSource, start time.Time, rollups ...[]string) (Snapshot, error) {
	if err := request.Validate(); err != nil {
		return Snapshot{}, err
	}
	if request.Mode != "aggregate" {
		return Snapshot{}, errors.New("aggregate mode required")
	}
	end := start.Add(request.Aggregation.Window)
	windowCtx, cancel := context.WithDeadline(ctx, end)
	defer cancel()
	reductions := []*aggregateReduction{newAggregateReduction(*request.Aggregation)}
	if interval := request.Aggregation.ReportEvery; interval != 0 {
		reductions = nil
		retained, groups := new(int), new(int)
		for elapsed := time.Duration(0); elapsed < request.Aggregation.Window; elapsed += interval {
			r := newAggregateReduction(*request.Aggregation)
			r.retained, r.seriesGroups = retained, groups
			for _, child := range r.metrics {
				child.retained, child.seriesGroups = retained, groups
			}
			reductions = append(reductions, r)
		}
	}
	for _, reduction := range reductions {
		reduction.configureRollups(rollups)
	}
	if request.Stacks != nil {
		for _, reduction := range reductions {
			reduction.stackCoverage = &StackCoverageReport{}
			if request.Stacks.User {
				reduction.stackCoverage.User = &StackCoverage{}
			}
			if request.Stacks.Kernel {
				reduction.stackCoverage.Kernel = &StackCoverage{}
			}
		}
	}
	var collection *CollectionStats
	var mu sync.Mutex
	// Sources receive only the event selection, not the query mode.
	selection := request
	selection.Mode, selection.Aggregation = "", nil
	err := source(windowCtx, selection, func(event Event) error {
		mu.Lock()
		defer mu.Unlock()
		if event.Collection != nil {
			stats := *event.Collection
			collection = &stats
		}
		if event.Kind == "collection_stats" {
			return nil
		}
		now := time.Now()
		if !now.Before(end) || windowCtx.Err() != nil || !selection.Matches(event) {
			return nil
		}
		fields := eventGroupFields(event, request.Stacks)
		if request.FileDepth > 0 {
			if dir, ok := fields["file.dir"].(string); ok {
				parts := strings.Split(strings.TrimPrefix(dir, "/"), "/")
				fields["file.dir"] = "/" + strings.Join(parts[:min(request.FileDepth, len(parts))], "/")
			}
		}
		bucket := 0
		if interval := request.Aggregation.ReportEvery; interval != 0 {
			bucket = int(now.Sub(start) / interval)
		}
		if coverage := reductions[bucket].stackCoverage; coverage != nil {
			if coverage.User != nil {
				coverage.User.observe(event.UserStack, request.Stacks.Symbolize)
			}
			if coverage.Kernel != nil {
				coverage.Kernel.observe(event.KernelStack, request.Stacks.Symbolize)
			}
		}
		return reductions[bucket].add(fields)
	})
	if ctx.Err() != nil {
		return Snapshot{}, ctx.Err()
	}
	if err != nil && !errors.Is(err, context.DeadlineExceeded) {
		return Snapshot{}, err
	}
	if windowCtx.Err() != context.DeadlineExceeded {
		if err != nil {
			return Snapshot{}, err
		}
		return Snapshot{}, errors.New("event source stopped before aggregation window completed")
	}
	if request.Aggregation.ReportEvery == 0 {
		result := reductions[0].result(request.Source, start, end)
		result.Aggregation.Collection = collection
		result.Aggregation.StackCoverage = reductions[0].stackCoverage
		return result, nil
	}
	result := Snapshot{Source: request.Source, Time: end.UTC()}
	for i, reduction := range reductions {
		begin := start.Add(time.Duration(i) * request.Aggregation.ReportEvery)
		finish := begin.Add(request.Aggregation.ReportEvery)
		if finish.After(end) {
			finish = end
		}
		window := reduction.result(request.Source, begin, finish).Aggregation
		window.StackCoverage = reduction.stackCoverage
		result.Windows = append(result.Windows, window)
	}
	// Collection counters remain subscription-wide, not per-bucket loss estimates.
	result.Windows[len(result.Windows)-1].Collection = collection
	if data, err := json.Marshal(result); err != nil {
		return Snapshot{}, err
	} else if len(data) > 7<<20 {
		return Snapshot{}, errors.New("periodic report output exceeds 7 MiB; narrow groups or limit output")
	}
	return result, nil
}
