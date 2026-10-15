package runner

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/rpc"
)

func TestSandboxCountsRunnerScope(t *testing.T) {
	env, cleanup := newTestServer(t)
	t.Cleanup(cleanup)
	ctx := context.Background()
	// The full IDs collide at the old short certificate prefix, and display
	// names are identical. Neither is a valid way to choose a node scope.
	otherRunner := "abcd1234-2222-2222-2222-222222222222"
	emptyRunner := "empty"
	deadRunner := "dead-only"
	for _, runnerID := range []string{authzRunnerID, otherRunner, emptyRunner, deadRunner} {
		_, err := env.server.EAC.Create(ctx, entity.New(
			entity.DBId, compute_v1alpha.NewNodeId(runnerID).Id(),
			(&compute_v1alpha.Node{RunnerId: runnerID, Name: "same-name"}).Encode,
		).Attrs())
		require.NoError(t, err)
	}
	statuses := []compute_v1alpha.SandboxStatus{
		compute_v1alpha.RUNNING, compute_v1alpha.RUNNING,
		compute_v1alpha.PENDING, compute_v1alpha.PENDING, compute_v1alpha.PENDING,
		compute_v1alpha.NOT_READY, compute_v1alpha.STOPPED, compute_v1alpha.DEAD,
		"",
	}
	for i, status := range statuses {
		_, err := env.server.EAC.Create(ctx, entity.New(
			entity.DBId, entity.Id(fmt.Sprintf("sandbox/own-%d", i)),
			(&compute_v1alpha.Sandbox{Status: status}).Encode,
			(&compute_v1alpha.Schedule{Key: compute_v1alpha.Key{
				Kind: compute_v1alpha.KindSandbox, Node: compute_v1alpha.NewNodeId(authzRunnerID).Id(),
			}}).Encode,
		).Attrs())
		require.NoError(t, err)
	}
	// Unknown enum references decode to an unset status, just as they do for
	// runner_status.go. Write the raw reference so this exercises that path
	// rather than the generated encoder silently omitting an unknown value.
	_, err := env.server.EAC.Create(ctx, entity.New(
		entity.DBId, entity.Id("sandbox/future-status"),
		(&compute_v1alpha.Sandbox{}).Encode,
		entity.Ref(compute_v1alpha.SandboxStatusId, entity.Id("dev.miren.compute/status.future")),
		(&compute_v1alpha.Schedule{Key: compute_v1alpha.Key{
			Kind: compute_v1alpha.KindSandbox, Node: compute_v1alpha.NewNodeId(authzRunnerID).Id(),
		}}).Encode,
	).Attrs())
	require.NoError(t, err)
	for _, fixture := range []struct {
		name   string
		runner string
		status compute_v1alpha.SandboxStatus
	}{
		{"other", otherRunner, compute_v1alpha.RUNNING},
		{"dead", deadRunner, compute_v1alpha.DEAD},
		{"stopped", deadRunner, compute_v1alpha.STOPPED},
		{"unscheduled", "", compute_v1alpha.RUNNING},
	} {
		schedule := &compute_v1alpha.Schedule{}
		if fixture.runner != "" {
			schedule.Key = compute_v1alpha.Key{Kind: compute_v1alpha.KindSandbox, Node: compute_v1alpha.NewNodeId(fixture.runner).Id()}
		}
		_, err := env.server.EAC.Create(ctx, entity.New(
			entity.DBId, entity.Id("sandbox/"+fixture.name),
			(&compute_v1alpha.Sandbox{Status: fixture.status}).Encode, schedule.Encode,
		).Attrs())
		require.NoError(t, err)
	}

	result, err := env.client.SandboxCounts(ctxWithCert(runnerCertName(authzRunnerID)))
	require.NoError(t, err)
	require.EqualValues(t, 8, result.Live())
	require.EqualValues(t, 2, result.Running())
	var names []string
	var counts []int64
	for _, count := range result.Others() {
		names = append(names, count.Status())
		counts = append(counts, count.Count())
	}
	require.Equal(t, []string{"not_ready", "pending", "unknown"}, names)
	require.Equal(t, []int64{1, 3, 2}, counts)

	for _, runnerID := range []string{otherRunner, emptyRunner, deadRunner} {
		t.Run(runnerID, func(t *testing.T) {
			result, err := env.client.SandboxCounts(ctxWithCert(runnerCertName(runnerID)))
			require.NoError(t, err)
			var expected int64
			if runnerID == otherRunner {
				expected = 1
			}
			require.Equal(t, expected, result.Live())
			require.Equal(t, expected, result.Running())
			require.Empty(t, result.Others())
		})
	}
}

func TestSandboxCountsRejectsUnscopedIdentities(t *testing.T) {
	env, cleanup := newTestServer(t)
	t.Cleanup(cleanup)
	_, err := env.server.EAC.Create(context.Background(), entity.New(
		entity.DBId, compute_v1alpha.NewNodeId(authzRunnerID).Id(),
		(&compute_v1alpha.Node{RunnerId: "different-owner"}).Encode,
	).Attrs())
	require.NoError(t, err)

	for _, tc := range []struct {
		name     string
		identity *rpc.Identity
	}{
		{"missing", nil},
		{"anonymous", &rpc.Identity{Method: rpc.AuthMethodAnonymous}},
		{"jwt-admin", &rpc.Identity{Method: rpc.AuthMethodJWT, Subject: runnerCertName(authzRunnerID)}},
		{"workload-admin", &rpc.Identity{Method: rpc.AuthMethodWorkload, Subject: runnerCertName(authzRunnerID), Metadata: map[string]any{"role": "admin"}}},
		{"non-runner-cert", &rpc.Identity{Method: rpc.AuthMethodCert, Subject: "coordinator"}},
		{"empty-runner-cert", &rpc.Identity{Method: rpc.AuthMethodCert, Subject: "runner-"}},
		{"unregistered-runner", &rpc.Identity{Method: rpc.AuthMethodCert, Subject: "runner-missing"}},
		{"short-prefix", &rpc.Identity{Method: rpc.AuthMethodCert, Subject: "runner-abcd1234"}},
		{"wrong-owner", &rpc.Identity{Method: rpc.AuthMethodCert, Subject: runnerCertName(authzRunnerID)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			if tc.identity != nil {
				ctx = rpc.ContextWithIdentity(ctx, tc.identity)
			}
			result, err := env.client.SandboxCounts(ctx)
			require.Error(t, err)
			require.Nil(t, result)
		})
	}
}
