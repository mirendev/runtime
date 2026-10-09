package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/containerd/containerd/v2/pkg/identifiers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"miren.dev/runtime/api/app/app_v1alpha"
	compute "miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	runapi "miren.dev/runtime/api/run"
	run_v1alpha "miren.dev/runtime/api/run/run_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/testutils"
	"miren.dev/runtime/pkg/rpc"
)

func TestSubmitRunIdentityAndWorkerHealth(t *testing.T) {
	ctx := context.Background()
	inm, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)
	app, err := inm.Client.Create(ctx, "dagster", &core_v1alpha.App{})
	require.NoError(t, err)
	cfg, err := inm.Client.Create(ctx, "worker-config", &core_v1alpha.ConfigVersion{App: app,
		Spec: core_v1alpha.ConfigSpec{Tasks: []core_v1alpha.ConfigSpecTasks{{Name: "dagster"}}}})
	require.NoError(t, err)
	ver, err := inm.Client.Create(ctx, "pinned", &core_v1alpha.AppVersion{App: app, ConfigVersion: cfg})
	require.NoError(t, err)
	r := &AppInfo{Log: slog.Default(), EC: inm.Client}
	client := &app_v1alpha.RunsClient{Client: rpc.LocalClient(app_v1alpha.AdaptRuns(r))}
	command := []string{"python", "-m", "dagster", "argument with ' quotes"}
	// All callers see one identity, even with a simultaneous absent-ID race.
	var wg sync.WaitGroup
	ids := make(chan string, 8)
	for range 8 {
		wg.Go(func() {
			ret, err := client.SubmitRun(ctx, "dagster", "dagster", command, ver.String(), "request-1")
			assert.NoError(t, err)
			if err == nil {
				ids <- ret.Id()
			}
		})
	}
	wg.Wait()
	close(ids)
	var id string
	for other := range ids {
		if id == "" {
			id = other
		}
		require.Equal(t, id, other)
	}
	require.NotEmpty(t, id)
	sb, err := inm.Client.Create(ctx, "worker", &compute.Sandbox{Status: compute.PENDING})
	require.NoError(t, err)
	require.NoError(t, inm.Client.Patch(ctx, entity.Id(id), 0,
		entity.Ref(run_v1alpha.RunStatusId, run_v1alpha.RunStatusRunningId),
		entity.Ref(run_v1alpha.RunSandboxId, sb)))
	ret, err := client.SubmitRun(ctx, "dagster", "dagster", command, ver.String(), "request-1")
	require.NoError(t, err)
	require.Equal(t, id, ret.Id())
	info, err := client.GetRun(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, "running", info.Run().Status(), "retry must not reset state")
	assert.Equal(t, "pending", info.Run().WorkerStatus(), "admission is not startup")
	require.NoError(t, inm.Client.Patch(ctx, sb, 0, entity.Ref(compute.SandboxStatusId, compute.SandboxStatusRunningId)))
	info, err = client.GetRun(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, "running", info.Run().WorkerStatus())
	_, err = client.SubmitRun(ctx, "dagster", "dagster", []string{"different"}, ver.String(), "request-1")
	require.ErrorContains(t, err, "different work")
	otherApp, err := inm.Client.Create(ctx, "other", &core_v1alpha.App{})
	require.NoError(t, err)
	foreign, err := inm.Client.Create(ctx, "foreign", &core_v1alpha.AppVersion{App: otherApp})
	require.NoError(t, err)
	_, err = client.SubmitRun(ctx, "dagster", "dagster", command, foreign.String(), "request-2")
	require.ErrorContains(t, err, "does not belong")
	denied := rpc.ContextWithIdentity(ctx, &rpc.Identity{Method: rpc.AuthMethodWorkload, Metadata: map[string]any{"app": "other"}})
	_, err = client.SubmitRun(denied, "dagster", "dagster", command, ver.String(), "request-1")
	require.Error(t, err, "deduplication must not bypass app authorization")
	for _, task := range []string{"", "console", "undeclared"} {
		_, err = client.SubmitRun(ctx, "dagster", task, command, ver.String(), "invalid-task")
		require.Error(t, err)
	}
	mux := http.NewServeMux()
	rpc.RegisterREST(mux, app_v1alpha.AdaptRuns(r))
	for _, tc := range []struct {
		name, field string
		value       any
		status      int
		code        string
	}{
		{"missing request ID", "request_id", "", http.StatusBadRequest, "validation-failure"},
		{"missing version", "version", "", http.StatusBadRequest, "validation-failure"},
		{"missing command", "command", []string{}, http.StatusBadRequest, "validation-failure"},
		{"missing task", "task", "", http.StatusBadRequest, "validation-failure"},
		{"console task", "task", "console", http.StatusBadRequest, "validation-failure"},
		{"undeclared task", "task", "undeclared", http.StatusBadRequest, "validation-failure"},
		{"foreign version", "version", foreign.String(), http.StatusBadRequest, "validation-failure"},
		{"changed command", "command", []string{"different"}, http.StatusConflict, "conflict"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			payload := map[string]any{"task": "dagster", "command": command, "version": ver.String(), "request_id": "new-request"}
			if tc.status == http.StatusConflict {
				payload["request_id"] = "request-1"
			}
			payload[tc.field] = tc.value
			body, err := json.Marshal(payload)
			require.NoError(t, err)
			req := httptest.NewRequest(http.MethodPost, "/api/v1/apps/dagster/runs/submit", bytes.NewReader(body))
			req = req.WithContext(rpc.ContextWithIdentity(req.Context(), &rpc.Identity{Method: rpc.AuthMethodCert}))
			resp := httptest.NewRecorder()
			mux.ServeHTTP(resp, req)
			require.Equal(t, tc.status, resp.Code, resp.Body.String())
			var out struct {
				Code string `json:"code"`
			}
			require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &out))
			assert.Equal(t, tc.code, out.Code)
		})
	}
	// Exercise the submitted identity, not a separately implemented encoding.
	// Base64url used to produce leading or adjacent separators for some keys.
	for i := range 256 {
		ret, err := client.SubmitRun(ctx, "dagster", "dagster", command, ver.String(), fmt.Sprintf("key-%d", i))
		require.NoError(t, err)
		sandbox := runapi.SandboxName(entity.Id(ret.Id()), 1)
		prefix := "sandbox." + strings.TrimPrefix(sandbox.String(), "sandbox/")
		require.NoError(t, identifiers.Validate(prefix+"_pause"))
		require.NoError(t, identifiers.Validate(prefix+"-dagster"))
	}
	terminal, err := inm.Client.Create(ctx, "never-started", &run_v1alpha.Run{App: app, Status: run_v1alpha.CANCELED})
	require.NoError(t, err)
	info, err = client.GetRun(ctx, terminal.String())
	require.NoError(t, err)
	assert.Equal(t, "none", info.Run().WorkerStatus())
}

