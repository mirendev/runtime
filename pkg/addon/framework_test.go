package addon

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
