package addon

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/testutils"
	"miren.dev/runtime/pkg/entity/types"
)

// An addon's metrics have to be summable with the metrics of the app that owns
// it, and deployed services publish the app as "miren.app". Without this the two
// spell the same fact differently and no query can add them together.
func TestMetricLabelsNormalizesTheAppKey(t *testing.T) {
	in := types.LabelSet("addon", "postgresql", "app", "myapp", "server", "db1")

	out := metricLabels(in)

	app, ok := out.Get("miren.app")
	require.True(t, ok)
	assert.Equal(t, "myapp", app)

	// The original key stays: it is also the pool's entity label, which the
	// usage directory and other readers already depend on.
	orig, ok := out.Get("app")
	require.True(t, ok)
	assert.Equal(t, "myapp", orig)
}

func TestMetricLabelsLeavesTheInputAlone(t *testing.T) {
	in := types.LabelSet("addon", "valkey", "app", "myapp")

	_ = metricLabels(in)

	// The same slice is stored as the pool's SandboxLabels, so appending
	// through it would leak a metric-only key onto the entity.
	_, ok := in.Get("miren.app")
	assert.False(t, ok, "the caller's labels must not be modified in place")
}

func TestMetricLabelsSkipsSharedAddons(t *testing.T) {
	// A shared server belongs to no single app and carries no app label, so
	// there is nothing to attribute it to.
	in := types.LabelSet("addon", "mysql", "shared", "true")

	out := metricLabels(in)

	_, ok := out.Get("miren.app")
	assert.False(t, ok, "an empty app label would group every shared server under one phantom app")
}

func TestRunOneShotSandboxCreatesAtMostOnceWorkloadAndWaitsForExit(t *testing.T) {
	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()
	ctx := t.Context()
	fw := NewProviderFramework(testutils.TestLogger(t), server.Client, server.EAC, nil)

	patched := make(chan error, 1)
	go func() {
		id := entity.Id("sandbox/backup")
		for {
			if err := server.Client.GetById(ctx, id, &compute_v1alpha.Sandbox{}); err == nil {
				patched <- server.Client.Patch(ctx, id, 0, (&compute_v1alpha.Sandbox{
					Status: compute_v1alpha.STOPPED,
					Exit:   compute_v1alpha.Exit{At: time.Now(), Code: 0},
				}).Encode()...)
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Millisecond):
			}
		}
	}()

	id, err := fw.RunOneShotSandbox(ctx, OneShotSandboxSpec{
		Name: "backup", Image: "postgres:18", Command: "pg_basebackup",
	}, time.Second)
	require.NoError(t, err)
	require.NoError(t, <-patched)
	assert.Equal(t, entity.Id("sandbox/backup"), id)

	var sandbox compute_v1alpha.Sandbox
	require.NoError(t, server.Client.GetById(ctx, id, &sandbox))
	assert.Equal(t, compute_v1alpha.SandboxSpecNEVER, sandbox.Spec.RestartPolicy)
	require.Len(t, sandbox.Spec.Container, 1)
	assert.Equal(t, "pg_basebackup", sandbox.Spec.Container[0].Command)
}

func TestRunOneShotSandboxReportsExistingFailureWithoutRerunning(t *testing.T) {
	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()
	ctx := context.Background()
	fw := NewProviderFramework(testutils.TestLogger(t), server.Client, server.EAC, nil)

	id, err := server.Client.Create(ctx, "failed-backup", &compute_v1alpha.Sandbox{
		Status: compute_v1alpha.STOPPED,
		Exit:   compute_v1alpha.Exit{At: time.Now(), Code: 7},
	})
	require.NoError(t, err)

	got, err := fw.RunOneShotSandbox(ctx, OneShotSandboxSpec{Name: "failed-backup"}, time.Second)
	require.Error(t, err)
	assert.Equal(t, id, got)
	assert.Contains(t, err.Error(), "exited with code 7")
}
