package sagagc

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"miren.dev/runtime/pkg/entity/testutils"
	"miren.dev/runtime/pkg/saga"
)

func newTestController(t *testing.T, cfg GCConfig) (*GCController, saga.Storage) {
	t.Helper()

	storage := saga.NewMemoryStorage()
	return &GCController{
		Log:     testutils.TestLogger(t),
		Storage: storage,
		Config:  cfg,
	}, storage
}

// save persists an execution whose last state change was `age` ago.
func save(t *testing.T, storage saga.Storage, id string, status saga.Status, age time.Duration) {
	t.Helper()

	changed := time.Now().Add(-age)
	require.NoError(t, storage.Save(context.Background(), &saga.Execution{
		ID:              id,
		DefinitionName:  "create-sandbox",
		Status:          status,
		InitialInputs:   map[string]any{},
		ExecutedActions: map[string]*saga.ActionResult{},
		ExecutionOrder:  []string{},
		CreatedAt:       changed,
		UpdatedAt:       changed,
	}))
}

func statusOf(t *testing.T, storage saga.Storage, id string) saga.Status {
	t.Helper()

	exec, err := storage.Get(context.Background(), id)
	require.NoError(t, err)
	return exec.Status
}

func exists(t *testing.T, storage saga.Storage, id string) bool {
	t.Helper()

	_, err := storage.Get(context.Background(), id)
	return err == nil
}

// TestSweep_RunsBothPolicies pins that a tick drives both sweeps. They deal
// with disjoint populations that neither can reach on its own: retention only
// looks at finished executions, and the stalled sweep only at in-flight ones.
// A tick that ran one of them would leave the other's backlog growing with
// nothing to say so.
func TestSweep_RunsBothPolicies(t *testing.T) {
	c, storage := newTestController(t, DefaultGCConfig())

	save(t, storage, "expired", saga.StatusCompleted, 30*24*time.Hour)
	save(t, storage, "stranded", saga.StatusPending, 30*24*time.Hour)
	save(t, storage, "working", saga.StatusRunning, time.Hour)

	c.sweep(context.Background())

	assert.False(t, exists(t, storage, "expired"), "retention must have collected the finished execution")
	assert.Equal(t, saga.StatusFailed, statusOf(t, storage, "stranded"),
		"the stalled sweep must have forced the stranded one")
	assert.Equal(t, saga.StatusRunning, statusOf(t, storage, "working"),
		"neither sweep touches an execution that is genuinely in progress")
}

// TestSweep_EachWindowDisablesOnlyItsOwnPolicy pins that the two windows are
// actually separate switches. An operator reaches both through one config
// field, so in production they go dark together, but the controller must not
// have quietly coupled them: the coupling is a decision the coordinator makes
// and can revisit, not something baked in here.
func TestSweep_EachWindowDisablesOnlyItsOwnPolicy(t *testing.T) {
	t.Run("retention off, stalled sweep on", func(t *testing.T) {
		cfg := DefaultGCConfig()
		cfg.Retention = 0
		c, storage := newTestController(t, cfg)

		save(t, storage, "expired", saga.StatusCompleted, 30*24*time.Hour)
		save(t, storage, "stranded", saga.StatusPending, 30*24*time.Hour)

		c.sweep(context.Background())

		assert.True(t, exists(t, storage, "expired"), "frozen history must stay frozen")
		assert.Equal(t, saga.StatusFailed, statusOf(t, storage, "stranded"))
	})

	t.Run("stalled sweep off, retention on", func(t *testing.T) {
		cfg := DefaultGCConfig()
		cfg.StaleAfter = 0
		c, storage := newTestController(t, cfg)

		save(t, storage, "expired", saga.StatusCompleted, 30*24*time.Hour)
		save(t, storage, "stranded", saga.StatusPending, 30*24*time.Hour)

		c.sweep(context.Background())

		assert.False(t, exists(t, storage, "expired"))
		assert.Equal(t, saga.StatusPending, statusOf(t, storage, "stranded"),
			"in-flight executions must be left exactly as they are")
	})
}

// TestStart_RunsWhileEitherPolicyIsOn is the regression for the gate. It used to
// read the retention window alone, which was the whole configuration at the
// time. Left that way, a caller that had only the stalled sweep turned on would
// get no controller at all and never be told.
func TestStart_RunsWhileEitherPolicyIsOn(t *testing.T) {
	for _, tc := range []struct {
		name       string
		retention  time.Duration
		staleAfter time.Duration
		wantRun    bool
	}{
		{"both on", 7 * 24 * time.Hour, 7 * 24 * time.Hour, true},
		{"retention off", 0, 7 * 24 * time.Hour, true},
		{"stalled sweep off", 7 * 24 * time.Hour, 0, true},
		{"both off", 0, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := DefaultGCConfig()
			cfg.Retention = tc.retention
			cfg.StaleAfter = tc.staleAfter

			c, _ := newTestController(t, cfg)
			c.Start(context.Background())
			t.Cleanup(c.Stop)

			assert.Equal(t, tc.wantRun, c.cancel != nil,
				"the controller runs while either policy has work to do")
		})
	}
}
