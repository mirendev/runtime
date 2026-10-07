package query

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	p "github.com/lab47/peggysue"
	"github.com/lab47/peggysue/toolkit"
)

type probeExpr struct {
	kind, text string
	items      []probeExpr
	keys       []string
}

type probeStatement struct {
	name   string
	groups []probeExpr
	value  probeExpr
	local  bool
}

type probeReport struct {
	kind, duration, table, sort, limit string
	clear, stop                        bool
	ascending                          bool
	joinKind, right                    string
	on, clears                         []string
	leftRollup, rightRollup            []string
	selects                            []AggregateComputedColumn
}

type probeProgram struct {
	selector   string
	conditions []queryCondition
	using      *parsedCorrelation
	statements []probeStatement
	reports    []probeReport
}

type probeScript struct {
	probes       []probeProgram
	reports      []probeReport
	reportBlocks int
}

var probeGrammar = newProbeGrammar()

func newProbeGrammar() p.Rule {
	token := toolkit.After(toolkit.WS)
	sym := func(s string) p.Rule { return token(p.S(s)) }
	id := token(p.Capture(p.Re(`[A-Za-z_][A-Za-z_0-9.]*`)))
	word := token(p.Capture(p.Re(`(?:[A-Za-z_][A-Za-z_0-9.:]*|:[A-Za-z_][A-Za-z_0-9]*|-?[0-9][A-Za-z_0-9.µμ]*)`)))
	kw := func(s string) p.Rule { return token(p.Seq(p.S(s), p.Not(p.Re(`[A-Za-z_0-9.]`)))) }
	expr := p.R("probe-expression")
	collect := func(v []any) any {
		out := make([]probeExpr, len(v))
		for i := range v {
			out[i] = v[i].(probeExpr)
		}
		return out
	}
	list := func(open, close string, item p.Rule) p.Rule {
		return p.Action(p.Seq(sym(open), p.Named("items", p.Maybe(p.Action(p.Seq(p.Named("first", item), p.Named("rest", p.Many(p.Seq(sym(","), item), 0, -1, collect))), func(v p.Values) any {
			return append([]probeExpr{v.Get("first").(probeExpr)}, v.Get("rest").([]probeExpr)...)
		}))), sym(close)), func(v p.Values) any {
			e := probeExpr{kind: "list"}
			if x := v.Get("items"); x != nil {
				e.items = x.([]probeExpr)
			}
			return e
		})
	}
	entry := p.Action(p.Seq(p.Named("key", id), sym(":"), p.Named("value", expr)), func(v p.Values) any {
		return probeExpr{keys: []string{v.Get("key").(string)}, items: []probeExpr{v.Get("value").(probeExpr)}}
	})
	entries := p.Action(p.Seq(p.Named("first", entry), p.Named("rest", p.Many(p.Seq(sym(","), entry), 0, -1, collect))), func(v p.Values) any {
		return append([]probeExpr{v.Get("first").(probeExpr)}, v.Get("rest").([]probeExpr)...)
	})
	dict := p.Action(p.Seq(sym("{"), p.Named("entries", entries), sym("}")), func(v p.Values) any {
		e := probeExpr{kind: "object"}
		for _, pair := range v.Get("entries").([]probeExpr) {
			e.keys = append(e.keys, pair.keys[0])
			e.items = append(e.items, pair.items[0])
		}
		return e
	})
	call := p.Action(p.Seq(p.Named("function", id), sym("("), p.Named("args", p.Maybe(p.Or(entries, p.Action(p.Seq(p.Named("first", expr), p.Named("rest", p.Many(p.Seq(sym(","), expr), 0, -1, collect))), func(v p.Values) any {
		return append([]probeExpr{v.Get("first").(probeExpr)}, v.Get("rest").([]probeExpr)...)
	})))), sym(")")), func(v p.Values) any {
		e := probeExpr{kind: "call", text: v.Get("function").(string)}
		if args := v.Get("args"); args != nil {
			for _, arg := range args.([]probeExpr) {
				if len(arg.keys) != 0 {
					e.keys = append(e.keys, arg.keys[0])
					e.items = append(e.items, arg.items[0])
				} else {
					e.items = append(e.items, arg)
				}
			}
		}
		return e
	})
	quoted := token(p.Transform(p.Re(`"(?:[^"\\]|\\["\\])*"`), func(s string) any {
		text, err := strconv.Unquote(s)
		if err != nil {
			return probeExpr{kind: "invalid"}
		}
		return probeExpr{kind: "string", text: text}
	}))
	atom := p.Action(p.Named("value", word), func(v p.Values) any { return probeExpr{kind: "atom", text: v.Get("value").(string)} })
	expr.Set(p.Or(call, list("[", "]", expr), dict, quoted, atom))
	condition := p.Action(p.Seq(p.Named("field", id), p.Named("op", p.Or(p.Transform(kw("in"), func(string) any { return "in" }), token(p.Transform(p.Or(p.S(">="), p.S("<="), p.S("=="), p.S(">"), p.S("<"), p.S("=")), func(s string) any {
		if s == "==" {
			return "="
		}
		return s
	})))), p.Named("value", p.Or(list("(", ")", expr), expr))), func(v p.Values) any {
		e := v.Get("value").(probeExpr)
		values := []string{e.text}
		if e.kind == "list" {
			values = nil
			for _, item := range e.items {
				if item.kind != "atom" && item.kind != "string" {
					values = nil
					break
				}
				values = append(values, item.text)
			}
		}
		// Unsupported predicate expressions are rejected rather than interpreted.
		if e.kind != "atom" && e.kind != "string" && e.kind != "list" {
			values = nil
		}
		if e.kind == "list" && v.Get("op").(string) != "in" {
			values = nil
		}
		return queryCondition{field: queryField(v.Get("field").(string)), op: v.Get("op").(string), values: values}
	})
	conditions := p.Action(p.Seq(kw("where"), p.Named("first", condition), p.Named("rest", p.Many(p.Seq(kw("and"), condition), 0, -1, func(v []any) any {
		out := []queryCondition{}
		for _, x := range v {
			out = append(out, x.(queryCondition))
		}
		return out
	}))), func(v p.Values) any {
		return append([]queryCondition{v.Get("first").(queryCondition)}, v.Get("rest").([]queryCondition)...)
	})
	local := p.Action(p.Seq(kw("let"), p.Named("name", id), sym("="), p.Named("value", expr)), func(v p.Values) any {
		return probeStatement{name: v.Get("name").(string), value: v.Get("value").(probeExpr), local: true}
	})
	table := p.Action(p.Seq(sym("@"), p.Named("name", id), p.Named("groups", list("[", "]", p.Or(entry, expr))), sym("="), p.Named("value", expr)), func(v p.Values) any {
		return probeStatement{name: v.Get("name").(string), groups: v.Get("groups").(probeExpr).items, value: v.Get("value").(probeExpr)}
	})
	// Use an action so the optional ordering retains the column, not keyword.
	order := p.Action(p.Seq(kw("order"), kw("by"), p.Named("field", id), p.Named("ascending", p.Or(p.Transform(kw("asc"), func(string) any { return true }), p.Transform(kw("desc"), func(string) any { return false })))), func(v p.Values) any {
		return probeReport{sort: v.Get("field").(string), ascending: v.Get("ascending").(bool)}
	})
	groupNames := p.Action(p.Seq(p.Named("first", id), p.Named("rest", p.Many(p.Seq(sym(","), id), 0, -1, func(v []any) any { return v }))), func(v p.Values) any {
		names := []string{v.Get("first").(string)}
		for _, x := range v.Get("rest").([]any) {
			names = append(names, x.(string))
		}
		return names
	})
	rollup := p.Seq(kw("rollup"), kw("by"), groupNames)
	join := p.Action(p.Seq(p.Named("kind", p.Or(p.Transform(kw("inner"), func(string) any { return "inner" }), p.Transform(kw("left"), func(string) any { return "left" }), p.Transform(kw("full"), func(string) any { return "full" }))), kw("join"), sym("@"), p.Named("right", id), p.Named("rollup", p.Maybe(rollup)), kw("on"), p.Named("first", id), p.Named("rest", p.Many(p.Seq(sym(","), id), 0, -1, func(v []any) any { return v }))), func(v p.Values) any {
		r := probeReport{joinKind: v.Get("kind").(string), right: v.Get("right").(string), on: []string{v.Get("first").(string)}}
		if x := v.Get("rollup"); x != nil {
			r.rightRollup = x.([]string)
		}
		for _, x := range v.Get("rest").([]any) {
			r.on = append(r.on, x.(string))
		}
		return r
	})
	selectColumn := p.Action(p.Seq(p.Named("name", id), sym("="), p.Named("expr", newAggregateExpressionGrammar(token))), func(v p.Values) any {
		return AggregateComputedColumn{Name: v.Get("name").(string), Expression: v.Get("expr").(AggregateExpression)}
	})
	selects := p.Action(p.Seq(kw("select"), p.Named("first", selectColumn), p.Named("rest", p.Many(p.Seq(sym(","), selectColumn), 0, -1, func(v []any) any { return v }))), func(v p.Values) any {
		out := []AggregateComputedColumn{v.Get("first").(AggregateComputedColumn)}
		for _, x := range v.Get("rest").([]any) {
			out = append(out, x.(AggregateComputedColumn))
		}
		return out
	})
	emit := p.Action(p.Seq(kw("emit"), sym("@"), p.Named("table", id), p.Named("rollup", p.Maybe(rollup)), p.Named("join", p.Maybe(join)), p.Named("select", p.Maybe(selects)), p.Named("sort", p.Maybe(order)), p.Named("limit", p.Maybe(p.Seq(kw("limit"), word)))), func(v p.Values) any {
		r := probeReport{table: v.Get("table").(string)}
		if x := v.Get("rollup"); x != nil {
			r.leftRollup = x.([]string)
		}
		if x := v.Get("select"); x != nil {
			r.selects = x.([]AggregateComputedColumn)
		}
		if x := v.Get("join"); x != nil {
			j := x.(probeReport)
			r.joinKind, r.right, r.on, r.rightRollup = j.joinKind, j.right, j.on, j.rightRollup
		}
		if x := v.Get("sort"); x != nil {
			sort := x.(probeReport)
			r.sort, r.ascending = sort.sort, sort.ascending
		}
		if x := v.Get("limit"); x != nil {
			r.limit = x.(string)
		}
		return r
	})
	clear := p.Seq(kw("clear"), sym("@"), id)
	reportAction := p.Action(p.Seq(p.Named("action", p.Or(emit, p.Transform(kw("stop"), func(string) any { return probeReport{stop: true} }))), p.Maybe(sym(";")), p.Named("clear", p.Many(p.Seq(clear, p.Maybe(sym(";"))), 0, -1, func(v []any) any { return v }))), func(v p.Values) any {
		r := v.Get("action").(probeReport)
		for _, x := range v.Get("clear").([]any) {
			r.clears = append(r.clears, x.(string))
		}
		r.clear = slices.Contains(r.clears, r.table)
		return r
	})
	report := p.Action(p.Seq(p.Named("kind", p.Or(p.Transform(kw("after"), func(string) any { return "after" }), p.Transform(kw("every"), func(string) any { return "every" }))), p.Named("duration", word), sym("{"), p.Named("actions", p.Many(reportAction, 1, -1, func(v []any) any { return append([]any(nil), v...) })), sym("}")), func(v p.Values) any {
		out := []probeReport{}
		for _, action := range v.Get("actions").([]any) {
			r := action.(probeReport)
			r.kind, r.duration = v.Get("kind").(string), v.Get("duration").(string)
			out = append(out, r)
		}
		return out
	})
	statement := p.Action(p.Seq(p.Named("statement", p.Or(local, table)), p.Maybe(sym(";"))), func(v p.Values) any { return v.Get("statement") })
	probe := p.Action(p.Seq(p.Named("selector", word), p.Named("using", p.Maybe(correlationGrammar(token, word, conditions))), p.Named("where", p.Maybe(conditions)), sym("{"), p.Named("statements", p.Many(statement, 1, -1, func(v []any) any {
		out := []probeStatement{}
		for _, x := range v {
			out = append(out, x.(probeStatement))
		}
		return out
	})), sym("}")), func(v p.Values) any {
		out := probeProgram{selector: v.Get("selector").(string), statements: v.Get("statements").([]probeStatement)}
		if x := v.Get("using"); x != nil {
			out.using = x.(*parsedCorrelation)
		}
		if x := v.Get("where"); x != nil {
			out.conditions = x.([]queryCondition)
		}
		return out
	})
	return p.Action(p.Seq(toolkit.WS, p.Named("probes", p.Many(probe, 1, -1, func(v []any) any { return append([]any(nil), v...) })), p.Named("reports", p.Many(report, 1, 2, func(v []any) any { return append([]any(nil), v...) })), toolkit.WS, p.EOS()), func(v p.Values) any {
		out := probeScript{reportBlocks: len(v.Get("reports").([]any))}
		for _, x := range v.Get("probes").([]any) {
			out.probes = append(out.probes, x.(probeProgram))
		}
		for _, x := range v.Get("reports").([]any) {
			out.reports = append(out.reports, x.([]probeReport)...)
		}
		return out
	})
}

