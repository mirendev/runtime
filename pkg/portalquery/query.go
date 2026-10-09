package query

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	p "github.com/lab47/peggysue"
	"github.com/lab47/peggysue/toolkit"
)

type queryCondition struct {
	field, op string
	values    []string
}

type parsedMonitorQuery struct {
	source      string
	conditions  []queryCondition
	aggregation *AggregationRequest
	using       *parsedCorrelation
}

type parsedCorrelation struct {
	inventory  parsedMonitorQuery
	field, key string
}

func correlationGrammar(token func(p.Rule) p.Rule, word, where p.Rule) p.Rule {
	keyword := func(s string) p.Rule { return token(p.Seq(p.Re("(?i:"+s+")"), p.Not(p.Re(`[A-Za-z_0-9.]`)))) }
	return p.Action(p.Seq(keyword("using"), token(p.S("(")), p.Named("source", word), p.Named("where", p.Maybe(where)), token(p.S(")")), keyword("on"), p.Named("field", word), token(p.S("=")), p.Named("key", word)), func(v p.Values) any {
		c := &parsedCorrelation{inventory: parsedMonitorQuery{source: strings.ToLower(v.Get("source").(string))}, field: queryField(v.Get("field").(string)), key: queryField(v.Get("key").(string))}
		if conditions := v.Get("where"); conditions != nil {
			c.inventory.conditions = conditions.([]queryCondition)
		}
		return c
	})
}

func queryField(field string) string {
	if strings.HasPrefix(strings.ToLower(field), "field.") {
		return "field." + field[len("field."):]
	}
	return strings.ToLower(field)
}

var monitorQueryGrammar = newMonitorQueryGrammar()

