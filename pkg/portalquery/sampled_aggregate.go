package query

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"reflect"
	"strings"
	"time"
)

const DefaultSampleInterval = time.Second
const MinSampleInterval = 100 * time.Millisecond

// Snapshot sources share one sampler. Process remains an event aggregate unless
// every is explicit; all other entries are snapshot-only sources.
var sampledSources = map[string]struct {
	field    string
	identity []string
}{
	"cpu":        {"CPU", []string{"name"}},
	"memory":     {"Memory", nil},
	"network":    {"Network", []string{"name", "index"}},
	"kernel":     {"Kernel", []string{"boot_time"}},
	"sensors":    {"Sensors", []string{"name"}},
	"gpu":        {"GPUs", []string{"uuid"}},
	"containers": {"Containers", []string{"id"}},
	"process":    {"Processes", []string{"pid", "started"}},
	"cgroups":    {"Cgroups", []string{"path", "id"}},
}

func sampledAggregation(r MonitorRequest) bool {
	_, ok := sampledSources[r.Source]
	return ok && (r.Source != "process" || r.Aggregation != nil && r.Aggregation.Every != 0)
}

type SampleField struct {
	FieldCapability
	Semantics string `json:"semantics"` // identity, gauge, counter, rate, utilization
}

type SamplingCapability struct {
	DefaultInterval string        `json:"default_interval"`
	MinInterval     string        `json:"min_interval"`
	Fields          []SampleField `json:"fields"`
	GroupByFields   []string      `json:"group_by_fields"`
	NumericFields   []string      `json:"numeric_fields"`
}

func snapshotSampleFields(source string) []SampleField {
	def, ok := sampledSources[source]
	if !ok {
		return nil
	}
	f, _ := reflect.TypeOf(Snapshot{}).FieldByName(def.field)
	typ := f.Type.Elem()
	var fields []SampleField
	for _, field := range outputFields(reflect.Zero(typ).Interface(), "") {
		if field.Type != "integer" && field.Type != "number" && field.Type != "string" && field.Type != "timestamp" {
			continue
		}
		if source == "cgroups" && strings.HasPrefix(field.Path, "io.devices") {
			continue // Device rows are snapshot detail; sampled metrics use cgroup totals.
		}
		field.QueryField = field.Path
		kind := "identity"
		if field.Type == "integer" || field.Type == "number" {
			kind = "gauge"
		}
		switch {
		case source == "cpu" && kind == "gauge":
			kind, field.Unit = "counter", "seconds"
		case source == "network" && (strings.HasPrefix(field.Path, "bytes_") || strings.HasPrefix(field.Path, "packets_")):
			kind = "counter"
			field.Unit = "packets"
			if strings.HasPrefix(field.Path, "bytes_") {
				field.Unit = "bytes"
			}
		case source == "kernel" && field.Path == "counters.context_switches":
			kind, field.Unit = "counter", "switches"
		case (source == "process" || source == "cgroups") && field.Path == "cpu_seconds":
			kind, field.Unit = "counter", "seconds"
		case source == "cgroups" && strings.HasPrefix(field.Path, "io."):
			kind, field.Unit = "counter", "operations"
			if strings.HasSuffix(field.Path, "_bytes") {
				field.Unit = "bytes"
			}
		case source == "process" && field.Path == "pid" || source == "gpu" && field.Path == "index" || source == "network" && field.Path == "index":
			kind = "identity"
		case source == "memory" || field.Path == "mtu" || strings.HasSuffix(field.Path, "_bytes"):
			field.Unit = "bytes"
		case source == "process" && field.Path == "threads":
			field.Unit = "threads"
		case source == "cgroups" && field.Path == "cpu_limit_cores":
			field.Unit = "cores"
		case source == "cgroups" && field.Path == "pids_current":
			field.Unit = "tasks"
		case strings.HasSuffix(field.Path, "_celsius"):
			field.Unit = "degrees Celsius"
		case strings.HasSuffix(field.Path, "_percent"):
			field.Unit = "percent"
		case strings.HasSuffix(field.Path, "_mib"):
			field.Unit = "MiB"
		case strings.HasSuffix(field.Path, "_watts"):
			field.Unit = "watts"
		case field.Path == "uptime_seconds":
			field.Unit = "seconds"
		}
		fields = append(fields, SampleField{field, kind})
		if kind == "counter" {
			name := field.Path + "_per_second"
			fields = append(fields, SampleField{FieldCapability{Path: name, QueryField: name, Type: "number", Optional: true, Unit: field.Unit + "/second", Description: "Counter delta divided by actual elapsed seconds; needs consecutive observations, skips resets."}, "rate"})
		}
	}
	if source == "cpu" {
		fields = append(fields, SampleField{FieldCapability{Path: "utilization_percent", QueryField: "utilization_percent", Type: "number", Optional: true, Unit: "percent", Description: "100 × (delta total − delta idle − delta iowait) / delta total per CPU; needs consecutive observations."}, "utilization"})
	}
	if source == "process" {
		fields = append(fields, SampleField{FieldCapability{Path: "cpu_percent", QueryField: "cpu_percent", Type: "number", Optional: true, Unit: "percent", Description: "100 × delta process user + system CPU seconds / elapsed seconds; 100% is one fully occupied core, may exceed 100%. Excludes child processes and needs consecutive observations of the same PID/start time."}, "utilization"})
	}
	if source == "cgroups" {
		fields = append(fields, SampleField{FieldCapability{Path: "cpu_percent", QueryField: "cpu_percent", Type: "number", Optional: true, Unit: "percent", Description: "100 × delta cpu_seconds / elapsed seconds, including descendants; 100% is one busy core, may exceed 100%. Not normalized to quota; needs consecutive observations of the same directory identity."}, "utilization"})
	}
	for i := range fields {
		fields[i].Aggregatable = true // Scalars support distinct count; counters additionally support rate.
	}
	return fields
}

