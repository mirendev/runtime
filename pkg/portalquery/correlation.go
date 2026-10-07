package query

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"reflect"
	"slices"
	"strings"
	"time"
)

// SnapshotCorrelation selects target identities from a frozen custom snapshot.
// Inventory.Data must be a JSON array of flat objects with declared scalar
// fields. Matching is exact, not a glob or a completed-aggregate join.
type SnapshotCorrelation struct {
	Inventory MonitorRequest `json:"inventory"`
	Field     string         `json:"field"` // target string identity field
	Key       string         `json:"key"`   // inventory string field
}

func (c SnapshotCorrelation) validate(r MonitorRequest) error {
	def, ok := sampledSources[r.Source]
	if !ok || r.customSource != nil || r.Mode != "snapshot" && r.Mode != "aggregate" || !slices.Contains(def.identity, c.Field) {
		return errors.New("using requires a sampled built-in source and a string identity field")
	}
	if r.Source == "cgroups" && (c.Field != "path" || r.Path != "") {
		return errors.New("cgroups using requires on path and no additional path filter")
	}
	if !slices.ContainsFunc(snapshotSampleFields(r.Source), func(f SampleField) bool { return f.Path == c.Field && f.Type == "string" }) {
		return errors.New("using target must be a string identity field")
	}
	i := c.Inventory
	if i.customSource == nil || i.customSource.Snapshots == nil || i.Mode != "snapshot" || i.Aggregation != nil || i.Using != nil || !slices.Contains(i.customSource.Fields, c.Key) {
		return errors.New("using inventory requires a registered custom snapshot source and declared key; no nested using or aggregates")
	}
	return i.Validate()
}

// Freeze inventory within the caller's window/deadline. The returned collector
// preserves physical target identity for rates and enriches each row once.
func correlatedCollector(ctx context.Context, r MonitorRequest, collect SnapshotSource) (SnapshotSource, error) {
	if r.Using == nil {
		return collect, nil
	}
	c := *r.Using
	snapshot, err := collect(ctx, c.Inventory)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		return nil, err
	}
	if snapshot.Source != c.Inventory.Source {
		return nil, errors.New("inventory snapshot source mismatch")
	}
	data, err := json.Marshal(snapshot.Data)
	if err != nil {
		return nil, err
	}
	if len(data) > 7<<20 {
		return nil, errors.New("inventory snapshot exceeds 7 MiB")
	}
	if len(data) == 0 || data[0] != '[' {
		return nil, errors.New("using inventory data must be an array of flat objects")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var rows []map[string]any
	if err := decoder.Decode(&rows); err != nil {
		return nil, fmt.Errorf("decode inventory rows: %w", err)
	}
	if len(rows) > maxAggregateGroups {
		return nil, errors.New("inventory exceeds 4096 rows")
	}
	owners := make(map[string]map[string]any)
	for _, row := range rows {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if row == nil {
			return nil, errors.New("inventory rows must be objects, not null")
		}
		for field, value := range row {
			if !slices.Contains(c.Inventory.customSource.Fields, field) {
				return nil, fmt.Errorf("undeclared inventory field %q", field)
			}
			switch value.(type) {
			case nil, string, json.Number, bool:
			default:
				return nil, fmt.Errorf("inventory field %q must be scalar", field)
			}
		}
		value := row[c.Key]
		if value == nil || value == "" {
			continue // Inventory rows without a target cannot contribute metrics.
		}
		key, ok := value.(string)
		if !ok {
			return nil, errors.New("inventory correlation key must be a string")
		}
		if r.Source == "cgroups" && (!strings.HasPrefix(key, "/") || path.Clean(key) != key || strings.ContainsAny(key, "*?[]\x00")) {
			return nil, fmt.Errorf("inventory cgroup path %q must be an exact canonical absolute path", key)
		}
		if previous, exists := owners[key]; exists && !reflect.DeepEqual(previous, row) {
			return nil, fmt.Errorf("ambiguous inventory ownership for %q", key)
		}
		owners[key] = row
	}
	keys := make([]string, 0, len(owners))
	for key := range owners {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	if r.Source == "cgroups" {
		for _, key := range keys {
			for parent := path.Dir(key); parent != key; parent = path.Dir(parent) {
				if _, exists := owners[parent]; exists {
					return nil, fmt.Errorf("overlapping cgroup paths %q and %q would double-count descendants", parent, key)
				}
				if parent == "/" {
					break
				}
			}
		}
	}
	return func(ctx context.Context, selection MonitorRequest) (Snapshot, error) {
		selection.Using = nil
		result := Snapshot{Source: r.Source, Time: time.Now()}
		records := make([]map[string]any, 0)
		seen := make(map[string]bool)
		appendSnapshot := func(snapshot Snapshot) error {
			if snapshot.Source != r.Source {
				return errors.New("snapshot source mismatch")
			}
			rows, err := sampleRecords(snapshot)
			if err != nil {
				return err
			}
			if len(rows) > maxAggregateGroups {
				return errors.New("correlation target exceeds 4096 records")
			}
			for _, row := range rows {
				key, _ := row[c.Field].(string)
				if r.Source == "cgroups" && key != selection.Path {
					return errors.New("cgroup collector returned a path outside the exact selection")
				}
				owner, ok := owners[key]
				if !ok {
					continue
				}
				var identity []any
				for _, field := range sampledSources[r.Source].identity {
					identity = append(identity, row[field])
				}
				encoded, _ := json.Marshal(identity)
				if seen[string(encoded)] {
					return errors.New("duplicate target identity in correlated snapshot")
				}
				seen[string(encoded)] = true
				for field, value := range owner {
					row["inventory."+field] = value
				}
				records = append(records, row)
				if len(records) > maxAggregateGroups {
					return errors.New("correlation exceeds 4096 matched records")
				}
			}
			return nil
		}
		if len(keys) != 0 {
			if r.Source == "cgroups" {
				for _, key := range keys {
					if ctx.Err() != nil {
						return Snapshot{}, ctx.Err()
					}
					selection.Path = key // Push exact paths down; never collect the whole hierarchy.
					snapshot, err := collect(ctx, selection)
					if err != nil {
						return Snapshot{}, err
					}
					if err := appendSnapshot(snapshot); err != nil {
						return Snapshot{}, err
					}
				}
			} else {
				snapshot, err := collect(ctx, selection)
				if err != nil {
					return Snapshot{}, err
				}
				if err := appendSnapshot(snapshot); err != nil {
					return Snapshot{}, err
				}
			}
		}
		if ctx.Err() != nil {
			return Snapshot{}, ctx.Err()
		}
		result.Data = records
		result.sampleRows = records
		return result, nil
	}, nil
}