// A canceled or timed-out run does produce an observed exit code -- the
// platform killed the process and the kernel reported something -- but that is
// teardown noise, not the task's outcome. Showing it beside a "canceled" status
// invites reading it as an application error, so it is not surfaced as the
// run's result.
func TestRunInfoOnlyReportsCommandExitCodes(t *testing.T) {
	base := func(status run_v1alpha.RunStatus) *run_v1alpha.Run {
		return &run_v1alpha.Run{
			ID:      "run/demo-x-1",
			Task:    "reindex",
			Trigger: run_v1alpha.MANUAL,
			Status:  status,
			Result:  run_v1alpha.Result{Code: 2, At: time.Now()},
		}
	}

	t.Run("succeeded reports it", func(t *testing.T) {
		info := runInfo(base(run_v1alpha.SUCCEEDED), "")
		assert.True(t, info.HasExitCode())
		assert.Equal(t, int32(2), info.ExitCode())
	})

	t.Run("failed reports it", func(t *testing.T) {
		assert.True(t, runInfo(base(run_v1alpha.FAILED), "").HasExitCode())
	})

	for _, status := range []run_v1alpha.RunStatus{
		run_v1alpha.CANCELED, run_v1alpha.TIMED_OUT, run_v1alpha.SKIPPED,
	} {
		t.Run(string(status)+" does not", func(t *testing.T) {
			assert.False(t, runInfo(base(status), "").HasExitCode(),
				"the status carries the meaning; the killed process's code is teardown noise")
		})
	}
}

