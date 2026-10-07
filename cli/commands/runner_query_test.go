package commands

import (
	"bytes"
	"context"
	"strings"
	"testing"

	query "miren.dev/runtime/pkg/portalquery"
)

func TestRunnerQueryReferenceIsOffline(t *testing.T) {
	var stdout bytes.Buffer
	ctx := &Context{Context: context.Background(), Stdout: &stdout}

	err := RunnerQuery(ctx, struct {
		ConfigCentric
		Node       string `position:"0" usage:"Runner to query (name, ID, or short ID)"`
		Expression string `position:"1" usage:"Portal monitoring query expression (not SQL); quote expressions containing spaces"`
		Reference  bool   `long:"reference" description:"Print the offline query syntax and source reference"`
	}{
		ConfigCentric: ConfigCentric{Config: "/does/not/exist"},
		Reference:     true,
	})
	if err != nil {
		t.Fatalf("reference failed without config: %v", err)
	}

	for _, want := range []string{
		"sandboxes", "sandbox_events", "snapshot only", "COMPACT GRAMMAR",
		"SELECTOR/ACTION BLOCK GRAMMAR", "JOINS, ROLLUPS AND COMPUTED COLUMNS",
		"SYMBOLS AND STACKS", "RESULT JSON AND LIMITS", "ENGINE-DERIVED FIELD APPENDIX",
		"io.write_ios: integer, counter, operations", "cpu_percent: number, utilization",
		"file.path", "return_value", "NOT exposed", "ONE-MINUTE DEADLINE",
		"INVENTORY-DRIVEN SNAPSHOT CORRELATION", "on path = cgroup", "inventory.app",
		"frozen once per selector", "Parent/child cgroup selections fail",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("reference does not contain %q", want)
		}
	}
	if strings.Contains(stdout.String(), "https://") || strings.Contains(stdout.String(), "http://") {
		t.Error("reference must be self-contained, not send the reader to a URL")
	}
}

func TestRunnerQueryRequiresRunnerAndExpression(t *testing.T) {
	ctx := &Context{Context: context.Background(), Stdout: &bytes.Buffer{}}

	err := RunnerQuery(ctx, struct {
		ConfigCentric
		Node       string `position:"0" usage:"Runner to query (name, ID, or short ID)"`
		Expression string `position:"1" usage:"Portal monitoring query expression (not SQL); quote expressions containing spaces"`
		Reference  bool   `long:"reference" description:"Print the offline query syntax and source reference"`
	}{})
	if err == nil || !strings.Contains(err.Error(), "runner and query expression are required") {
		t.Fatalf("missing arguments returned %v", err)
	}
}

func TestRunnerQueryReferenceExamplesParse(t *testing.T) {
	engine := query.Engine{Sources: map[string]query.CustomSource{
		"sandboxes": {
			Fields:        []string{"sandbox_id", "container_id", "app", "version", "pid", "state", "cgroup"},
			NumericFields: []string{"pid"},
			Snapshots:     func(context.Context, query.MonitorRequest) (query.Snapshot, error) { return query.Snapshot{}, nil },
		},
		"sandbox_events": {
			Fields:        []string{"sandbox_id", "container_id", "app", "version", "action", "pid", "exit_status"},
			NumericFields: []string{"pid", "exit_status"},
			Events:        func(context.Context, query.MonitorRequest, func(query.Event) error) error { return nil },
		},
	}}

	examples := 0
	for _, block := range strings.Split(runnerQueryReference, "\n\n") {
		if !strings.HasPrefix(block, "Example query:\n") {
			continue
		}
		expression := strings.TrimPrefix(block, "Example query:\n")
		expression = strings.TrimPrefix(strings.ReplaceAll(expression, "\n  ", "\n"), "  ")
		examples++
		request, err := engine.ParseMonitorQuery(expression)
		if err != nil {
			t.Errorf("reference example %q does not parse: %v", expression, err)
			continue
		}
		if err := engine.Validate(request); err != nil {
			t.Errorf("reference example %q does not validate: %v", expression, err)
		}
	}
	if examples < 19 {
		t.Fatalf("only checked %d embedded examples; expected the full reference", examples)
	}
}
