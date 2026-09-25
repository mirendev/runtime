package app

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/pkg/apphealth"
	"miren.dev/runtime/pkg/entity/testutils"
	"miren.dev/runtime/pkg/entity/types"
)

func TestPoolHealthClassify(t *testing.T) {
	cases := []struct {
		name string
		h    poolHealth
		want string
	}{
		{"in cooldown is crashed regardless of counts", poolHealth{ready: 1, desired: 1, inCooldown: true}, apphealth.Crashed},
		{"autoscale at zero is idle", poolHealth{ready: 0, desired: 0, isAutoscale: true}, apphealth.Idle},
		{"fixed at zero is starting, not idle", poolHealth{ready: 0, desired: 0, isAutoscale: false}, apphealth.Starting},
		// An app that needs no service has no pools by design; reporting it as
		// idle would say it went to sleep rather than that it is ready.
		{"service-free app at zero is ready, not idle", poolHealth{ready: 0, desired: 0, isAutoscale: true, needsNoService: true}, apphealth.Ready},
		{"service-free app wins over the autoscale reading", poolHealth{ready: 0, desired: 0, isAutoscale: false, needsNoService: true}, apphealth.Ready},
		{"all ready is healthy", poolHealth{ready: 3, desired: 3}, apphealth.Healthy},
		{"some ready is degraded", poolHealth{ready: 1, desired: 3}, apphealth.Degraded},
		{"none ready is starting", poolHealth{ready: 0, desired: 2}, apphealth.Starting},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.h.classify())
		})
	}
}

func TestPoolHealthAccumulate(t *testing.T) {
	now := time.Now()

	var h poolHealth

	p1 := &compute_v1alpha.SandboxPool{ReadyInstances: 1, DesiredInstances: 2}
	h.accumulate(p1, now)

	// A second pool in cooldown contributes its crash count and the longest
	// remaining cooldown.
	p2 := &compute_v1alpha.SandboxPool{
		ReadyInstances:        0,
		DesiredInstances:      1,
		CooldownUntil:         now.Add(30 * time.Second),
		ConsecutiveCrashCount: 4,
	}
	h.accumulate(p2, now)

	assert.Equal(t, 1, h.ready)
	assert.Equal(t, 3, h.desired)
	assert.True(t, h.inCooldown)
	assert.Equal(t, int64(4), h.crashCount)
	assert.Greater(t, h.cooldownLeft, 29*time.Second)
	assert.Equal(t, apphealth.Crashed, h.classify())
}

func TestPoolHealthAccumulate_ExpiredCooldownIgnored(t *testing.T) {
	now := time.Now()

	var h poolHealth
	p := &compute_v1alpha.SandboxPool{
		ReadyInstances:        2,
		DesiredInstances:      2,
		CooldownUntil:         now.Add(-time.Minute), // already passed
		ConsecutiveCrashCount: 9,
	}
	h.accumulate(p, now)

	assert.False(t, h.inCooldown)
	assert.Equal(t, apphealth.Healthy, h.classify())
}

func TestCollectServiceHealthUsesSharedCooldownAndLatestExit(t *testing.T) {
	ctx := context.Background()
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)
	ec := entityserver.NewClient(slog.Default(), inmem.EAC)
	r := &AppInfo{Log: slog.Default(), EC: ec}
	now := time.Now().Truncate(time.Second)
	pools := []compute_v1alpha.SandboxPool{
		{ID: "pool-db", Service: "db", DesiredInstances: 1, ConsecutiveCrashCount: 7, CooldownUntil: now.Add(3 * time.Minute), LastCrashTime: now.Add(-12 * time.Minute)},
		{ID: "pool-web", Service: "web", DesiredInstances: 1, ReadyInstances: 1},
	}
	failedID, err := ec.Create(ctx, "failed", &compute_v1alpha.Sandbox{Status: compute_v1alpha.DEAD, Exit: compute_v1alpha.Exit{Code: 17, At: now.Add(-12 * time.Minute)}}, entityserver.WithLabels(types.LabelSet("pool", "pool-db")))
	require.NoError(t, err)
	_, err = ec.Create(ctx, "success", &compute_v1alpha.Sandbox{Status: compute_v1alpha.DEAD, Exit: compute_v1alpha.Exit{Code: 0, At: now.Add(-11 * time.Minute)}}, entityserver.WithLabels(types.LabelSet("pool", "pool-db")))
	require.NoError(t, err)
	_, err = ec.Create(ctx, "retired", &compute_v1alpha.Sandbox{Status: compute_v1alpha.DEAD}, entityserver.WithLabels(types.LabelSet("pool", "pool-db")))
	require.NoError(t, err)
	_, err = ec.Create(ctx, "web", &compute_v1alpha.Sandbox{Status: compute_v1alpha.RUNNING}, entityserver.WithLabels(types.LabelSet("pool", "pool-web")))
	require.NoError(t, err)
	_, err = ec.Create(ctx, "foreign", &compute_v1alpha.Sandbox{Status: compute_v1alpha.DEAD, Exit: compute_v1alpha.Exit{Code: 99, At: now}}, entityserver.WithLabels(types.LabelSet("pool", "other")))
	require.NoError(t, err)

	got, err := r.collectServiceHealth(ctx, pools, nil, now)
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, "db", got[0].Service())
	assert.Equal(t, apphealth.Crashed, got[0].Health(), "cooldown still active past the 10-minute failure window")
	assert.EqualValues(t, 7, got[0].CrashCount())
	assert.EqualValues(t, 3, got[0].Dead())
	require.True(t, got[0].HasLastExitCode())
	assert.Zero(t, got[0].LastExitCode(), "latest successful exit replaces the older code")
	assert.Equal(t, failedID.String(), got[0].LastFailureSandbox())
	assert.Equal(t, apphealth.Healthy, got[1].Health())
	assert.EqualValues(t, 1, got[1].Running())
	fixed, err := r.collectServiceHealth(ctx,
		[]compute_v1alpha.SandboxPool{{ID: "pool-fixed", Service: "fixed"}},
		&core_v1alpha.ConfigSpec{Services: []core_v1alpha.ConfigSpecServices{{Name: "fixed", Concurrency: core_v1alpha.ConfigSpecServicesConcurrency{Mode: "fixed"}}}}, now)
	require.NoError(t, err)
	require.Len(t, fixed, 1)
	assert.Equal(t, apphealth.Starting, fixed[0].Health(), "fixed service at zero must not be classified as idle")
}

func TestSpecNeedsNoService(t *testing.T) {
	assert.False(t, specNeedsNoService(nil))
	assert.False(t, specNeedsNoService(&core_v1alpha.ConfigSpec{}), "an empty app has no valid workload")

	assert.False(t, specNeedsNoService(&core_v1alpha.ConfigSpec{
		Services: []core_v1alpha.ConfigSpecServices{{Name: "web"}},
		Tasks:    []core_v1alpha.ConfigSpecTasks{{Name: "migrate"}},
	}), "an app with a service still has something long-running")

	assert.True(t, specNeedsNoService(&core_v1alpha.ConfigSpec{
		Tasks: []core_v1alpha.ConfigSpecTasks{{Name: "session"}},
	}))
	assert.True(t, specNeedsNoService(&core_v1alpha.ConfigSpec{StaticDir: "/site"}))
}
