package query

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// AggregateReport emits a table or a one-to-one join of two completed tables.
// Sort is a metric name (unambiguous) or TABLE.METRIC; limits apply after joining.
type AggregateReport struct {
	Left        string                    `json:"left"`
	Right       string                    `json:"right,omitempty"`
	Kind        string                    `json:"kind,omitempty"` // inner, left, full; empty without Right
	On          []string                  `json:"on,omitempty"`
	LeftRollup  []string                  `json:"left_rollup,omitempty"`
	RightRollup []string                  `json:"right_rollup,omitempty"`
	Select      []AggregateComputedColumn `json:"select,omitempty"` // appended numeric columns, evaluated before sorting/limit
	Sort        string                    `json:"sort,omitempty"`
	Limit       int                       `json:"limit,omitempty"`
	Ascending   bool                      `json:"ascending,omitempty"`
}

func compileJoinedScript(script probeScript, sources map[string]CustomSource) (MonitorRequest, error) {
	if len(script.probes) > maxScriptProbes {
		return MonitorRequest{}, errors.New("scripts support at most 8 selectors")
	}
	periodic := script.reportBlocks == 2
	used, cleared := make(map[string]bool), make(map[string]bool)
	r := MonitorRequest{Source: "script", Mode: "aggregate"}
	var timing []probeReport
	stops := 0
	for _, report := range script.reports {
		if report.stop {
			stops++
			if !periodic || report.kind != "after" || len(report.clears) != 0 || stops > 1 {
				return MonitorRequest{}, errors.New("stop requires the final after block without clear")
			}
			continue
		}
		if report.kind != "after" && !periodic || periodic && report.kind != "every" {
			return MonitorRequest{}, errors.New("joins require after { emit } or every { emit; clear } followed by after { stop }")
		}
		a := AggregateReport{Left: report.table, Right: report.right, Kind: report.joinKind, On: report.on, LeftRollup: report.leftRollup, RightRollup: report.rightRollup, Select: report.selects, Sort: report.sort, Ascending: report.ascending}
		if report.limit != "" {
			var err error
			a.Limit, err = strconv.Atoi(report.limit)
			if err != nil || a.Limit <= 0 {
				return MonitorRequest{}, errors.New("limit must be a positive integer")
			}
		}
		r.Reports = append(r.Reports, a)
		used[a.Left] = true
		if a.Right != "" {
			used[a.Right] = true
		}
		for _, table := range report.clears {
			if !periodic || cleared[table] {
				return MonitorRequest{}, fmt.Errorf("unexpected or duplicate clear @%s", table)
			}
			cleared[table] = true
		}
		if len(timing) == 0 {
			timing = append(timing, probeReport{kind: report.kind, duration: report.duration})
		}
	}
	if periodic {
		last := script.reports[len(script.reports)-1]
		if !last.stop {
			return MonitorRequest{}, errors.New("periodic joins require after { stop }")
		}
		timing = append(timing, last)
		for table := range used {
			if !cleared[table] {
				return MonitorRequest{}, fmt.Errorf("periodic report must clear @%s", table)
			}
		}
		for table := range cleared {
			if !used[table] {
				return MonitorRequest{}, fmt.Errorf("clear references unused table @%s", table)
			}
		}
	}
	for _, probe := range script.probes {
		var table string
		for _, statement := range probe.statements {
			if !statement.local {
				table = statement.name
			}
		}
		probe.reports = slices.Clone(timing)
		probe.reports[0].table, probe.reports[0].clear = table, periodic
		selection, err := compileProbe(probe, sources)
		if err != nil {
			return MonitorRequest{}, fmt.Errorf("selector %s: %w", probe.selector, err)
		}
		if r.Aggregation == nil {
			r.Aggregation = &AggregationRequest{Window: selection.Aggregation.Window, ReportEvery: selection.Aggregation.ReportEvery}
		}
		r.Probes = append(r.Probes, selection)
	}
	return r, r.Validate()
}

func reportColumns(a *AggregationRequest, qualified bool) []AggregateMetric {
	columns := slices.Clone(a.Metrics)
	if len(columns) == 0 {
		function := a.Function
		if function == "" {
			function = "count"
		}
		columns = []AggregateMetric{{Name: "value", Function: function, Field: a.Field, Percentile: a.Percentile}}
	}
	if qualified {
		for i := range columns {
			columns[i].Name = a.Table + "." + columns[i].Name
		}
	}
	return columns
}

func reportSortIndex(report AggregateReport, columns []AggregateMetric) (int, error) {
	if report.Sort == "" {
		return 0, nil
	}
	index := -1
	for i, column := range columns {
		name := column.Name
		if name == report.Sort || name[strings.LastIndex(name, ".")+1:] == report.Sort {
			if index >= 0 {
				return 0, fmt.Errorf("ambiguous sort metric %q; use TABLE.METRIC", report.Sort)
			}
			index = i
		}
	}
	if index < 0 {
		return 0, fmt.Errorf("unknown sort metric %q", report.Sort)
	}
	return index, nil
}

