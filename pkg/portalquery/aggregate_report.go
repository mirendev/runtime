package query

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"slices"
	"strings"

	p "github.com/lab47/peggysue"
)

// AggregateExpression is a bounded arithmetic tree over completed numeric metrics.
// Op is number, field, neg, +, -, *, or /; leaf nodes use Value.
type AggregateExpression struct {
	Op    string                `json:"op"`
	Value string                `json:"value,omitempty"`
	Args  []AggregateExpression `json:"args,omitempty"`
}

type AggregateComputedColumn struct {
	Name       string              `json:"name"`
	Expression AggregateExpression `json:"expression"`
}

var aggregateDecimal = regexp.MustCompile(`\A(?:[0-9]+(?:\.[0-9]+)?|\.[0-9]+)\z`)

func newAggregateExpressionGrammar(token func(p.Rule) p.Rule) p.Rule {
	expr, factor := p.R("aggregate-arithmetic"), p.R("aggregate-factor")
	field := token(p.Transform(p.Re(`[A-Za-z_][A-Za-z_0-9.]*`), func(s string) any {
		return AggregateExpression{Op: "field", Value: s}
	}))
	number := token(p.Transform(p.Re(`(?:[0-9]+(?:\.[0-9]+)?|\.[0-9]+)`), func(s string) any {
		return AggregateExpression{Op: "number", Value: s}
	}))
	factor.Set(p.Or(
		p.Action(p.Seq(token(p.S("-")), p.Named("operand", factor)), func(v p.Values) any {
			return AggregateExpression{Op: "neg", Args: []AggregateExpression{v.Get("operand").(AggregateExpression)}}
		}),
		p.Action(p.Seq(token(p.S("(")), p.Named("expr", expr), token(p.S(")"))), func(v p.Values) any { return v.Get("expr") }),
		number, field,
	))
	chain := func(operand p.Rule, operators p.Rule) p.Rule {
		pair := p.Action(p.Seq(p.Named("op", token(p.Capture(operators))), p.Named("right", operand)), func(v p.Values) any {
			return AggregateExpression{Op: v.Get("op").(string), Args: []AggregateExpression{v.Get("right").(AggregateExpression)}}
		})
		return p.Action(p.Seq(p.Named("left", operand), p.Named("rest", p.Many(pair, 0, -1, func(v []any) any { return v }))), func(v p.Values) any {
			left := v.Get("left").(AggregateExpression)
			for _, x := range v.Get("rest").([]any) {
				right := x.(AggregateExpression)
				left = AggregateExpression{Op: right.Op, Args: []AggregateExpression{left, right.Args[0]}}
			}
			return left
		})
	}
	expr.Set(chain(chain(factor, p.Or(p.S("*"), p.S("/"))), p.Or(p.S("+"), p.S("-"))))
	return expr
}

func validateComputedColumns(selects []AggregateComputedColumn, columns []AggregateMetric) error {
	if len(selects) > maxAggregateMetrics {
		return errors.New("select supports at most 8 computed columns")
	}
	columns = slices.Clone(columns)
	for _, column := range selects {
		if !tracepointIdentifier.MatchString(column.Name) || len(column.Name) > 64 || column.Name == "window" {
			return errors.New("computed column names must be identifiers up to 64 bytes, excluding window")
		}
		for _, existing := range columns {
			namespace, _, qualified := strings.Cut(existing.Name, ".")
			if existing.Name[strings.LastIndex(existing.Name, ".")+1:] == column.Name || qualified && namespace == column.Name {
				return fmt.Errorf("computed column %q conflicts with an output metric or table namespace", column.Name)
			}
		}
		nodes := 0
		var validate func(AggregateExpression, int) error
		validate = func(e AggregateExpression, depth int) error {
			nodes++
			if nodes > 64 || depth > 16 {
				return errors.New("computed expression exceeds 64 nodes or depth 16")
			}
			arity := 0
			switch e.Op {
			case "number":
				if len(e.Value) > 128 || !aggregateDecimal.MatchString(e.Value) {
					return errors.New("computed numeric literals must be decimal numbers up to 128 bytes")
				}
			case "field":
				if e.Value != "window.seconds" {
					index, err := reportSortIndex(AggregateReport{Sort: e.Value}, columns)
					if e.Value == "" || err != nil {
						return fmt.Errorf("unknown or ambiguous computed metric %q; use TABLE.METRIC for joins", e.Value)
					}
					if columns[index].Function == "hist" {
						return fmt.Errorf("computed metric %q is a histogram, not a number", e.Value)
					}
				}
			case "neg":
				arity = 1
			case "+", "-", "*", "/":
				arity = 2
			default:
				return fmt.Errorf("unknown computed operator %q", e.Op)
			}
			if len(e.Args) != arity || arity > 0 && e.Value != "" {
				return errors.New("invalid computed expression operands")
			}
			for _, arg := range e.Args {
				if err := validate(arg, depth+1); err != nil {
					return err
				}
			}
			return nil
		}
		if err := validate(column.Expression, 1); err != nil {
			return fmt.Errorf("computed column %s: %w", column.Name, err)
		}
		columns = append(columns, AggregateMetric{Name: column.Name, Function: "computed"})
	}
	return nil
}