func newMonitorQueryGrammar() p.Rule {
	token := toolkit.After(toolkit.WS)
	bare := p.Plus(p.Rune(func(r rune) bool {
		return !unicode.IsSpace(r) && !strings.ContainsRune(`=<>(),"'`, r)
	}))
	word := token(p.Capture(bare))
	keyword := func(s string) p.Rule {
		return token(p.Seq(p.Re("(?i:"+s+")"), p.Not(bare)))
	}
	quoted := p.Re(`(?:"(?:[^"\\]|\\["\\])*"|'(?:[^'\\]|\\['\\])*')`)
	value := token(p.Transform(p.Or(quoted, bare), func(text string) any {
		if len(text) == 0 || (text[0] != '\'' && text[0] != '"') {
			return text
		}
		if text[0] == '"' {
			decoded, _ := strconv.Unquote(text)
			return decoded
		}
		inner := text[1 : len(text)-1]
		return strings.ReplaceAll(strings.ReplaceAll(inner, `\'`, `'`), `\\`, `\`)
	}))
	collect := func(values []any) any {
		copy := make([]string, len(values))
		for i, v := range values {
			copy[i] = v.(string)
		}
		return copy
	}
	equals := p.Action(p.Seq(p.Named("op", token(p.Capture(p.Or(p.S(">="), p.S("<="), p.S("=="), p.S(">"), p.S("<"), p.S("="))))), p.Named("value", value)), func(v p.Values) any {
		op := v.Get("op").(string)
		if op == "==" {
			op = "="
		}
		return queryCondition{op: op, values: []string{v.Get("value").(string)}}
	})
	list := p.Action(p.Seq(
		keyword("in"), token(p.S("(")),
		p.Named("first", value),
		p.Named("rest", p.Many(p.Seq(token(p.S(",")), value), 0, -1, collect)),
		token(p.S(")")),
	), func(v p.Values) any {
		return queryCondition{op: "in", values: append([]string{v.Get("first").(string)}, v.Get("rest").([]string)...)}
	})
	condition := p.Action(p.Seq(p.Named("field", word), p.Named("operation", p.Or(list, equals))), func(v p.Values) any {
		result := v.Get("operation").(queryCondition)
		result.field = queryField(v.Get("field").(string))
		return result
	})
	more := p.Many(p.Seq(keyword("and"), condition), 0, -1, func(values []any) any {
		copy := make([]queryCondition, len(values))
		for i, v := range values {
			copy[i] = v.(queryCondition)
		}
		return copy
	})
	where := p.Action(p.Seq(keyword("where"), p.Named("first", condition), p.Named("rest", more)), func(v p.Values) any {
		return append([]queryCondition{v.Get("first").(queryCondition)}, v.Get("rest").([]queryCondition)...)
	})
	group := p.Action(p.Seq(keyword("by"), p.Named("first", word), p.Named("rest", p.Many(p.Seq(token(p.S(",")), word), 0, -1, collect))), func(v p.Values) any {
		fields := append([]string{v.Get("first").(string)}, v.Get("rest").([]string)...)
		for i, field := range fields {
			fields[i] = queryField(field)
		}
		return fields
	})
	metric := p.Action(p.Seq(
		p.Named("function", token(p.Capture(p.Re("(?i:sum|avg|min|max|count_distinct|hist|rate)")))),
		token(p.S("(")), p.Named("field", word), token(p.S(")")),
	), func(v p.Values) any {
		return &AggregationRequest{Function: strings.ToLower(v.Get("function").(string)), Field: queryField(v.Get("field").(string))}
	})
	percentile := p.Action(p.Seq(keyword("percentile"), token(p.S("(")), p.Named("field", word), token(p.S(",")), p.Named("percentile", word), token(p.S(")"))), func(v p.Values) any {
		percent, err := strconv.ParseFloat(v.Get("percentile").(string), 64)
		if err != nil {
			percent = -1 // Rejected by validation.
		}
		return &AggregationRequest{Function: "percentile", Field: queryField(v.Get("field").(string)), Percentile: percent}
	})
	count := p.Transform(keyword("count"), func(string) any { return &AggregationRequest{} })
	operation := p.Or(percentile, metric, count)
	additionalMetrics := p.Many(p.Seq(token(p.S(",")), operation), 0, -1, func(values []any) any {
		metrics := make([]AggregateMetric, 0, len(values))
		for _, value := range values {
			a := value.(*AggregationRequest)
			function := a.Function
			if function == "" {
				function = "count"
			}
			metrics = append(metrics, AggregateMetric{Function: function, Field: a.Field, Percentile: a.Percentile})
		}
		return metrics
	})
	every := p.Action(p.Seq(keyword("every"), p.Named("interval", word)), func(v p.Values) any {
		interval, err := time.ParseDuration(v.Get("interval").(string))
		if err != nil || interval <= 0 {
			return time.Duration(-1)
		}
		return interval
	})
	aggregate := p.Action(p.Seq(p.Named("operation", operation), p.Named("metrics", additionalMetrics), keyword("over"), p.Named("window", word), p.Named("every", p.Maybe(every)), p.Named("group", p.Maybe(group))), func(v p.Values) any {
		window, _ := time.ParseDuration(v.Get("window").(string)) // Invalid durations become zero and fail validation.
		a := v.Get("operation").(*AggregationRequest)
		if rest := v.Get("metrics").([]AggregateMetric); len(rest) != 0 {
			function := a.Function
			if function == "" {
				function = "count"
			}
			a.Metrics = append([]AggregateMetric{{Function: function, Field: a.Field, Percentile: a.Percentile}}, rest...)
			a.Function, a.Field, a.Percentile = "", "", 0
		}
		a.Window = window
		if interval := v.Get("every"); interval != nil {
			a.Every = interval.(time.Duration)
		}
		if fields := v.Get("group"); fields != nil {
			a.GroupBy = fields.([]string)
		}
		return a
	})
	return p.Action(p.Seq(toolkit.WS, p.Named("source", word), p.Named("using", p.Maybe(correlationGrammar(token, word, where))), p.Named("where", p.Maybe(where)), p.Named("aggregate", p.Maybe(aggregate)), toolkit.WS, p.EOS()), func(v p.Values) any {
		result := parsedMonitorQuery{source: strings.ToLower(v.Get("source").(string))}
		if using := v.Get("using"); using != nil {
			result.using = using.(*parsedCorrelation)
		}
		if conditions := v.Get("where"); conditions != nil {
			result.conditions = conditions.([]queryCondition)
		}
		if a := v.Get("aggregate"); a != nil {
			result.aggregation = a.(*AggregationRequest)
		}
		return result
	})
}

// ParseMonitorQuery compiles the text DSL into the validated, signed request
// used by Client.Monitor or Client.Query. The syntax is SOURCE [where FIELD = VALUE [and ...]],
// plus "syscall in (NUMBER, :NAME, ...)" and "tracepoint where event = CATEGORY:NAME
// and fields in (NAME, ...) [and field.NAME = NUMBER]". Append
// "count over DURATION [by FIELD, ...]", "FUNCTION(FIELD) over ...", or
// "percentile(FIELD, PERCENT) over ..." for a one-shot aggregation query.
// Comma-separated functions share one window, sampling interval and grouping.
func ParseMonitorQuery(query string) (MonitorRequest, error) {
	return parseMonitorQuery(query, nil)
}

func parseMonitorQuery(query string, sources map[string]CustomSource) (MonitorRequest, error) {
	if len(query) > 4096 {
		return MonitorRequest{}, errors.New("monitor query exceeds 4096 bytes")
	}
	if strings.Contains(query, "{") {
		// Quoted braces remain ordinary values in the original DSL.
		if value, matched, err := p.New().Parse(monitorQueryGrammar, query); err == nil && matched {
			return compileMonitorQuery(value.(parsedMonitorQuery), sources)
		}
		return parseProbeQuery(query, sources)
	}
	value, matched, err := p.New().Parse(monitorQueryGrammar, query, p.WithErrors())
	if err != nil {
		return MonitorRequest{}, querySyntaxError(err)
	}
	if !matched {
		return MonitorRequest{}, errors.New("invalid monitor query")
	}
	return compileMonitorQuery(value.(parsedMonitorQuery), sources)
}

func querySyntaxError(err error) error {
	var parse *p.ParseError
	if !errors.As(err, &parse) {
		return err
	}
	pos := min(max(parse.MaxPos, 0), len(parse.Input))
	start := strings.LastIndex(parse.Input[:pos], "\n") + 1
	end := len(parse.Input)
	if i := strings.IndexByte(parse.Input[pos:], '\n'); i >= 0 {
		end = pos + i
	}
	line := strings.Count(parse.Input[:pos], "\n") + 1
	column := utf8.RuneCountInString(parse.Input[start:pos]) + 1
	message := "invalid query syntax; check the selector, predicates, action and report blocks"
	// Give operator mistakes a concrete expectation without exposing PEG rules.
	prefix := strings.Fields(parse.Input[:pos])
	if len(prefix) >= 2 && (strings.EqualFold(prefix[len(prefix)-2], "where") || strings.EqualFold(prefix[len(prefix)-2], "and")) {
		message = "expected =, ==, in, >, >=, < or <= after a field name"
	}
	preview := parse.Input[start:end]
	padding := strings.Map(func(r rune) rune {
		if r == '\t' {
			return '\t'
		}
		return ' '
	}, parse.Input[start:pos])
	return fmt.Errorf("line %d, column %d: %s\n  %s\n  %s^", line, column, message, preview, padding)
}

func compileMonitorQuery(parsed parsedMonitorQuery, sources map[string]CustomSource) (MonitorRequest, error) {
	r := (Engine{Sources: sources}).bindSources(MonitorRequest{Source: parsed.source})
	if parsed.using != nil {
		inventory, err := compileMonitorQuery(parsed.using.inventory, sources)
		if err != nil {
			return MonitorRequest{}, err
		}
		inventory.Mode = "snapshot"
		r.Using = &SnapshotCorrelation{Inventory: inventory, Field: parsed.using.field, Key: parsed.using.key}
	}
	if r.Source == "symbols" {
		r.Mode = "snapshot"
		r.Symbols = &SymbolRequest{}
	}
	if !builtInSource(r.Source) && r.customSource == nil {
		return MonitorRequest{}, fmt.Errorf("unknown monitor source %q", parsed.source)
	}
	if r.Source == "cpu" || r.Source == "memory" || r.Source == "network" || r.Source == "kernel" || r.Source == "sensors" || r.Source == "containers" || r.Source == "cgroups" || r.Source == "gpu" || r.Source == "capabilities" {
		r.Mode = "snapshot"
	}
	if parsed.aggregation != nil {
		r.Mode = "aggregate"
		r.Aggregation = parsed.aggregation
	} else if r.customSource != nil && r.customSource.Events == nil {
		r.Mode = "snapshot"
	}
	seen := make(map[string]bool)
	for _, condition := range parsed.conditions {
		if len(condition.values) == 0 && !(r.Source == "tracepoint" && condition.field == "fields" && condition.op == "in" && r.Aggregation != nil && r.Aggregation.Table != "") {
			return MonitorRequest{}, fmt.Errorf("filter %q requires a value", condition.field)
		}
		key := condition.field
		if condition.op != "=" && condition.op != "in" {
			key += condition.op
		}
		if seen[key] {
			return MonitorRequest{}, fmt.Errorf("duplicate filter %q", condition.field)
		}
		seen[key] = true
		if r.customSource != nil && strings.HasPrefix(condition.field, "result.") && condition.op == "=" {
			if err := setQueryFilter(&r, condition.field, condition.values[0]); err != nil {
				return MonitorRequest{}, err
			}
			continue
		}
		if r.customSource != nil && (condition.op == "=" || condition.op == "in") {
			r.Filters = append(r.Filters, SourceFilter{Field: condition.field, Values: condition.values})
			continue
		}
		if condition.op != "=" && condition.op != "in" {
			if len(condition.values) != 1 {
				return MonitorRequest{}, errors.New("comparison requires one numeric value")
			}
			value := condition.values[0]
			if condition.field == "duration_ns" {
				// Preserve raw numeric thresholds exactly. ParseDuration requires a
				// unit, so only duration literals are converted to nanoseconds.
				if duration, err := time.ParseDuration(value); err == nil {
					value = strconv.FormatInt(int64(duration), 10)
				}
			}
			r.Comparisons = append(r.Comparisons, NumericComparison{Field: condition.field, Op: condition.op, Value: value})
			continue
		}
		if condition.op == "in" {
			if condition.field == "user.stack.from" || condition.field == "kernel.stack.from" {
				if err := setQueryFilter(&r, condition.field, condition.values[0]); err != nil {
					return MonitorRequest{}, err
				}
				shape := r.Stacks.UserShape
				if condition.field == "kernel.stack.from" {
					shape = r.Stacks.KernelShape
				}
				shape.From, shape.FromPatterns = "", condition.values
				continue
			}
			if r.Source == "symbols" && condition.field == "addresses" {
				for _, text := range condition.values {
					address, err := parseTracepointNumber(text)
					if err != nil || strings.HasPrefix(text, "-") {
						return MonitorRequest{}, fmt.Errorf("invalid symbol address %q", text)
					}
					r.Symbols.Addresses = append(r.Symbols.Addresses, address)
				}
				continue
			}
			if r.Source == "tracepoint" && condition.field == "fields" {
				if r.Tracepoint == nil {
					r.Tracepoint = &TracepointFilter{}
				}
				r.Tracepoint.Fields = condition.values
				continue
			}
			if r.Source != "syscalls" || condition.field != "syscall" {
				return MonitorRequest{}, fmt.Errorf("in is not supported for %s", condition.field)
			}
			for _, text := range condition.values {
				if err := addSyscallFilter(&r, text); err != nil {
					return MonitorRequest{}, err
				}
			}
			continue
		}
		if err := setQueryFilter(&r, condition.field, condition.values[0]); err != nil {
			return MonitorRequest{}, err
		}
	}
	if seen["file.depth"] && !r.Paths {
		return MonitorRequest{}, errors.New("file.depth requires paths=true")
	}
	if r.Source == "tracepoint" && !seen["fields"] {
		return MonitorRequest{}, errors.New("tracepoint requires fields in (...); selector/action queries infer fields automatically")
	}
	if err := r.Validate(); err != nil {
		return MonitorRequest{}, err
	}
	return r, nil
}

func setQueryFilter(r *MonitorRequest, field, value string) error {
	if r.Source == "syscalls" || r.Source == "disk" || r.Source == "tracepoint" {
		switch field {
		case "name", "process_name", "name_group", "cgroup.path", "device_name", "rwbs", "io.cgroup.path":
			if r.EventFilters == nil {
				r.EventFilters = make(map[string]string)
			}
			r.EventFilters[field] = value
			return nil
		case "file.depth":
			if r.Source != "syscalls" || r.Aggregation == nil {
				return errors.New("file.depth requires syscall path aggregation")
			}
			n, err := strconv.Atoi(value)
			if err != nil {
				return errors.New("file.depth must be an integer")
			}
			r.FileDepth = n
			return nil
		}
	}
	if strings.HasPrefix(field, "result.") {
		if r.Aggregation == nil {
			return errors.New("result options require aggregate mode")
		}
		r.Aggregation.Compact = true
		switch field {
		case "result.format":
			if value != "rows" {
				return errors.New("result.format must be rows")
			}
		case "result.nonzero":
			if value != "true" && value != "false" {
				return errors.New("result.nonzero must be true or false")
			}
			r.Aggregation.Nonzero = value == "true"
		case "result.limit", "result.sort_metric":
			n, err := strconv.Atoi(value)
			if err != nil || n < 0 {
				return errors.New("result limit/sort_metric must be nonnegative integers")
			}
			if field == "result.limit" {
				r.Aggregation.Limit = n
			} else {
				r.Aggregation.SortMetric = n
			}
		default:
			return fmt.Errorf("unknown result option %q", field)
		}
		return nil
	}
	if field == "stacks" {
		if value != "user" && value != "kernel" && value != "both" {
			return errors.New("stacks must be user, kernel or both")
		}
		if r.Stacks == nil {
			r.Stacks = &StackCapture{}
		}
		r.Stacks.User, r.Stacks.Kernel, r.Stacks.Symbolize = value == "user" || value == "both", value == "kernel" || value == "both", true
		return nil
	}
	if field == "stack.depth" {
		depth, err := strconv.Atoi(value)
		if err != nil || depth < 0 || depth > 64 {
			return errors.New("stack.depth must be between 0 and 64")
		}
		if r.Stacks == nil {
			r.Stacks = &StackCapture{}
		}
		r.Stacks.Depth = depth
		return nil
	}
	if strings.HasPrefix(field, "user.stack.") || strings.HasPrefix(field, "kernel.stack.") {
		if r.Stacks == nil {
			r.Stacks = &StackCapture{}
		}
		shape := &r.Stacks.UserShape
		if strings.HasPrefix(field, "kernel.") {
			shape = &r.Stacks.KernelShape
		}
		if *shape == nil {
			*shape = &StackShape{}
		}
		_, option, _ := strings.Cut(field, ".stack.")
		switch option {
		case "offsets":
			if value != "true" && value != "false" {
				return errors.New("stack.offsets must be true or false")
			}
			(*shape).DropOffsets = value == "false"
		case "top", "drop_bottom", "drop_top":
			n, err := strconv.Atoi(value)
			if err != nil || n < 0 || n > 64 {
				return errors.New("stack top/drop_bottom/drop_top must be between 0 and 64")
			}
			if option == "top" {
				(*shape).Top = n
			} else if option == "drop_top" {
				(*shape).DropTop = n
			} else {
				(*shape).DropBottom = n
			}
		case "until", "from":
			if value == "" {
				return errors.New("stack.until/from cannot be empty")
			}
			if option == "from" {
				(*shape).From = value
			} else {
				(*shape).Until = value
			}
		default:
			return fmt.Errorf("unknown stack option %q", field)
		}
		return nil
	}
	if r.Source == "symbols" {
		switch field {
		case "target":
			r.Symbols.Target = value
		case "name":
			r.Symbols.Name = value
		case "path":
			r.Symbols.Path = value
		case "pid":
			pid, err := strconv.ParseUint(value, 10, 32)
			if err != nil {
				return err
			}
			r.Symbols.PID = uint32(pid)
		case "limit":
			limit, err := strconv.ParseUint(value, 10, 16)
			if err != nil {
				return err
			}
			r.Symbols.Limit = int(limit)
		default:
			return fmt.Errorf("unknown symbols field %q", field)
		}
		return nil
	}
	if r.Source == "cgroups" {
		if field != "path" || value == "" {
			return fmt.Errorf("invalid cgroups field %q", field)
		}
		r.Path = value
		return nil
	}
	if r.Source == "tracepoint" {
		if r.Tracepoint == nil {
			r.Tracepoint = &TracepointFilter{}
		}
		switch {
		case field == "event":
			r.Tracepoint.Event = value
		case strings.HasPrefix(field, "field."):
			if r.Tracepoint.Equals == nil {
				r.Tracepoint.Equals = make(map[string]string)
			}
			r.Tracepoint.Equals[strings.TrimPrefix(field, "field.")] = value
		default:
			return fmt.Errorf("unknown tracepoint field %q", field)
		}
		return nil
	}
	if r.Source == "network" || r.Source == "sensors" || r.Source == "containers" || r.Source == "gpu" {
		if field != "name" || value == "" {
			return fmt.Errorf("invalid %s field %q", r.Source, field)
		}
		r.Name = value
		return nil
	}
	if r.Source == "cpu" || r.Source == "memory" || r.Source == "kernel" || r.Source == "capabilities" {
		return fmt.Errorf("unknown %s field %q", r.Source, field)
	}
	if r.Source == "disk" {
		if r.Disk == nil {
			r.Disk = &DiskFilter{}
		}
		switch field {
		case "phase":
			r.Phase = value
		case "device":
			id, err := strconv.ParseUint(value, 0, 32)
			if err != nil || id == 0 {
				return fmt.Errorf("invalid device %q", value)
			}
			r.Disk.Device = uint32(id)
		case "operation":
			r.Disk.Operation = strings.ToLower(value)
		default:
			return fmt.Errorf("unknown disk field %q", field)
		}
		return nil
	}
	if r.Source == "process" {
		switch field {
		case "pid":
			id, err := strconv.ParseUint(value, 10, 32)
			if err != nil || id == 0 {
				return fmt.Errorf("invalid pid %q", value)
			}
			r.PID = uint32(id)
		case "name", "action":
			if r.Process == nil {
				r.Process = &ProcessFilter{}
			}
			if field == "name" {
				if value == "" {
					return errors.New("process name cannot be empty")
				}
				r.Process.Name = value
			} else {
				r.Process.Action = strings.ToLower(value)
			}
		default:
			return fmt.Errorf("unknown process field %q", field)
		}
		return nil
	}
	if r.Source == "syscalls" {
		switch field {
		case "paths":
			if value != "true" && value != "false" {
				return errors.New("paths must be true or false")
			}
			r.Paths = value == "true"
		case "phase":
			r.Phase = value
		case "pid":
			id, err := strconv.ParseUint(value, 10, 32)
			if err != nil || id == 0 {
				return fmt.Errorf("invalid pid %q", value)
			}
			r.PID = uint32(id)
		case "syscall":
			return addSyscallFilter(r, value)
		default:
			return fmt.Errorf("unknown syscalls field %q", field)
		}
		return nil
	}
	if r.Packet == nil {
		r.Packet = &PacketFilter{}
	}
	switch field {
	case "protocol":
		r.Packet.Protocol = strings.ToLower(value)
	case "direction":
		r.Packet.Direction = strings.ToLower(value)
	case "src.ip":
		r.Packet.SourceIP = value
	case "dst.ip":
		r.Packet.DestinationIP = value
	case "src.port", "dst.port":
		port, err := strconv.ParseUint(value, 10, 16)
		if err != nil || port == 0 {
			return fmt.Errorf("invalid %s %q", field, value)
		}
		if field == "src.port" {
			r.Packet.SourcePort = uint16(port)
		} else {
			r.Packet.DestinationPort = uint16(port)
		}
	default:
		return fmt.Errorf("unknown packets field %q", field)
	}
	return nil
}