func validateAggregateReports(r MonitorRequest) error {
	if len(r.Reports) == 0 {
		return nil
	}
	if len(r.Reports) > maxScriptProbes {
		return errors.New("scripts support at most 8 output reports")
	}
	tables := make(map[string]*AggregationRequest)
	for _, probe := range r.Probes {
		a := probe.Aggregation
		if !a.Compact || a.Limit != 0 || a.Nonzero || a.SortMetric != 0 || a.Ascending {
			return errors.New("reported tables must be compact and unlimited; put sorting and limits on the report")
		}
		tables[a.Table] = a
	}
	used := make(map[string]bool)
	for _, report := range r.Reports {
		left := tables[report.Left]
		if left == nil {
			return fmt.Errorf("report references unknown table @%s", report.Left)
		}
		used[report.Left] = true
		if err := validateRollup(left, report.LeftRollup); err != nil {
			return err
		}
		columns := reportColumns(left, report.Right != "")
		if report.Right != "" {
			right := tables[report.Right]
			if right == nil || report.Right == report.Left {
				return fmt.Errorf("join requires two different known tables, got @%s and @%s", report.Left, report.Right)
			}
			if report.Kind != "inner" && report.Kind != "left" && report.Kind != "full" {
				return errors.New("join kind must be inner, left or full")
			}
			if err := validateRollup(right, report.RightRollup); err != nil {
				return err
			}
			if len(report.On) == 0 || len(report.On) > 4 {
				return errors.New("join requires 1–4 explicit group keys")
			}
			keys := make(map[string]bool)
			for _, key := range report.On {
				if keys[key] || !slices.Contains(reportGroupNames(left, report.LeftRollup), key) || !slices.Contains(reportGroupNames(right, report.RightRollup), key) {
					return fmt.Errorf("join key %q must be a unique grouping column on both tables", key)
				}
				keys[key] = true
			}
			used[report.Right] = true
			columns = append(columns, reportColumns(right, true)...)
		} else if report.Kind != "" || len(report.On) != 0 || len(report.RightRollup) != 0 {
			return errors.New("join kind/keys require a right table")
		}
		if err := validateComputedColumns(report.Select, columns); err != nil {
			return err
		}
		for _, column := range report.Select {
			columns = append(columns, AggregateMetric{Name: column.Name, Function: "computed"})
		}
		if report.Limit < 0 || report.Limit > maxAggregateGroups {
			return errors.New("report limit must be 0–4096")
		}
		index, err := reportSortIndex(report, columns)
		if err != nil {
			return err
		}
		if (report.Sort != "" || report.Limit != 0 || report.Ascending) && columns[index].Function == "hist" {
			return errors.New("histograms cannot be used as a sort metric; select a numeric metric")
		}
	}
	for name := range tables {
		if !used[name] {
			return fmt.Errorf("table @%s is not emitted", name)
		}
	}
	return nil
}

func outputGroupNames(a *AggregationRequest) []string {
	groups := slices.Clone(a.GroupBy)
	for i, field := range groups {
		if alias := a.GroupAliases[field]; alias != "" {
			groups[i] = alias
		}
	}
	return groups
}

// Scalar, type-sensitive equality; numbers compare exactly, not via float64.
// Missing/null keys never match, including another missing/null key.
func joinKey(row AggregateRow, keys []string) (string, bool) {
	var result strings.Builder
	for _, key := range keys {
		data := row.Group[key]
		if len(data) == 0 || string(data) == "null" {
			return "", false
		}
		value := string(data)
		if n, ok := new(big.Rat).SetString(value); ok {
			value = "number:" + n.RatString()
		}
		fmt.Fprintf(&result, "%d:%s", len(value), value)
	}
	return result.String(), true
}