// A run with no observed exit reports none, whatever its status -- a bare 0
// would read as a clean exit.
func TestRunInfoWithNoObservedExit(t *testing.T) {
	info := runInfo(&run_v1alpha.Run{
		ID:     "run/demo-x-1",
		Status: run_v1alpha.FAILED,
	}, "")
	assert.False(t, info.HasExitCode())
}

// A legitimate zero has to survive, since it is the most common successful
// outcome and the one a naive presence check drops.
func TestRunInfoReportsAZeroExitCode(t *testing.T) {
	info := runInfo(&run_v1alpha.Run{
		ID:     "run/demo-x-1",
		Status: run_v1alpha.SUCCEEDED,
		Result: run_v1alpha.Result{Code: 0, At: time.Now()},
	}, "")
	assert.True(t, info.HasExitCode())
	assert.Equal(t, int32(0), info.ExitCode())
}

// The entity fields are int64 and the wire ones int32. Wrapping a large value
// into a small plausible-looking one is worse than saturating, because nothing
// downstream can tell the result apart from a genuine one.
func TestRunInfoSaturatesRatherThanWrapping(t *testing.T) {
	info := runInfo(&run_v1alpha.Run{
		ID:      "run/demo-x-1",
		Status:  run_v1alpha.FAILED,
		Attempt: math.MaxInt32 + 1,
		Result:  run_v1alpha.Result{Code: math.MaxInt32 + 1, At: time.Now()},
	}, "")

	assert.Equal(t, int32(math.MaxInt32), info.Attempt(), "must not wrap to a negative attempt")
	assert.Equal(t, int32(math.MaxInt32), info.ExitCode(), "must not wrap to a small exit code")

	info = runInfo(&run_v1alpha.Run{
		ID:      "run/demo-x-1",
		Status:  run_v1alpha.FAILED,
		Attempt: math.MinInt32 - 1,
		Result:  run_v1alpha.Result{Code: math.MinInt32 - 1, At: time.Now()},
	}, "")
	assert.Equal(t, int32(math.MinInt32), info.ExitCode())
}

// A bare `miren app run` has to resolve to a real command. The sandbox only
// sets the container's process args when the command is non-empty, so an empty
// one leaves runc with nothing to execute and the container never starts:
// "args must not be empty" on any stack-built image, and the app's own server
// process on an image that happens to define a CMD.
func TestResolveCommandFallsBackToAShell(t *testing.T) {
	assert.Equal(t, defaultConsoleCommand, resolveCommand(nil, nil))
	assert.Equal(t, defaultConsoleCommand, resolveCommand(&core_v1alpha.ConfigSpecTasks{Name: "console"}, nil))
}

func TestResolveCommandPrefersWhatWasAsked(t *testing.T) {
	task := &core_v1alpha.ConfigSpecTasks{Name: "migrate", Command: "rails db:migrate"}

	assert.Equal(t, "rails db:migrate", resolveCommand(task, nil))
	assert.Equal(t, "'psql'", resolveCommand(task, []string{"psql"}),
		"an explicit command overrides the task's")
}

// The container's args are built as sh -c <string>, so an override has to
// survive a round of shell parsing to arrive as the argv the caller typed.
func TestShellQuotePreservesArgumentBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name string
		argv []string
		want string
	}{
		{"spaces inside an argument", []string{"echo", "hello   world"}, `'echo' 'hello   world'`},
		{"a quoted sql statement", []string{"psql", "-c", "SELECT 1"}, `'psql' '-c' 'SELECT 1'`},
		{"an embedded single quote", []string{"echo", "it's"}, `'echo' 'it'\''s'`},
		{"characters the shell would expand", []string{"echo", "$HOME", "*"}, `'echo' '$HOME' '*'`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, shellQuote(tc.argv))
		})
	}
}
