package run

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"miren.dev/runtime/pkg/entity"
)

// Both the run controller and the app server derive this name independently of
// each other's state, so it has to be a pure function of the run and attempt.
// If they ever disagree, a client attaches to a sandbox that will never exist
// and sees only a generic failure.
func TestSandboxName(t *testing.T) {
	id := entity.Id("run/demo-migrate-abc")

	assert.Equal(t, entity.Id("sandbox/run-demo-migrate-abc-a1"), SandboxName(id, 1))
	assert.Equal(t, entity.Id("sandbox/run-demo-migrate-abc-a2"), SandboxName(id, 2))

	// A retry must get its own sandbox, or create-if-absent refuses it and the
	// attempt silently reuses the failed one.
	assert.NotEqual(t, SandboxName(id, 1), SandboxName(id, 2))

	// Callers that haven't stamped an attempt yet get the first.
	assert.Equal(t, SandboxName(id, 1), SandboxName(id, 0))
	assert.Equal(t, SandboxName(id, 1), SandboxName(id, -3))

	// The kind prefix is stripped, not embedded, so the name stays one segment.
	assert.NotContains(t, SandboxName(id, 1).String(), "run/demo")
}
