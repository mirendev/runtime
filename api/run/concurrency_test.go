package run

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/appconfig"
)

// The console default is deliberately not the conservative one: `miren app run`
// had no limit before tasks absorbed it, and most apps never declare the task,
// so falling back to 1 would make the second person to open a console wait
// behind the first.
func TestMaxConcurrentDefaults(t *testing.T) {
	assert.Equal(t, int64(appconfig.ConsoleMaxConcurrent), MaxConcurrent(nil, "console"))
	assert.Equal(t, int64(appconfig.DefaultTaskMaxConcurrent), MaxConcurrent(nil, "migrate"))
}

func TestMaxConcurrentPrefersTheDeclaredValue(t *testing.T) {
	assert.Equal(t, int64(3),
		MaxConcurrent(&core_v1alpha.ConfigSpecTasks{Name: "console", MaxConcurrent: 3}, "console"))
	assert.Equal(t, int64(5),
		MaxConcurrent(&core_v1alpha.ConfigSpecTasks{Name: "migrate", MaxConcurrent: 5}, "migrate"))

	// A declared task that says nothing about concurrency still gets the
	// default for its name, so declaring [tasks.console] to set a command does
	// not silently drop the console's higher ceiling.
	assert.Equal(t, int64(appconfig.ConsoleMaxConcurrent),
		MaxConcurrent(&core_v1alpha.ConfigSpecTasks{Name: "console"}, "console"))
}
