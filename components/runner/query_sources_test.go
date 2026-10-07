package runner

import (
	"testing"

	events "github.com/containerd/containerd/api/events"
	"github.com/stretchr/testify/require"
	query "miren.dev/runtime/pkg/portalquery"
)

func TestSandboxTaskEvent(t *testing.T) {
	for _, tc := range []struct {
		name   string
		value  any
		id     string
		fields map[string]any
	}{
		{"start", &events.TaskStart{ContainerID: "web", Pid: 37}, "web", map[string]any{"action": "start", "pid": uint32(37)}},
		{"exit", &events.TaskExit{ContainerID: "web", ID: "web", Pid: 37, ExitStatus: 19}, "web", map[string]any{"action": "exit", "pid": uint32(37), "exit_status": uint32(19)}},
		{"exec exit", &events.TaskExit{ContainerID: "web", ID: "exec-1", Pid: 42}, "", nil},
		{"oom", &events.TaskOOM{ContainerID: "worker"}, "worker", map[string]any{"action": "oom"}},
		{"unrelated", &events.ContainerCreate{ID: "web"}, "", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id, fields := sandboxTaskEvent(tc.value)
			require.Equal(t, tc.id, id)
			require.Equal(t, tc.fields, fields)
		})
	}
}

func TestSandboxSourceContracts(t *testing.T) {
	engine := (&SandboxHost{}).queryEngine()
	for _, text := range []string{
		"sandboxes where sandbox_id = sandbox/web and state = running and pid = 37",
		"sandbox_events where action = exit and exit_status > 3 count over 1s by sandbox_id",
		"sandbox_events { @failures[sandbox_id] = sum(exit_status) } after 1s { emit @failures }",
	} {
		req, err := engine.ParseMonitorQuery(text)
		require.NoError(t, err, text)
		require.NoError(t, engine.Validate(req), text)
	}
	for _, text := range []string{"sandboxes count over 1s", "sandboxes where pid > 3", "sandboxes where env = secret"} {
		_, err := engine.ParseMonitorQuery(text)
		require.Error(t, err, text)
	}
	_, err := engine.Query(t.Context(), query.MonitorRequest{Source: "sandbox_events"})
	require.ErrorContains(t, err, "custom snapshot source unavailable")
}
