package sandboxpool

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	compute "miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/testutils"
	"miren.dev/runtime/pkg/entity/types"
)

func checkpointSpec() compute.SandboxSpec {
	return compute.SandboxSpec{Version: "version/one",
		LogAttribute: types.LabelSet("miren.stage", "app-run"),
		Container:    []compute.SandboxSpecContainer{{Name: "web", Image: "app:v1"}}}
}

func TestCheckpointRequiresAutoIdlePolicy(t *testing.T) {
	ctx := context.Background()
	es, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()
	m := NewManager(testutils.TestLogger(t), es.EAC)
	for i, tc := range []struct {
		mode, delay, ephemeral string
		idle                   time.Duration
		want                   bool
	}{
		{"auto", "2m", "", 3 * time.Minute, true},
		{"", "", "", 3 * time.Minute, true},
		{"auto", "2m", "", time.Minute, false},
		{"fixed", "2m", "", 3 * time.Minute, false},
		{"auto", "0s", "", 3 * time.Minute, false},
		{"auto", "2m", "preview", 3 * time.Minute, false},
	} {
		ver := &core_v1alpha.AppVersion{EphemeralLabel: tc.ephemeral, Config: core_v1alpha.Config{
			Services: []core_v1alpha.Services{{Name: "web", ServiceConcurrency: core_v1alpha.ServiceConcurrency{
				Mode: tc.mode, ScaleDownDelay: tc.delay,
			}}},
		}}
		id, err := es.Client.Create(ctx, fmt.Sprintf("policy-version-%d", i), ver)
		require.NoError(t, err)
		pool := &compute.SandboxPool{Service: "web", SandboxSpec: compute.SandboxSpec{Version: id}}
		require.Equal(t, tc.want, m.autoIdle(ctx, pool, time.Now().Add(-tc.idle)), "%+v", tc)
	}
}

func TestCheckpointCandidateOnlyLastInstanceAtZero(t *testing.T) {
	now := time.Now()
	sb := &compute.Sandbox{ID: "sandbox/original", Status: compute.RUNNING, Spec: checkpointSpec(), LastActivity: now.Add(-5 * time.Minute)}
	pool := &compute.SandboxPool{SandboxSpec: checkpointSpec(), ReferencedByVersions: []entity.Id{"version/one"}}
	all := []*sandboxWithMeta{{sandbox: sb}}
	require.True(t, checkpointCandidate(pool, all, sb, now))
	pool.DesiredInstances = 1
	require.False(t, checkpointCandidate(pool, all, sb, now), "scale-in to one is not idle-to-zero")
	pool.DesiredInstances = 0
	for _, status := range []compute.SandboxStatus{compute.RUNNING, compute.PENDING, compute.RESTORING, compute.HIBERNATED} {
		other := &sandboxWithMeta{sandbox: &compute.Sandbox{ID: "sandbox/extra", Status: status}}
		require.False(t, checkpointCandidate(pool, append(all, other), sb, now), "must not checkpoint with sibling %s", status)
	}
	pool.SandboxSpec.Version = "version/two"
	require.False(t, checkpointCandidate(pool, all, sb, now), "pool reuse across versions invalidates memory")
	pool.SandboxSpec = checkpointSpec()
	pool.ReferencedByVersions = nil
	require.False(t, checkpointCandidate(pool, all, sb, now), "decommission is not auto-idle")
	pool.ReferencedByVersions = []entity.Id{"version/other"}
	require.False(t, checkpointCandidate(pool, all, sb, now), "another version's reference does not authorize replay")
}

func TestCheckpointPoolWakeAndInvalidation(t *testing.T) {
	for _, tc := range []struct {
		name       string
		status     compute.SandboxStatus
		desired    int64
		invalidate bool
		want       compute.SandboxStatus
		count      int
		expired    bool
		offline    bool
	}{
		{"stay asleep", compute.HIBERNATED, 0, false, compute.HIBERNATED, 1, false, false},
		{"request during checkpoint", compute.HIBERNATING, 1, false, compute.HIBERNATING, 1, false, false},
		{"wake same sandbox", compute.HIBERNATED, 1, false, compute.RESTORING, 1, false, false},
		{"scale out only cold replicas", compute.HIBERNATED, 3, false, compute.RESTORING, 3, false, false},
		{"new spec", compute.HIBERNATED, 1, true, compute.STOPPED, 2, false, false},
		{"expired", compute.HIBERNATED, 1, false, compute.STOPPED, 2, true, false},
		{"runner unavailable", compute.HIBERNATED, 1, false, compute.STOPPED, 2, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			es, cleanup := testutils.NewInMemEntityServer(t)
			defer cleanup()
			node := &compute.Node{Status: compute.READY, ApiAddress: "runner:9000"}
			if tc.offline {
				node.Status = compute.UNHEALTHY
			}
			nodeID := entity.Id("node/checkpoint-runner")
			es.AddEntity(entity.New(entity.DBId, nodeID, node.Encode))
			pool := &compute.SandboxPool{Service: "web", DesiredInstances: tc.desired,
				SandboxSpec: checkpointSpec(), ReferencedByVersions: []entity.Id{"version/one"}}
			id, err := es.Client.Create(ctx, "checkpoint-pool", pool)
			require.NoError(t, err)
			pool.ID = id
			sb := &compute.Sandbox{Status: tc.status, Spec: checkpointSpec(), HibernatedAt: time.Now()}
			if tc.expired {
				sb.HibernatedAt = time.Now().Add(-2 * time.Hour)
			}
			resp, err := es.EAC.Create(ctx, entity.New(sb.Encode, (&core_v1alpha.Metadata{
				Labels: types.LabelSet("pool", id.String(), "service", "web", "instance", "7"),
			}).Encode, (&compute.Schedule{Key: compute.Key{Kind: compute.KindSandbox, Node: nodeID}}).Encode).Attrs())
			require.NoError(t, err)
			sb.ID = entity.Id(resp.Id())
			if tc.invalidate {
				pool.SandboxSpec.Container[0].Image = "app:v2"
			}
			m := NewManager(testutils.TestLogger(t), es.EAC)
			// Repeat reconciliation to prove the claim is single-use and that
			// desired=3 creates two fresh replicas, not three restored copies.
			for range 3 {
				require.NoError(t, m.Reconcile(ctx, pool, nil))
			}
			all, err := m.listSandboxes(ctx, pool)
			require.NoError(t, err)
			require.Len(t, all, tc.count)
			for _, item := range all {
				if item.sandbox.ID == sb.ID {
					require.Equal(t, tc.want, item.sandbox.Status)
					if tc.want == compute.RESTORING {
						require.False(t, item.sandbox.RestoredAt.IsZero())
					}
				} else {
					require.Equal(t, compute.PENDING, item.sandbox.Status)
				}
			}
			require.Zero(t, pool.ReadyInstances, "restoring is never ready")
		})
	}
}