func sampledFields(source string) (fields, numeric []string) {
	for _, field := range snapshotSampleFields(source) {
		fields = append(fields, field.Path)
		if field.Semantics == "gauge" || field.Semantics == "rate" || field.Semantics == "utilization" {
			numeric = append(numeric, field.Path)
		}
	}
	return
}

func sampleRecords(snapshot Snapshot) ([]map[string]any, error) {
	if snapshot.sampleRows != nil {
		return snapshot.sampleRows, nil
	}
	def := sampledSources[snapshot.Source]
	value := reflect.ValueOf(snapshot).FieldByName(def.field)
	var records []map[string]any
	appendRecord := func(value any) error {
		data, err := json.Marshal(value)
		if err != nil {
			return err
		}
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.UseNumber()
		var object map[string]any
		if err := decoder.Decode(&object); err != nil {
			return err
		}
		flat := make(map[string]any)
		var flatten func(map[string]any, string)
		flatten = func(object map[string]any, prefix string) {
			for name, v := range object {
				name = prefix + name
				if nested, ok := v.(map[string]any); ok {
					flatten(nested, name+".")
				} else if v != nil {
					flat[name] = v
				}
			}
		}
		flatten(object, "")
		records = append(records, flat)
		return nil
	}
	if value.Kind() == reflect.Pointer {
		if !value.IsNil() {
			if err := appendRecord(value.Interface()); err != nil {
				return nil, err
			}
		}
	} else {
		for i := 0; i < value.Len(); i++ {
			if err := appendRecord(value.Index(i).Interface()); err != nil {
				return nil, err
			}
		}
	}
	return records, nil
}

type sampleObservation struct {
	fields map[string]any
	time   time.Time
}

type counterInterval struct {
	delta      *big.Rat
	start, end time.Time
}

func sampleNumber(value any) *big.Rat {
	n, ok := value.(json.Number)
	if !ok {
		return nil
	}
	r, _ := new(big.Rat).SetString(n.String())
	return r
}

func deriveSample(fields map[string]any, previous sampleObservation, at time.Time, metadata []SampleField) {
	if previous.fields == nil || !at.After(previous.time) {
		return
	}
	deltas := make(map[string]*big.Rat)
	for _, field := range metadata {
		if field.Semantics != "counter" {
			continue
		}
		if strings.HasPrefix(field.Path, "io.") && !cgroupIODeltaValid(fields, previous.fields, field.Path) {
			continue
		}
		current, before := sampleNumber(fields[field.Path]), sampleNumber(previous.fields[field.Path])
		if current == nil || before == nil || current.Cmp(before) < 0 {
			continue
		}
		delta := new(big.Rat).Sub(current, before)
		deltas[field.Path] = delta
		fields["__counter_delta."+field.Path] = counterInterval{delta, previous.time, at}
		seconds := new(big.Rat).SetFrac(big.NewInt(int64(at.Sub(previous.time))), big.NewInt(int64(time.Second)))
		rate := new(big.Rat).Quo(delta, seconds)
		fields[field.Path+"_per_second"] = json.Number(rate.FloatString(18))
		if field.Path == "cpu_seconds" {
			fields["cpu_percent"] = json.Number(rate.Mul(rate, big.NewRat(100, 1)).FloatString(18))
		}
	}
	total, idle, iowait := deltas["total"], deltas["idle"], deltas["iowait"]
	if total != nil && idle != nil && iowait != nil && total.Sign() > 0 {
		busy := new(big.Rat).Sub(total, idle)
		busy.Sub(busy, iowait)
		if busy.Sign() >= 0 && busy.Cmp(total) <= 0 {
			busy.Quo(busy, total).Mul(busy, big.NewRat(100, 1))
			fields["utilization_percent"] = json.Number(busy.FloatString(18))
		}
	}
}