func joinAggregateRows(report AggregateReport, left, right *AggregationResult) (*AggregationResult, error) {
	result := &AggregationResult{Table: left.Table, Join: &report, Start: left.Start, End: left.End, GroupBy: slices.Clone(report.On), Rows: []AggregateRow{}}
	if left.Every == right.Every {
		result.Every = left.Every
	}
	for _, table := range []*AggregationResult{left, right} {
		if table.Collection != nil {
			if result.Collections == nil {
				result.Collections = make(map[string]*CollectionStats)
			}
			result.Collections[table.Table] = table.Collection
		}
		if table.StackCoverage != nil {
			if result.StackCoverageByTable == nil {
				result.StackCoverageByTable = make(map[string]*StackCoverageReport)
			}
			result.StackCoverageByTable[table.Table] = table.StackCoverage
		}
	}
	for _, table := range []*AggregationResult{left, right} {
		for _, column := range table.Columns {
			column.Name = table.Table + "." + column.Name
			result.Columns = append(result.Columns, column)
		}
		for _, field := range table.GroupBy {
			if !slices.Contains(report.On, field) {
				result.GroupBy = append(result.GroupBy, table.Table+"."+field)
			}
		}
	}
	indexes := []map[string]int{make(map[string]int), make(map[string]int)}
	for side, table := range []*AggregationResult{left, right} {
		for i, row := range table.Rows {
			key, valid := joinKey(row, report.On)
			if !valid {
				continue
			}
			if _, exists := indexes[side][key]; exists {
				return nil, fmt.Errorf("table @%s has duplicate join keys (%s); group more narrowly or include more join keys", table.Table, strings.Join(report.On, ", "))
			}
			indexes[side][key] = i
		}
	}
	appendRow := func(l, r *AggregateRow) {
		row := AggregateRow{Group: make(map[string]json.RawMessage), Values: make([]json.RawMessage, len(result.Columns))}
		for i := range row.Values {
			row.Values[i] = json.RawMessage("null")
		}
		for side, source := range []*AggregateRow{l, r} {
			table := []*AggregationResult{left, right}[side]
			for _, field := range table.GroupBy {
				name := field
				if !slices.Contains(report.On, field) {
					name = table.Table + "." + field
				}
				if source != nil {
					row.Group[name] = source.Group[field]
				} else if row.Group[name] == nil {
					row.Group[name] = json.RawMessage("null")
				}
			}
			if source != nil {
				offset := 0
				if side == 1 {
					offset = len(left.Columns)
				}
				copy(row.Values[offset:], source.Values)
			}
		}
		result.Rows = append(result.Rows, row)
	}
	matched := make(map[int]bool)
	for i := range left.Rows {
		row := &left.Rows[i]
		key, valid := joinKey(*row, report.On)
		index, exists := indexes[1][key]
		if valid && exists {
			appendRow(row, &right.Rows[index])
			matched[index] = true
		} else if report.Kind != "inner" {
			appendRow(row, nil)
		}
	}
	if report.Kind == "full" {
		for i := range right.Rows {
			if !matched[i] {
				appendRow(nil, &right.Rows[i])
			}
		}
	}
	if err := applyComputedColumns(report.Select, result); err != nil {
		return nil, err
	}
	applyReportControls(report, result)
	return result, nil
}

func applyReportControls(report AggregateReport, result *AggregationResult) {
	index, _ := reportSortIndex(report, result.Columns) // validated before collection
	result.TotalGroups = len(result.Rows)
	// Deterministic ties, including null keys/values, independent of map order.
	sort.SliceStable(result.Rows, func(i, j int) bool {
		x, _ := json.Marshal(result.Rows[i].Group)
		y, _ := json.Marshal(result.Rows[j].Group)
		return string(x) < string(y)
	})
	sort.SliceStable(result.Rows, func(i, j int) bool {
		x, xok := new(big.Rat).SetString(string(result.Rows[i].Values[index]))
		y, yok := new(big.Rat).SetString(string(result.Rows[j].Values[index]))
		if !xok {
			return false
		}
		if !yok {
			return true
		}
		if report.Ascending {
			return x.Cmp(y) < 0
		}
		return x.Cmp(y) > 0
	})
	if report.Limit > 0 && len(result.Rows) > report.Limit {
		result.Rows = result.Rows[:report.Limit]
	}
}

func reportScriptTables(request MonitorRequest, tables []Snapshot) ([]Snapshot, error) {
	byName := make(map[string]Snapshot)
	for i, table := range tables {
		byName[request.Probes[i].Aggregation.Table] = table
	}
	output := make([]Snapshot, 0, len(request.Reports))
	for _, report := range request.Reports {
		left := byName[report.Left]
		right := byName[report.Right]
		joined := Snapshot{Source: left.Source, Time: left.Time}
		if report.Right != "" {
			joined.Source = "join"
		}
		combine := func(l, r *AggregationResult) (*AggregationResult, error) {
			l = reportRollupResult(l, report.LeftRollup)
			if report.Right != "" {
				r = reportRollupResult(r, report.RightRollup)
				return joinAggregateRows(report, l, r)
			}
			copy := *l
			copy.Rows = slices.Clone(l.Rows)
			if err := applyComputedColumns(report.Select, &copy); err != nil {
				return nil, err
			}
			applyReportControls(report, &copy)
			return &copy, nil
		}
		if left.Aggregation != nil {
			var err error
			joined.Aggregation, err = combine(left.Aggregation, right.Aggregation)
			if err != nil {
				return nil, err
			}
		} else {
			for i, window := range left.Windows {
				var peer *AggregationResult
				if report.Right != "" {
					peer = right.Windows[i]
				}
				result, err := combine(window, peer)
				if err != nil {
					return nil, fmt.Errorf("bucket %d: %w", i+1, err)
				}
				joined.Windows = append(joined.Windows, result)
			}
		}
		output = append(output, joined)
	}
	return output, nil
}