func evaluateAggregateExpression(e AggregateExpression, row AggregateRow, result *AggregationResult) *big.Rat {
	switch e.Op {
	case "number":
		n, _ := new(big.Rat).SetString(e.Value)
		return n
	case "field":
		if e.Value == "window.seconds" {
			return new(big.Rat).SetFrac(big.NewInt(int64(result.End.Sub(result.Start))), big.NewInt(1e9))
		}
		index, _ := reportSortIndex(AggregateReport{Sort: e.Value}, result.Columns)
		n, _ := new(big.Rat).SetString(string(row.Values[index]))
		return n
	}
	left := evaluateAggregateExpression(e.Args[0], row, result)
	if left == nil {
		return nil
	}
	if e.Op == "neg" {
		return left.Neg(left)
	}
	right := evaluateAggregateExpression(e.Args[1], row, result)
	if right == nil {
		return nil
	}
	switch e.Op {
	case "+":
		return left.Add(left, right)
	case "-":
		return left.Sub(left, right)
	case "*":
		return left.Mul(left, right)
	case "/":
		if right.Sign() != 0 {
			return left.Quo(left, right)
		}
	}
	return nil
}

func applyComputedColumns(selects []AggregateComputedColumn, result *AggregationResult) error {
	if len(selects) == 0 {
		return nil
	}
	result.Columns = slices.Clone(result.Columns)
	for i := range result.Rows {
		result.Rows[i].Values = slices.Clone(result.Rows[i].Values)
	}
	for _, column := range selects {
		for i, row := range result.Rows {
			value := json.RawMessage("null")
			if n := evaluateAggregateExpression(column.Expression, row, result); n != nil {
				if n.Num().BitLen() > 4096 || n.Denom().BitLen() > 4096 {
					return fmt.Errorf("computed column %s exceeds the 4096-bit numeric limit", column.Name)
				}
				value = json.RawMessage(aggregateNumber(n))
			}
			result.Rows[i].Values = append(result.Rows[i].Values, value)
		}
		result.Columns = append(result.Columns, AggregateMetric{Name: column.Name, Function: "computed"})
	}
	return nil
}

func reportGroupNames(a *AggregationRequest, rollup []string) []string {
	if len(rollup) != 0 {
		return rollup
	}
	return outputGroupNames(a)
}

func validateRollup(a *AggregationRequest, keys []string) error {
	if len(keys) > 4 {
		return errors.New("rollup supports at most 4 grouping columns")
	}
	seen := make(map[string]bool)
	for _, key := range keys {
		if seen[key] || !slices.Contains(outputGroupNames(a), key) {
			return fmt.Errorf("rollup key %q must be a unique grouping column on table @%s", key, a.Table)
		}
		seen[key] = true
	}
	return nil
}

func rollupKey(keys []string) string {
	data, _ := json.Marshal(keys)
	return string(data)
}

// Prepare coarse reductions from the same observations, rather than trying to
// combine rounded means, percentile values or independently normalized rates.
// Extra state is charged to the original query's retention/group budgets.
func (r *aggregateReduction) configureRollups(rollups [][]string) {
	for _, keys := range rollups {
		key := rollupKey(keys)
		if r.rollups[key] != nil {
			continue
		}
		a := r.request
		a.GroupBy = nil
		for _, name := range keys {
			for i, alias := range outputGroupNames(&r.request) {
				if alias == name {
					a.GroupBy = append(a.GroupBy, r.request.GroupBy[i])
				}
			}
		}
		coarse := newAggregateReduction(a)
		coarse.retained, coarse.seriesGroups = r.retained, r.seriesGroups
		for _, metric := range coarse.metrics {
			metric.retained, metric.seriesGroups = r.retained, r.seriesGroups
		}
		if r.rollups == nil {
			r.rollups = make(map[string]*aggregateReduction)
		}
		r.rollups[key] = coarse
	}
}

func (r *aggregateReduction) attachRollups(result *AggregationResult, source string) {
	if len(r.rollups) == 0 {
		return
	}
	result.rollups = make(map[string]*AggregationResult)
	for key, coarse := range r.rollups {
		result.rollups[key] = coarse.result(source, result.Start, result.End).Aggregation
	}
}

func reportRollupResult(original *AggregationResult, keys []string) *AggregationResult {
	if len(keys) == 0 {
		return original
	}
	result := *original.rollups[rollupKey(keys)]
	result.Collection, result.StackCoverage = original.Collection, original.StackCoverage
	return &result
}