// Totals alone can hide a device counter reset, or count a newly appearing
// device's lifetime usage as interval I/O. Require matching device sets and
// monotonic per-device counters before deriving each total's rate.
func cgroupIODeltaValid(current, previous map[string]any, path string) bool {
	key := map[string]string{"io.read_bytes": "rbytes", "io.write_bytes": "wbytes", "io.discard_bytes": "dbytes", "io.read_ios": "rios", "io.write_ios": "wios", "io.discard_ios": "dios"}[path]
	devices, ok := current["io.devices"].([]any)
	before, beforeOK := previous["io.devices"].([]any)
	if !ok || !beforeOK || len(devices) != len(before) {
		return false
	}
	old := make(map[string]map[string]any, len(before))
	for _, value := range before {
		device := value.(map[string]any)
		old[device["device"].(string)] = device["counters"].(map[string]any)
	}
	for _, value := range devices {
		device := value.(map[string]any)
		counters := device["counters"].(map[string]any)
		prior, exists := old[device["device"].(string)]
		if !exists {
			return false
		}
		now, then := sampleNumber(counters[key]), sampleNumber(prior[key])
		if now == nil || then == nil || now.Cmp(then) < 0 {
			return false
		}
	}
	return true
}

// Each sampled metric independently skips unavailable fields. In particular,
// a rate's first observation is a baseline, but gauges/count can use that sample.
func (r *aggregateReduction) addSample(fields map[string]any) error {
	return r.add(fields)
}

// aggregateSnapshots samples immediately and then on the interval grid within
// [start,end). Slow reads skip ticks, never overlap or generate catch-up bursts.
func aggregateSnapshots(ctx context.Context, request MonitorRequest, collect func(context.Context, MonitorRequest) (Snapshot, error)) (Snapshot, error) {
	return aggregateSnapshotsAt(ctx, request, collect, time.Now())
}

func aggregateSnapshotsAt(ctx context.Context, request MonitorRequest, collect func(context.Context, MonitorRequest) (Snapshot, error), start time.Time, rollups ...[]string) (Snapshot, error) {
	if err := request.Validate(); err != nil {
		return Snapshot{}, err
	}
	if request.Mode != "aggregate" || !sampledAggregation(request) {
		return Snapshot{}, errors.New("sampled aggregate required")
	}
	a := *request.Aggregation
	interval := a.Every
	if interval == 0 {
		interval = DefaultSampleInterval
	}
	a.Every = interval
	end := start.Add(a.Window)
	windowCtx, cancel := context.WithDeadline(ctx, end)
	defer cancel()
	reduction := newAggregateReduction(a)
	reduction.configureRollups(rollups)
	collect, err := correlatedCollector(windowCtx, request, collect)
	if err != nil {
		if ctx.Err() != nil {
			return Snapshot{}, ctx.Err()
		}
		if windowCtx.Err() != context.DeadlineExceeded || !errors.Is(err, context.DeadlineExceeded) {
			return Snapshot{}, err
		}
		return reduction.result(request.Source, start, end), nil
	}
	selection := request
	selection.Mode, selection.Aggregation = "snapshot", nil
	previous := make(map[string]sampleObservation)
	metadata := snapshotSampleFields(request.Source)
	next := start
	for {
		if ctx.Err() != nil {
			return Snapshot{}, ctx.Err()
		}
		if !time.Now().Before(end) {
			break
		}
		if delay := time.Until(next); delay > 0 {
			timer := time.NewTimer(delay)
			select {
			case <-windowCtx.Done():
				timer.Stop()
			case <-timer.C:
			}
			if windowCtx.Err() != nil || !time.Now().Before(end) {
				break
			}
		}
		snapshot, err := collect(windowCtx, selection)
		if ctx.Err() != nil {
			return Snapshot{}, ctx.Err()
		}
		if err != nil {
			if windowCtx.Err() == context.DeadlineExceeded && errors.Is(err, context.DeadlineExceeded) {
				break
			}
			return Snapshot{}, err
		}
		at := time.Now()
		if !at.Before(end) {
			break
		}
		if snapshot.Source != request.Source {
			return Snapshot{}, errors.New("snapshot source mismatch")
		}
		records, err := sampleRecords(snapshot)
		if err != nil {
			return Snapshot{}, err
		}
		if len(records) > maxAggregateGroups {
			return Snapshot{}, errors.New("sampling exceeds 4096 records")
		}
		current := make(map[string]sampleObservation, len(records))
		for _, record := range records {
			identity := make([]any, 0, len(sampledSources[request.Source].identity))
			for _, field := range sampledSources[request.Source].identity {
				identity = append(identity, record[field])
			}
			key, _ := json.Marshal(identity)
			deriveSample(record, previous[string(key)], at, metadata)
			current[string(key)] = sampleObservation{record, at}
			if err := reduction.addSample(record); err != nil {
				return Snapshot{}, err
			}
		}
		previous = current
		for !next.After(at) {
			next = next.Add(interval)
		}
		if next.After(end) {
			next = end
		}
	}
	if ctx.Err() != nil {
		return Snapshot{}, ctx.Err()
	}
	result := reduction.result(request.Source, start, end)
	result.Aggregation.Every = interval
	return result, nil
}