func parseProbeQuery(text string, sources map[string]CustomSource) (MonitorRequest, error) {
	v, ok, err := p.New().Parse(probeGrammar, text, p.WithErrors())
	if err != nil {
		return MonitorRequest{}, querySyntaxError(err)
	}
	if !ok {
		return MonitorRequest{}, errors.New("invalid selector/action query")
	}
	script := v.(probeScript)
	if script.reportBlocks == 2 && (script.reports[0].kind != "every" || script.reports[len(script.reports)-1].kind != "after") {
		return MonitorRequest{}, errors.New("reporting requires one after block, optionally preceded by one every block")
	}
	if slices.ContainsFunc(script.reports, func(r probeReport) bool { return r.right != "" || len(r.leftRollup) != 0 || len(r.selects) != 0 }) {
		return compileJoinedScript(script, sources)
	}
	for _, report := range script.reports {
		if len(report.clears) > 1 || len(report.clears) == 1 && (report.stop || report.clears[0] != report.table) {
			return MonitorRequest{}, errors.New("clear must name the emitted table")
		}
	}
	if len(script.probes) == 1 {
		script.probes[0].reports = script.reports
		return compileProbe(script.probes[0], sources)
	}
	if len(script.probes) > maxScriptProbes {
		return MonitorRequest{}, fmt.Errorf("scripts support at most %d selectors", maxScriptProbes)
	}
	tables := make(map[string]bool)
	for _, probe := range script.probes {
		for _, statement := range probe.statements {
			if !statement.local {
				if tables[statement.name] {
					return MonitorRequest{}, fmt.Errorf("duplicate table @%s", statement.name)
				}
				tables[statement.name] = true
			}
		}
	}
	for _, report := range script.reports {
		if !report.stop && !tables[report.table] {
			return MonitorRequest{}, fmt.Errorf("emit/clear references unknown table @%s", report.table)
		}
	}
	r := MonitorRequest{Source: "script", Mode: "aggregate"}
	for _, probe := range script.probes {
		for _, report := range script.reports {
			for _, statement := range probe.statements {
				if !statement.local && (report.stop || report.table == statement.name) {
					probe.reports = append(probe.reports, report)
				}
			}
		}
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

func compileProbe(program probeProgram, sources map[string]CustomSource) (MonitorRequest, error) {
	parts := strings.Split(program.selector, ":")
	parsed := parsedMonitorQuery{source: parts[0], conditions: program.conditions, aggregation: &AggregationRequest{Compact: true}, using: program.using}
	if len(parts) > 1 {
		switch {
		case (parts[0] == "syscalls" || parts[0] == "disk") && len(parts) == 2:
			parsed.conditions = append(parsed.conditions, queryCondition{"phase", "=", []string{parts[1]}})
		case parts[0] == "process" && len(parts) == 2:
			parsed.conditions = append(parsed.conditions, queryCondition{"action", "=", []string{parts[1]}})
		case parts[0] == "tracepoint" && len(parts) == 3:
			parsed.conditions = append(parsed.conditions, queryCondition{"event", "=", []string{parts[1] + ":" + parts[2]}})
		default:
			return MonitorRequest{}, errors.New("unsupported probe selector")
		}
	}
	for _, c := range parsed.conditions {
		if len(c.values) == 0 {
			return MonitorRequest{}, errors.New("predicate requires scalar values or a nonempty scalar list")
		}
		if c.field == "stacks" || c.field == "paths" || c.field == "phase" && len(parts) == 1 {
			return MonitorRequest{}, errors.New("capture controls belong in the selector/action, not where")
		}
	}
	a := parsed.aggregation
	locals := make(map[string]string)
	aliases := make(map[string]string)
	paths, user, kernel := false, false, false
	var inferred []string
	var resolve func(probeExpr) (string, error)
	resolve = func(e probeExpr) (string, error) {
		if e.kind == "atom" {
			if field, ok := locals[e.text]; ok {
				return field, nil
			}
			if e.text == "file.path" || e.text == "file.dir" {
				paths = true
			}
			field := queryField(e.text)
			if parsed.source == "tracepoint" && strings.HasPrefix(field, "field.") {
				inferred = append(inferred, strings.TrimPrefix(field, "field."))
			}
			return field, nil
		}
		if e.kind != "call" {
			return "", errors.New("grouping requires a field, local projection or supported capture function")
		}
		switch e.text {
		case "path.prefix":
			if len(e.keys) != 0 || len(e.items) != 2 || e.items[0].kind != "atom" || e.items[0].text != "file.path" || e.items[1].kind != "atom" {
				return "", errors.New("path.prefix requires (file.path, DEPTH)")
			}
			paths = true
			parsed.conditions = append(parsed.conditions, queryCondition{"file.depth", "=", []string{e.items[1].text}})
			return "file.dir", nil
		case "stack.user", "stack.kernel":
			prefix := "user.stack"
			if e.text == "stack.user" {
				user = true
			} else {
				kernel = true
				prefix = "kernel.stack"
			}
			if len(e.keys) != len(e.items) {
				return "", errors.New("stack capture accepts named options only")
			}
			for i, key := range e.keys {
				option := e.items[i]
				values, op := []string{option.text}, "="
				if option.kind == "list" {
					if key != "from" {
						return "", errors.New("only stack from accepts alternatives")
					}
					values, op = nil, "in"
					for _, item := range option.items {
						value, err := probePattern(item)
						if err != nil {
							return "", err
						}
						values = append(values, value)
					}
				} else if key == "from" || key == "until" {
					value, err := probePattern(option)
					if err != nil {
						return "", err
					}
					values = []string{value}
				} else if option.kind != "atom" {
					return "", errors.New("stack option requires a scalar")
				}
				parsed.conditions = append(parsed.conditions, queryCondition{prefix + "." + key, op, values})
			}
			return prefix, nil
		default:
			return "", fmt.Errorf("unsupported action function %q", e.text)
		}
	}
	table := ""
	for _, statement := range program.statements {
		if statement.local {
			if !tracepointIdentifier.MatchString(statement.name) || locals[statement.name] != "" || table != "" {
				return MonitorRequest{}, errors.New("locals must be unique identifiers defined before the table")
			}
			field, err := resolve(statement.value)
			if err != nil {
				return MonitorRequest{}, err
			}
			locals[statement.name] = field
			continue
		}
		if table != "" || !tracepointIdentifier.MatchString(statement.name) {
			return MonitorRequest{}, errors.New("one named aggregate table is supported per probe")
		}
		table = statement.name
		for _, group := range statement.groups {
			alias := ""
			if group.kind == "" && len(group.keys) != 0 {
				alias, group = group.keys[0], group.items[0]
			}
			field, err := resolve(group)
			if err != nil {
				return MonitorRequest{}, err
			}
			a.GroupBy = append(a.GroupBy, field)
			if alias != "" {
				aliases[field] = alias
			} else if locals[group.text] != "" {
				aliases[field] = group.text
			}
		}
		metrics := statement.value
		if metrics.kind != "object" {
			metrics = probeExpr{kind: "object", keys: []string{"value"}, items: []probeExpr{metrics}}
		}
		for i, metric := range metrics.items {
			if metric.kind != "call" || len(metric.keys) != 0 {
				return MonitorRequest{}, errors.New("table values must be aggregate function calls")
			}
			m := AggregateMetric{Name: metrics.keys[i], Function: metric.text}
			want := 1
			if metric.text == "count" {
				want = 0
			} else if metric.text == "percentile" {
				want = 2
			}
			if len(metric.items) != want {
				return MonitorRequest{}, fmt.Errorf("wrong argument count for %s", metric.text)
			}
			if want != 0 {
				field, err := resolve(metric.items[0])
				if err != nil {
					return MonitorRequest{}, err
				}
				m.Field = field
			}
			if want == 2 {
				var err error
				m.Percentile, err = strconv.ParseFloat(metric.items[1].text, 64)
				if err != nil {
					return MonitorRequest{}, err
				}
			}
			a.Metrics = append(a.Metrics, m)
		}
	}
	if table == "" {
		return MonitorRequest{}, errors.New("aggregate table required")
	}
	a.Table, a.GroupAliases = table, aliases
	emitted := false
	for _, report := range program.reports {
		if !report.stop && report.table == table {
			emitted = true
			break
		}
	}
	if !emitted {
		return MonitorRequest{}, fmt.Errorf("table @%s is not emitted", table)
	}
	if parsed.source == "tracepoint" {
		for _, c := range parsed.conditions {
			if c.field == "fields" {
				continue
			}
			if strings.HasPrefix(c.field, "field.") {
				inferred = append(inferred, strings.TrimPrefix(c.field, "field."))
			}
		}
		selection := -1
		for i, c := range parsed.conditions {
			if c.field == "fields" {
				selection = i
				break
			}
		}
		if selection == -1 {
			parsed.conditions = append(parsed.conditions, queryCondition{field: "fields", op: "in"})
			selection = len(parsed.conditions) - 1
		}
		for _, field := range inferred {
			if !slices.Contains(parsed.conditions[selection].values, field) {
				parsed.conditions[selection].values = append(parsed.conditions[selection].values, field)
			}
		}
	}
	if paths {
		parsed.conditions = append(parsed.conditions, queryCondition{"paths", "=", []string{"true"}})
	}
	if user || kernel {
		stacks := "user"
		if kernel {
			stacks = "kernel"
		}
		if user && kernel {
			stacks = "both"
		}
		parsed.conditions = append(parsed.conditions, queryCondition{"stacks", "=", []string{stacks}})
	}
	for _, report := range program.reports {
		d, err := time.ParseDuration(report.duration)
		if err != nil || d <= 0 {
			return MonitorRequest{}, errors.New("report duration must be positive")
		}
		if report.kind == "after" {
			if a.Window != 0 {
				return MonitorRequest{}, errors.New("duplicate after block")
			}
			a.Window = d
		} else {
			if a.ReportEvery != 0 {
				return MonitorRequest{}, errors.New("duplicate every block")
			}
			a.ReportEvery = d
		}
		if report.stop {
			if report.kind != "after" || report.clear {
				return MonitorRequest{}, errors.New("stop requires an after block")
			}
			continue
		}
		if report.table != table || (report.kind == "every" && !report.clear) || (report.kind == "after" && report.clear) {
			return MonitorRequest{}, errors.New("emit must name the table; every requires clear of that table")
		}
		if report.sort != "" {
			a.Ascending = report.ascending
			found := false
			for i, m := range a.Metrics {
				if m.Name == report.sort {
					a.SortMetric, found = i, true
				}
			}
			if !found {
				return MonitorRequest{}, errors.New("order by must name a metric")
			}
		}
		if report.limit != "" {
			a.Limit, err = strconv.Atoi(report.limit)
			if err != nil || a.Limit <= 0 {
				return MonitorRequest{}, errors.New("limit must be a positive integer")
			}
		}
	}
	if a.ReportEvery != 0 {
		if len(program.reports) != 2 || program.reports[0].kind != "every" || !program.reports[1].stop {
			return MonitorRequest{}, errors.New("periodic reports require every { emit; clear } followed by after { stop }")
		}
	} else if len(program.reports) != 1 || program.reports[0].stop {
		return MonitorRequest{}, errors.New("one-shot report requires after { emit }")
	}
	r, err := compileMonitorQuery(parsed, sources)
	if err != nil {
		return MonitorRequest{}, err
	}
	fields, _, err := aggregateFields(r)
	if err != nil {
		return MonitorRequest{}, err
	}
	for _, field := range locals {
		valid := false
		for _, allowed := range fields {
			if field == allowed {
				valid = true
				break
			}
		}
		if !valid {
			return MonitorRequest{}, fmt.Errorf("local references unknown field %q", field)
		}
	}
	return r, nil
}

func probePattern(e probeExpr) (string, error) {
	if e.kind == "string" && !strings.HasPrefix(e.text, "*") && !strings.HasSuffix(e.text, "*") && e.text != "" {
		return e.text, nil
	}
	if e.kind == "call" && e.text == "glob" && len(e.keys) == 0 && len(e.items) == 1 && e.items[0].kind == "string" && validEdgeGlob(e.items[0].text) {
		return e.items[0].text, nil
	}
	return "", errors.New("stack pattern requires a literal quoted name or glob(\"prefix*\"); edge stars in literal names are unsupported")
}
