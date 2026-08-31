package saga

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test input/output types
type AddNumbersIn struct {
	A int `saga:"a"`
	B int `saga:"b"`
}

type AddNumbersOut struct {
	Sum int `saga:"sum"`
}

type MultiplyIn struct {
	Sum    int `saga:"sum"`
	Factor int `saga:"factor"`
}

type MultiplyOut struct {
	Result int `saga:"result"`
}

// Test controller to track calls
type testController struct {
	mu            sync.Mutex
	addCalls      []AddNumbersIn
	multiplyCalls []MultiplyIn
	undoAddCalls  []AddNumbersOut
	undoMultCalls []MultiplyOut
	failAdd       bool
	failMultiply  bool
	failUndoAdd   bool
}

func (c *testController) recordAdd(in AddNumbersIn) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.addCalls = append(c.addCalls, in)
}

func (c *testController) recordMultiply(in MultiplyIn) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.multiplyCalls = append(c.multiplyCalls, in)
}

func (c *testController) recordUndoAdd(out AddNumbersOut) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.undoAddCalls = append(c.undoAddCalls, out)
}

func (c *testController) recordUndoMult(out MultiplyOut) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.undoMultCalls = append(c.undoMultCalls, out)
}

// Action functions
func AddNumbers(ctx context.Context, in AddNumbersIn) (AddNumbersOut, error) {
	ctrl := Get[*testController](ctx)
	ctrl.recordAdd(in)
	if ctrl.failAdd {
		return AddNumbersOut{}, errors.New("add failed")
	}
	return AddNumbersOut{Sum: in.A + in.B}, nil
}

func UndoAddNumbers(ctx context.Context, in AddNumbersIn, out AddNumbersOut) error {
	ctrl := Get[*testController](ctx)
	ctrl.recordUndoAdd(out)
	if ctrl.failUndoAdd {
		return errors.New("undo add failed")
	}
	return nil
}

func Multiply(ctx context.Context, in MultiplyIn) (MultiplyOut, error) {
	ctrl := Get[*testController](ctx)
	ctrl.recordMultiply(in)
	if ctrl.failMultiply {
		return MultiplyOut{}, errors.New("multiply failed")
	}
	return MultiplyOut{Result: in.Sum * in.Factor}, nil
}

func UndoMultiply(ctx context.Context, in MultiplyIn, out MultiplyOut) error {
	ctrl := Get[*testController](ctx)
	ctrl.recordUndoMult(out)
	return nil
}

func TestBuilder_SingleAction(t *testing.T) {
	registry := NewRegistry()

	ctrl := &testController{}
	err := Define("single-action").
		Using(ctrl).
		Action("add", AddNumbers).Undo(UndoAddNumbers).
		RegisterTo(registry)
	require.NoError(t, err)

	def, ok := registry.Get("single-action")
	require.True(t, ok)
	assert.Equal(t, "single-action", def.Name)
	assert.Len(t, def.Actions, 1)
	assert.Contains(t, def.Actions, "add")
}

func TestBuilder_MultipleActionsWithDependencies(t *testing.T) {
	registry := NewRegistry()

	ctrl := &testController{}
	err := Define("calc").
		Using(ctrl).
		Action("add", AddNumbers).Undo(UndoAddNumbers).
		Action("multiply", Multiply).Undo(UndoMultiply).
		RegisterTo(registry)
	require.NoError(t, err)

	def, ok := registry.Get("calc")
	require.True(t, ok)
	assert.Len(t, def.Actions, 2)

	// Multiply depends on add (because it needs "sum")
	multNode := def.Actions["multiply"]
	assert.Contains(t, multNode.Dependencies, "add")

	// Execution order should have add before multiply
	addIdx := -1
	multIdx := -1
	for i, name := range def.executionOrder {
		if name == "add" {
			addIdx = i
		}
		if name == "multiply" {
			multIdx = i
		}
	}
	assert.True(t, addIdx < multIdx, "add should come before multiply")
}

func TestBuilder_DuplicateOutputsError(t *testing.T) {
	ctrl := &testController{}
	// Both actions produce "sum"
	_, err := Define("duplicate").
		Using(ctrl).
		Action("add1", AddNumbers).Undo(UndoAddNumbers).
		Action("add2", AddNumbers).Undo(UndoAddNumbers).
		Build()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "sum")
}

func TestExecutor_Success(t *testing.T) {
	registry := NewRegistry()

	ctrl := &testController{}
	err := Define("calc").
		Using(ctrl).
		Action("add", AddNumbers).Undo(UndoAddNumbers).
		Action("multiply", Multiply).Undo(UndoMultiply).
		RegisterTo(registry)
	require.NoError(t, err)

	storage := NewMemoryStorage()
	executor := NewExecutor(storage, WithRegistry(registry))

	ctx := context.Background()
	err = executor.Start("calc").
		Input("a", 2).
		Input("b", 3).
		Input("factor", 4).
		WithID("test-exec-1").
		Execute(ctx)
	require.NoError(t, err)

	// Verify actions were called
	assert.Len(t, ctrl.addCalls, 1)
	assert.Equal(t, AddNumbersIn{A: 2, B: 3}, ctrl.addCalls[0])

	assert.Len(t, ctrl.multiplyCalls, 1)
	assert.Equal(t, MultiplyIn{Sum: 5, Factor: 4}, ctrl.multiplyCalls[0])

	// Verify no undos
	assert.Empty(t, ctrl.undoAddCalls)
	assert.Empty(t, ctrl.undoMultCalls)

	// Verify final state
	exec, err := storage.Get(ctx, "test-exec-1")
	require.NoError(t, err)
	assert.Equal(t, StatusCompleted, exec.Status)
	assert.Len(t, exec.ExecutionOrder, 2)
}

func TestExecutor_FailureAndUndo(t *testing.T) {
	registry := NewRegistry()

	ctrl := &testController{failMultiply: true}
	err := Define("calc-fail").
		Using(ctrl).
		Action("add", AddNumbers).Undo(UndoAddNumbers).
		Action("multiply", Multiply).Undo(UndoMultiply).
		RegisterTo(registry)
	require.NoError(t, err)

	storage := NewMemoryStorage()
	executor := NewExecutor(storage, WithRegistry(registry))

	ctx := context.Background()
	err = executor.Start("calc-fail").
		Input("a", 2).
		Input("b", 3).
		Input("factor", 4).
		WithID("test-exec-2").
		Execute(ctx)
	require.Error(t, err)

	// Verify add was called
	assert.Len(t, ctrl.addCalls, 1)

	// Verify multiply was attempted
	assert.Len(t, ctrl.multiplyCalls, 1)

	// Verify undo was called for add (not multiply since it failed)
	assert.Len(t, ctrl.undoAddCalls, 1)
	assert.Equal(t, AddNumbersOut{Sum: 5}, ctrl.undoAddCalls[0])

	// Multiply doesn't produce output on failure, so no undo
	assert.Empty(t, ctrl.undoMultCalls)

	// Verify final state
	exec, err := storage.Get(ctx, "test-exec-2")
	require.NoError(t, err)
	assert.Equal(t, StatusFailed, exec.Status)
}

func TestExecutor_ActionCancellationStillCompensates(t *testing.T) {
	registry := NewRegistry()
	ctrl := &testController{}
	started := make(chan struct{})
	waitForCancellation := func(ctx context.Context, _ MultiplyIn) (MultiplyOut, error) {
		close(started)
		<-ctx.Done()
		return MultiplyOut{}, ctx.Err()
	}

	err := Define("action-context-cancel").
		Using(ctrl).
		Action("add", AddNumbers).Undo(UndoAddNumbers).
		Action("wait", waitForCancellation).Undo(UndoMultiply).
		RegisterTo(registry)
	require.NoError(t, err)

	storage := NewMemoryStorage()
	executor := NewExecutor(storage, WithRegistry(registry))
	actionCtx, cancelAction := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- executor.Start("action-context-cancel").
			Input("a", 2).
			Input("b", 3).
			Input("factor", 4).
			WithActionContext(actionCtx).
			WithID("action-context-cancel-1").
			Execute(context.Background())
	}()

	<-started
	cancelAction()
	require.Error(t, <-done)
	assert.Len(t, ctrl.undoAddCalls, 1,
		"the live control context must carry compensation after action cancellation")

	exec, err := storage.Get(context.Background(), "action-context-cancel-1")
	require.NoError(t, err)
	assert.Equal(t, StatusFailed, exec.Status)
}

func TestExecutor_ActionCancellationAfterSuccessCompensates(t *testing.T) {
	registry := NewRegistry()
	ctrl := &testController{}
	started := make(chan struct{}, 1)
	completeAfterCancellation := func(ctx context.Context, in AddNumbersIn) (AddNumbersOut, error) {
		select {
		case started <- struct{}{}:
		default:
		}
		<-ctx.Done()
		ctrl.recordAdd(in)
		return AddNumbersOut{Sum: in.A + in.B}, nil
	}

	err := Define("action-context-success-after-cancel").
		Using(ctrl).
		Action("add", completeAfterCancellation).Undo(UndoAddNumbers).
		RegisterTo(registry)
	require.NoError(t, err)

	storage := NewMemoryStorage()
	executor := NewExecutor(storage, WithRegistry(registry))
	actionCtx, cancelAction := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- executor.Start("action-context-success-after-cancel").
			Input("a", 2).
			Input("b", 3).
			WithActionContext(actionCtx).
			WithID("action-context-success-after-cancel-1").
			Execute(context.Background())
	}()

	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("action did not start")
	}
	cancelAction()

	select {
	case err := <-done:
		require.Error(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("successful action did not finish compensation")
	}

	assert.Len(t, ctrl.undoAddCalls, 1)
	exec, err := storage.Get(context.Background(), "action-context-success-after-cancel-1")
	require.NoError(t, err)
	assert.Equal(t, StatusFailed, exec.Status)
	assert.NotNil(t, exec.ExecutedActions["add"].UndoneAt)
}

func TestExecutor_ResumedExecutionChecksActionCancellationBeforeCompletion(t *testing.T) {
	registry := NewRegistry()
	ctrl := &testController{}
	err := Define("resumed-action-context-cancel").
		Using(ctrl).
		Action("add", AddNumbers).Undo(UndoAddNumbers).
		RegisterTo(registry)
	require.NoError(t, err)

	storage := NewMemoryStorage()
	now := time.Now()
	require.NoError(t, storage.Save(context.Background(), &Execution{
		ID:                "resumed-action-context-cancel-1",
		DefinitionName:    "resumed-action-context-cancel",
		DefinitionVersion: 1,
		InitialInputs:     map[string]any{"a": 2, "b": 3},
		Status:            StatusRunning,
		ExecutedActions: map[string]*ActionResult{
			"add": {
				Output:     []byte(`{"Sum":5}`),
				ExecutedAt: now,
			},
		},
		ExecutionOrder: []string{"add"},
		CreatedAt:      now,
		UpdatedAt:      now,
	}))

	actionCtx, cancelAction := context.WithCancel(context.Background())
	cancelAction()
	executor := NewExecutor(storage, WithRegistry(registry))
	err = executor.Start("resumed-action-context-cancel").
		WithActionContext(actionCtx).
		WithID("resumed-action-context-cancel-1").
		Execute(context.Background())
	require.Error(t, err)

	assert.Len(t, ctrl.undoAddCalls, 1)
	exec, err := storage.Get(context.Background(), "resumed-action-context-cancel-1")
	require.NoError(t, err)
	assert.Equal(t, StatusFailed, exec.Status)
	assert.NotNil(t, exec.ExecutedActions["add"].UndoneAt)
}

func TestExecutor_Recovery(t *testing.T) {
	registry := NewRegistry()

	ctrl := &testController{}
	err := Define("recoverable").
		Using(ctrl).
		Action("add", AddNumbers).Undo(UndoAddNumbers).
		Action("multiply", Multiply).Undo(UndoMultiply).
		RegisterTo(registry)
	require.NoError(t, err)

	storage := NewMemoryStorage()

	// Simulate a crashed execution
	// Note: Output uses uppercase "Sum" because Go's json.Marshal uses field names as-is
	crashedExec := &Execution{
		ID:                "crashed-exec",
		DefinitionName:    "recoverable",
		DefinitionVersion: 1,
		InitialInputs:     map[string]any{"a": float64(2), "b": float64(3), "factor": float64(4)},
		Status:            StatusRunning,
		ExecutedActions: map[string]*ActionResult{
			"add": {
				Output:     []byte(`{"Sum":5}`),
				ExecutedAt: time.Now(),
			},
		},
		ExecutionOrder: []string{"add"},
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}
	storage.Save(context.Background(), crashedExec)

	// Create new executor and recover
	executor := NewExecutor(storage, WithRegistry(registry))
	err = executor.Recover(context.Background())
	require.NoError(t, err)

	// Verify only multiply was called (add was already done)
	assert.Empty(t, ctrl.addCalls) // add not called again
	assert.Len(t, ctrl.multiplyCalls, 1)

	// Verify final state
	exec, err := storage.Get(context.Background(), "crashed-exec")
	require.NoError(t, err)
	assert.Equal(t, StatusCompleted, exec.Status)
}

func TestExecutor_RecoveryScope(t *testing.T) {
	registry := NewRegistry()
	ctrl := &testController{}
	require.NoError(t, Define("runner-scoped").
		Using(ctrl).
		Action("add", AddNumbers).Undo(UndoAddNumbers).
		Action("multiply", Multiply).Undo(UndoMultiply).
		RegisterTo(registry))

	ctx := context.Background()
	storage := NewMemoryStorage()
	require.NoError(t, storage.Save(ctx, &Execution{
		ID:                "runner-a-execution",
		DefinitionName:    "runner-scoped",
		DefinitionVersion: 1,
		InitialInputs:     map[string]any{"a": float64(2), "b": float64(3), "factor": float64(4)},
		RecoveryScope:     "node/runner-a",
		Status:            StatusRunning,
		ExecutedActions: map[string]*ActionResult{
			"add": {
				Output:     []byte(`{"Sum":5}`),
				ExecutedAt: time.Now(),
			},
		},
		ExecutionOrder: []string{"add"},
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}))
	require.NoError(t, storage.Save(ctx, &Execution{
		ID:                "runner-b-execution",
		DefinitionName:    "runner-scoped",
		DefinitionVersion: 1,
		InitialInputs:     map[string]any{"a": float64(6), "b": float64(7), "factor": float64(2)},
		RecoveryScope:     "node/runner-b",
		Status:            StatusRunning,
		ExecutedActions: map[string]*ActionResult{
			"add": {
				Output:     []byte(`{"Sum":13}`),
				ExecutedAt: time.Now(),
			},
		},
		ExecutionOrder: []string{"add"},
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}))

	runnerB := NewExecutor(storage,
		WithRegistry(registry),
		WithRecoveryScope("node/runner-b"),
	)
	require.NoError(t, runnerB.Recover(ctx))
	assert.Empty(t, ctrl.addCalls)
	require.Equal(t, []MultiplyIn{{Sum: 13, Factor: 2}}, ctrl.multiplyCalls,
		"runner B must only resume its own execution")

	stillRunning, err := storage.Get(ctx, "runner-a-execution")
	require.NoError(t, err)
	assert.Equal(t, StatusRunning, stillRunning.Status)
	assert.Equal(t, "node/runner-a", stillRunning.RecoveryScope)
	runnerBCompleted, err := storage.Get(ctx, "runner-b-execution")
	require.NoError(t, err)
	assert.Equal(t, StatusCompleted, runnerBCompleted.Status)

	runnerA := NewExecutor(storage,
		WithRegistry(registry),
		WithRecoveryScope("node/runner-a"),
	)
	require.NoError(t, runnerA.Recover(ctx))
	assert.Empty(t, ctrl.addCalls, "the completed action must not run again")
	require.Equal(t, []MultiplyIn{
		{Sum: 13, Factor: 2},
		{Sum: 5, Factor: 4},
	}, ctrl.multiplyCalls, "runner A must only add its own execution")

	completed, err := storage.Get(ctx, "runner-a-execution")
	require.NoError(t, err)
	assert.Equal(t, StatusCompleted, completed.Status)
}

func TestExecutor_NamedReentryHonorsRecoveryScope(t *testing.T) {
	registry := NewRegistry()
	ctrl := &testController{}
	require.NoError(t, Define("scoped-reentry").
		Using(ctrl).
		Action("add", AddNumbers).Undo(UndoAddNumbers).
		RegisterTo(registry))

	ctx := context.Background()

	t.Run("different scope is rejected before execution", func(t *testing.T) {
		storage := NewMemoryStorage()
		require.NoError(t, storage.Save(ctx, &Execution{
			ID:                "owned-by-a",
			DefinitionName:    "scoped-reentry",
			DefinitionVersion: 1,
			InitialInputs:     map[string]any{"a": float64(2), "b": float64(3)},
			RecoveryScope:     "node/runner-a",
			Status:            StatusRunning,
			ExecutedActions:   map[string]*ActionResult{},
			ExecutionOrder:    []string{},
			CreatedAt:         time.Now(),
			UpdatedAt:         time.Now(),
		}))

		runnerB := NewExecutor(storage,
			WithRegistry(registry),
			WithRecoveryScope("node/runner-b"),
		)
		err := runnerB.Start("scoped-reentry").
			Input("a", 2).
			Input("b", 3).
			WithID("owned-by-a").
			Execute(ctx)

		assert.ErrorIs(t, err, ErrRecoveryScopeMismatch)
		assert.Empty(t, ctrl.addCalls)
	})

	t.Run("routed execute adopts a legacy unscoped record", func(t *testing.T) {
		storage := NewMemoryStorage()
		require.NoError(t, storage.Save(ctx, &Execution{
			ID:                "legacy-unscoped",
			DefinitionName:    "scoped-reentry",
			DefinitionVersion: 1,
			InitialInputs:     map[string]any{"a": float64(5), "b": float64(7)},
			Status:            StatusRunning,
			ExecutedActions:   map[string]*ActionResult{},
			ExecutionOrder:    []string{},
			CreatedAt:         time.Now(),
			UpdatedAt:         time.Now(),
		}))

		runnerA := NewExecutor(storage,
			WithRegistry(registry),
			WithRecoveryScope("node/runner-a"),
		)
		require.NoError(t, runnerA.Recover(ctx))
		assert.Empty(t, ctrl.addCalls,
			"startup recovery must not let a scoped executor claim a legacy record")

		require.NoError(t, runnerA.Start("scoped-reentry").
			Input("a", 999).
			Input("b", 999).
			WithID("legacy-unscoped").
			Execute(ctx))

		adopted, err := storage.Get(ctx, "legacy-unscoped")
		require.NoError(t, err)
		assert.Equal(t, "node/runner-a", adopted.RecoveryScope)
		assert.Equal(t, StatusCompleted, adopted.Status)
		require.Len(t, ctrl.addCalls, 1)
		assert.Equal(t, AddNumbersIn{A: 5, B: 7}, ctrl.addCalls[0],
			"adoption must keep the first attempt's durable inputs")
	})
}

func TestExecutor_ContextCancellation(t *testing.T) {
	registry := NewRegistry()

	ctrl := &testController{}
	err := Define("cancellable").
		Using(ctrl).
		Action("add", AddNumbers).Undo(UndoAddNumbers).
		Action("multiply", Multiply).Undo(UndoMultiply).
		RegisterTo(registry)
	require.NoError(t, err)

	storage := NewMemoryStorage()
	executor := NewExecutor(storage, WithRegistry(registry))

	// Create an already-cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err = executor.Start("cancellable").
		Input("a", 2).
		Input("b", 3).
		Input("factor", 4).
		WithID("cancel-exec").
		Execute(ctx)

	// Should return an error due to cancellation
	require.Error(t, err)
	assert.Contains(t, err.Error(), "interrupted")

	// Verify saga is left in Running state (no actions executed)
	exec, err := storage.Get(context.Background(), "cancel-exec")
	require.NoError(t, err)
	assert.Equal(t, StatusRunning, exec.Status)
	assert.Empty(t, exec.ExecutionOrder)

	// No actions should have been called
	assert.Empty(t, ctrl.addCalls)
	assert.Empty(t, ctrl.multiplyCalls)
}

func TestGet_Panic(t *testing.T) {
	ctx := context.Background()
	assert.Panics(t, func() {
		Get[*testController](ctx)
	})
}

func TestTryGet(t *testing.T) {
	ctx := context.Background()

	// Without dependency
	ctrl, ok := TryGet[*testController](ctx)
	assert.False(t, ok)
	assert.Nil(t, ctrl)

	// With dependency
	ctrl = &testController{}
	ctx = injectDependencies(ctx, []any{ctrl})
	got, ok := TryGet[*testController](ctx)
	assert.True(t, ok)
	assert.Same(t, ctrl, got)
}

// testService is an interface for testing UsingAs.
type testService interface {
	DoWork() string
}

// testServiceImpl implements testService.
type testServiceImpl struct {
	name string
}

func (s *testServiceImpl) DoWork() string {
	return s.name
}

func TestUsingAs_InterfaceInjection(t *testing.T) {
	impl := &testServiceImpl{name: "test-impl"}

	// Build a saga using UsingAs to key by interface type
	b := Define("interface-test")
	UsingAs[testService](b, impl)

	// Verify the dependency is stored correctly
	assert.Len(t, b.dependencies, 1)

	// Inject and retrieve by interface type
	ctx := context.Background()
	ctx = injectDependencies(ctx, b.dependencies)

	// Should be retrievable by interface type
	svc, ok := TryGet[testService](ctx)
	assert.True(t, ok, "should find dependency by interface type")
	assert.Equal(t, "test-impl", svc.DoWork())

	// Should NOT be retrievable by concrete type (different key)
	_, ok = TryGet[*testServiceImpl](ctx)
	assert.False(t, ok, "should not find dependency by concrete type when keyed by interface")
}

func TestInputs_Get(t *testing.T) {
	initial := map[string]any{
		"name":  "test",
		"count": float64(42),
	}
	outputs := map[string]json.RawMessage{
		"result": json.RawMessage(`"success"`),
	}

	inputs := newInputs(initial, outputs)

	// Get from initial
	var name string
	err := inputs.Get("name", &name)
	require.NoError(t, err)
	assert.Equal(t, "test", name)

	// Get from outputs (takes precedence)
	var result string
	err = inputs.Get("result", &result)
	require.NoError(t, err)
	assert.Equal(t, "success", result)

	// Missing key
	var missing string
	err = inputs.Get("missing", &missing)
	require.Error(t, err)
}

func TestInputs_Has(t *testing.T) {
	initial := map[string]any{"a": 1}
	outputs := map[string]json.RawMessage{"b": json.RawMessage("2")}

	inputs := newInputs(initial, outputs)

	assert.True(t, inputs.Has("a"))
	assert.True(t, inputs.Has("b"))
	assert.False(t, inputs.Has("c"))
}

func TestInputs_Keys(t *testing.T) {
	initial := map[string]any{"a": 1, "b": 2}
	outputs := map[string]json.RawMessage{"b": json.RawMessage("3"), "c": json.RawMessage("4")}

	inputs := newInputs(initial, outputs)
	keys := inputs.Keys()

	assert.Len(t, keys, 3)
	assert.Contains(t, keys, "a")
	assert.Contains(t, keys, "b")
	assert.Contains(t, keys, "c")
}

// Test types for optional input testing
type OptionalIn struct {
	Required int `saga:"required"`
	Optional int `saga:"optional,optional"`
}

type OptionalOut struct {
	Result int `saga:"result"`
}

func OptionalAction(ctx context.Context, in OptionalIn) (OptionalOut, error) {
	return OptionalOut{Result: in.Required + in.Optional}, nil
}

func UndoOptionalAction(ctx context.Context, in OptionalIn, out OptionalOut) error {
	return nil
}

func TestExecutor_MissingRequiredInput(t *testing.T) {
	registry := NewRegistry()

	err := Define("required-test").
		Action("optional-action", OptionalAction).Undo(UndoOptionalAction).
		RegisterTo(registry)
	require.NoError(t, err)

	storage := NewMemoryStorage()
	executor := NewExecutor(storage, WithRegistry(registry))

	ctx := context.Background()
	// Missing "required" input should cause an error
	err = executor.Start("required-test").
		Input("optional", 10).
		WithID("missing-required").
		Execute(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing required input")
	assert.Contains(t, err.Error(), "required")
}

func TestExecutor_OptionalInput(t *testing.T) {
	registry := NewRegistry()

	err := Define("optional-test").
		Action("optional-action", OptionalAction).Undo(UndoOptionalAction).
		RegisterTo(registry)
	require.NoError(t, err)

	storage := NewMemoryStorage()
	executor := NewExecutor(storage, WithRegistry(registry))

	ctx := context.Background()
	// Missing "optional" input should use zero value (0)
	err = executor.Start("optional-test").
		Input("required", 5).
		WithID("optional-missing").
		Execute(ctx)
	require.NoError(t, err)

	// Verify execution completed
	exec, err := storage.Get(ctx, "optional-missing")
	require.NoError(t, err)
	assert.Equal(t, StatusCompleted, exec.Status)

	// Result should be 5 + 0 = 5
	var output OptionalOut
	err = json.Unmarshal(exec.ExecutedActions["optional-action"].Output, &output)
	require.NoError(t, err)
	assert.Equal(t, 5, output.Result)
}

// failingStorage wraps MemoryStorage but fails Save after N successful calls.
type failingStorage struct {
	*MemoryStorage
	failAfter int
	saveCount int
	mu        sync.Mutex
}

func newFailingStorage(failAfter int) *failingStorage {
	return &failingStorage{
		MemoryStorage: NewMemoryStorage(),
		failAfter:     failAfter,
	}
}

func (f *failingStorage) Save(ctx context.Context, exec *Execution) error {
	f.mu.Lock()
	f.saveCount++
	count := f.saveCount
	f.mu.Unlock()

	if count > f.failAfter {
		return errors.New("simulated storage failure")
	}
	return f.MemoryStorage.Save(ctx, exec)
}

func TestExecutor_StorageFailureTriggersUndo(t *testing.T) {
	registry := NewRegistry()

	ctrl := &testController{}
	err := Define("storage-fail-test").
		Using(ctrl).
		Action("add", AddNumbers).Undo(UndoAddNumbers).
		Action("multiply", Multiply).Undo(UndoMultiply).
		RegisterTo(registry)
	require.NoError(t, err)

	// Storage that fails after 3 saves:
	// 1. Initial save (StatusPending)
	// 2. Status update (StatusRunning)
	// 3. After "add" action succeeds
	// 4. FAIL - after "multiply" action succeeds
	storage := newFailingStorage(3)
	executor := NewExecutor(storage, WithRegistry(registry))

	err = executor.Start("storage-fail-test").
		Input("a", 2).
		Input("b", 3).
		Input("factor", 4).
		WithID("storage-fail-exec").
		Execute(context.Background())

	// Saga returns an error indicating it failed and was rolled back
	require.Error(t, err)
	assert.Contains(t, err.Error(), "saga failed")

	// Both actions should have been executed
	assert.Len(t, ctrl.addCalls, 1)
	assert.Len(t, ctrl.multiplyCalls, 1)

	// KEY ASSERTION: Both actions should have been undone because
	// storage failure after multiply triggered compensation.
	// This verifies the fix: we don't leave the saga in a broken state
	// where an action executed but wasn't recorded.
	assert.Len(t, ctrl.undoMultCalls, 1, "multiply should be undone")
	assert.Len(t, ctrl.undoAddCalls, 1, "add should be undone")
}

func TestExecutor_FailedUndoNotMarkedAsUndone(t *testing.T) {
	registry := NewRegistry()

	ctrl := &testController{
		failMultiply: true, // multiply fails, triggering undo
		failUndoAdd:  true, // undo of add also fails
	}
	err := Define("undo-fail-test").
		Using(ctrl).
		Action("add", AddNumbers).Undo(UndoAddNumbers).
		Action("multiply", Multiply).Undo(UndoMultiply).
		RegisterTo(registry)
	require.NoError(t, err)

	storage := NewMemoryStorage()
	executor := NewExecutor(storage, WithRegistry(registry))

	err = executor.Start("undo-fail-test").
		Input("a", 2).
		Input("b", 3).
		Input("factor", 4).
		WithID("undo-fail-exec").
		Execute(context.Background())

	// Saga should return an error (with undo errors)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "undo errors")

	// Add executed, multiply was attempted
	assert.Len(t, ctrl.addCalls, 1)
	assert.Len(t, ctrl.multiplyCalls, 1)

	// Undo was attempted for add (multiply failed so no output to undo)
	assert.Len(t, ctrl.undoAddCalls, 1)

	// KEY ASSERTION: The "add" action should NOT be marked as undone
	// because its undo failed. This ensures recovery will retry the undo.
	exec, err := storage.Get(context.Background(), "undo-fail-exec")
	require.NoError(t, err)

	addResult := exec.ExecutedActions["add"]
	require.NotNil(t, addResult)
	assert.Nil(t, addResult.UndoneAt, "add should NOT be marked as undone since undo failed")

	// KEY ASSERTION: Status should be Undoing (not Failed) so recovery can retry
	assert.Equal(t, StatusUndoing, exec.Status, "saga should stay in Undoing status when undo fails")
}

func TestExecutor_RecoveryAfterActionFailure(t *testing.T) {
	// This test simulates a crash after an action fails but before undo starts.
	// Without the fix, recovery would incorrectly complete the saga instead of undoing.

	registry := NewRegistry()

	ctrl := &testController{}
	err := Define("recovery-after-fail").
		Using(ctrl).
		Action("add", AddNumbers).Undo(UndoAddNumbers).
		Action("multiply", Multiply).Undo(UndoMultiply).
		RegisterTo(registry)
	require.NoError(t, err)

	storage := NewMemoryStorage()

	// Simulate a crashed execution where multiply failed but undo never started.
	// This is the state we'd have if we crashed after recording the failure
	// but before runUndo set StatusUndoing.
	crashedExec := &Execution{
		ID:                "crashed-after-fail",
		DefinitionName:    "recovery-after-fail",
		DefinitionVersion: 1,
		InitialInputs:     map[string]any{"a": float64(2), "b": float64(3), "factor": float64(4)},
		Status:            StatusRunning, // BUG: should be Undoing
		Error:             "action \"multiply\" failed: multiply failed",
		ExecutedActions: map[string]*ActionResult{
			"add": {
				Output:     []byte(`{"Sum":5}`),
				ExecutedAt: time.Now(),
			},
			"multiply": {
				ExecutedAt: time.Now(),
				Error:      "multiply failed",
			},
		},
		ExecutionOrder: []string{"add", "multiply"},
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}
	storage.Save(context.Background(), crashedExec)

	// Recover
	executor := NewExecutor(storage, WithRegistry(registry))
	_ = executor.Recover(context.Background())

	// The saga had a failed action - recovery should have triggered undo
	exec, _ := storage.Get(context.Background(), "crashed-after-fail")

	// KEY ASSERTION: After recovery, the saga should be Failed (undone),
	// not Completed. The "add" action should be undone.
	assert.Equal(t, StatusFailed, exec.Status,
		"Saga with failed action should be Failed after recovery, not %s", exec.Status)
	assert.Len(t, ctrl.undoAddCalls, 1, "add should have been undone during recovery")
}

// UnserializableOut contains a channel which json.Marshal cannot serialize.
type UnserializableOut struct {
	Value int
	Ch    chan int // channels can't be serialized
}

type unserializableController struct {
	executeCalls int
	undoCalls    int
	failUndo     bool
}

func UnserializableAction(ctx context.Context, in AddNumbersIn) (UnserializableOut, error) {
	ctrl := Get[*unserializableController](ctx)
	ctrl.executeCalls++
	return UnserializableOut{Value: in.A + in.B, Ch: make(chan int)}, nil
}

func UndoUnserializableAction(ctx context.Context, in AddNumbersIn, out UnserializableOut) error {
	ctrl := Get[*unserializableController](ctx)
	ctrl.undoCalls++
	if ctrl.failUndo {
		return errors.New("undo failed")
	}
	return nil
}

func TestExecutor_SerializationFailurePlusUndoFailure(t *testing.T) {
	// This tests the edge case where:
	// 1. Action executes successfully
	// 2. Output serialization fails (contains channel)
	// 3. Immediate undo also fails
	// The action should still be recorded so runUndo can retry.

	registry := NewRegistry()

	ctrl := &unserializableController{failUndo: true}
	err := Define("unserializable-test").
		Using(ctrl).
		Action("unserializable", UnserializableAction).Undo(UndoUnserializableAction).
		RegisterTo(registry)
	require.NoError(t, err)

	storage := NewMemoryStorage()
	executor := NewExecutor(storage, WithRegistry(registry))

	err = executor.Start("unserializable-test").
		Input("a", 2).
		Input("b", 3).
		WithID("unserializable-exec").
		Execute(context.Background())

	// Saga should fail (undo errors)
	require.Error(t, err)

	// Action was executed
	assert.Equal(t, 1, ctrl.executeCalls)

	// Undo was attempted twice:
	// 1. Immediate undo after serialization failure (failed)
	// 2. runUndo retry (also failed, but at least it was attempted)
	assert.Equal(t, 2, ctrl.undoCalls, "undo should be attempted twice: immediate + runUndo retry")

	// KEY ASSERTION: The action should be recorded even though immediate undo failed
	exec, err := storage.Get(context.Background(), "unserializable-exec")
	require.NoError(t, err)

	_, recorded := exec.ExecutedActions["unserializable"]
	assert.True(t, recorded, "action should be recorded even when immediate undo fails")
}

// Edge dependency test types

type edgeStepAIn struct {
	Val int `saga:"val"`
}
type edgeStepAOut struct {
	AResult int  `saga:"a_result"`
	ADone   Edge `saga:"a_done"`
}

type edgeStepBIn struct {
	Val   int  `saga:"val"`
	ADone Edge `saga:"a_done"`
}
type edgeStepBOut struct {
	BResult int `saga:"b_result"`
}

type edgeTestController struct {
	mu    sync.Mutex
	order []string
}

func (c *edgeTestController) record(name string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.order = append(c.order, name)
}

func edgeStepAExec(ctx context.Context, in edgeStepAIn) (edgeStepAOut, error) {
	Get[*edgeTestController](ctx).record("A")
	return edgeStepAOut{AResult: in.Val * 10}, nil
}

func edgeStepAUndo(_ context.Context, _ edgeStepAIn, _ edgeStepAOut) error { return nil }

func edgeStepBExec(ctx context.Context, in edgeStepBIn) (edgeStepBOut, error) {
	Get[*edgeTestController](ctx).record("B")
	return edgeStepBOut{BResult: in.Val + 1}, nil
}

func edgeStepBUndo(_ context.Context, _ edgeStepBIn, _ edgeStepBOut) error { return nil }

func TestExecutor_EdgeDependency(t *testing.T) {
	registry := NewRegistry()

	ctrl := &edgeTestController{}
	err := Define("edge-exec").
		Using(ctrl).
		Action("step-b", edgeStepBExec).Undo(edgeStepBUndo).
		Action("step-a", edgeStepAExec).Undo(edgeStepAUndo).
		RegisterTo(registry)
	require.NoError(t, err)

	storage := NewMemoryStorage()
	executor := NewExecutor(storage, WithRegistry(registry))

	err = executor.Start("edge-exec").
		Input("val", 5).
		WithID("edge-exec-1").
		Execute(context.Background())
	require.NoError(t, err)

	// Verify B ran after A (Edge dependency)
	assert.Equal(t, []string{"A", "B"}, ctrl.order)

	// Verify execution completed
	exec, err := storage.Get(context.Background(), "edge-exec-1")
	require.NoError(t, err)
	assert.Equal(t, StatusCompleted, exec.Status)
	assert.Equal(t, []string{"step-a", "step-b"}, exec.ExecutionOrder)
}

// A caller that names its execution after the entity it belongs to re-enters
// Execute on every reconcile pass. That must continue the existing execution
// rather than start a fresh one over the top of it.
func TestExecutor_ReExecuteResumesInsteadOfRestarting(t *testing.T) {
	registry := NewRegistry()

	ctrl := &testController{}
	err := Define("resume-calc").
		Using(ctrl).
		Action("add", AddNumbers).Undo(UndoAddNumbers).
		Action("multiply", Multiply).Undo(UndoMultiply).
		RegisterTo(registry)
	require.NoError(t, err)

	storage := NewMemoryStorage()
	executor := NewExecutor(storage, WithRegistry(registry))
	ctx := context.Background()

	start := func() error {
		return executor.Start("resume-calc").
			Input("a", 2).
			Input("b", 3).
			Input("factor", 4).
			WithID("assoc-42").
			Execute(ctx)
	}

	require.NoError(t, start())
	require.Len(t, ctrl.addCalls, 1)
	require.Len(t, ctrl.multiplyCalls, 1)

	// Same name again: the work is already done, so nothing should re-run and
	// the record must survive intact.
	require.NoError(t, start())
	assert.Len(t, ctrl.addCalls, 1, "completed action must not run twice")
	assert.Len(t, ctrl.multiplyCalls, 1, "completed action must not run twice")

	exec, err := storage.Get(ctx, "assoc-42")
	require.NoError(t, err)
	assert.Equal(t, StatusCompleted, exec.Status)
	assert.Len(t, exec.ExecutionOrder, 2, "re-entry must not clobber the record")
}

// The case MIR-1524 is about: a process dies with an execution half finished,
// and the next pass picks it up where it stopped instead of starting over on
// top of what the first attempt already built.
func TestExecutor_ReExecuteContinuesInterruptedRun(t *testing.T) {
	registry := NewRegistry()

	ctrl := &testController{}
	err := Define("interrupted-calc").
		Using(ctrl).
		Action("add", AddNumbers).Undo(UndoAddNumbers).
		Action("multiply", Multiply).Undo(UndoMultiply).
		RegisterTo(registry)
	require.NoError(t, err)

	storage := NewMemoryStorage()
	ctx := context.Background()

	// Stand in for a crash after the first action: the record says running,
	// with only "add" recorded.
	require.NoError(t, storage.Save(ctx, &Execution{
		ID:                "assoc-99",
		DefinitionName:    "interrupted-calc",
		DefinitionVersion: 1,
		InitialInputs:     map[string]any{"a": float64(2), "b": float64(3), "factor": float64(4)},
		Status:            StatusRunning,
		ExecutedActions: map[string]*ActionResult{
			// Output keys are Go field names, as json.Marshal writes them.
			"add": {Output: []byte(`{"Sum":5}`), ExecutedAt: time.Now()},
		},
		ExecutionOrder: []string{"add"},
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}))

	executor := NewExecutor(storage, WithRegistry(registry))
	require.NoError(t, executor.Start("interrupted-calc").
		Input("a", 2).
		Input("b", 3).
		Input("factor", 4).
		WithID("assoc-99").
		Execute(ctx))

	assert.Empty(t, ctrl.addCalls, "already-recorded action must not re-run")
	require.Len(t, ctrl.multiplyCalls, 1, "remaining action must run")
	assert.Equal(t, MultiplyIn{Sum: 5, Factor: 4}, ctrl.multiplyCalls[0],
		"resumed action must see the first attempt's output")

	exec, err := storage.Get(ctx, "assoc-99")
	require.NoError(t, err)
	assert.Equal(t, StatusCompleted, exec.Status)
}

// A saga that already failed and compensated hands its error back rather than
// reporting a quiet success. Retrying under the same name would run against
// resources the rollback has already torn down, so the decision belongs to
// whoever owns the operation.
func TestExecutor_ReExecuteReportsAnAlreadyFailedRun(t *testing.T) {
	registry := NewRegistry()

	ctrl := &testController{}
	err := Define("failed-calc").
		Using(ctrl).
		Action("add", AddNumbers).Undo(UndoAddNumbers).
		Action("multiply", Multiply).Undo(UndoMultiply).
		RegisterTo(registry)
	require.NoError(t, err)

	storage := NewMemoryStorage()
	ctx := context.Background()

	require.NoError(t, storage.Save(ctx, &Execution{
		ID:                "assoc-failed",
		DefinitionName:    "failed-calc",
		DefinitionVersion: 1,
		InitialInputs:     map[string]any{"a": float64(2), "b": float64(3), "factor": float64(4)},
		Status:            StatusFailed,
		Error:             "out of turkey",
		CreatedAt:         time.Now(),
		UpdatedAt:         time.Now(),
	}))

	executor := NewExecutor(storage, WithRegistry(registry))
	err = executor.Start("failed-calc").
		Input("a", 2).
		Input("b", 3).
		Input("factor", 4).
		WithID("assoc-failed").
		Execute(ctx)

	require.Error(t, err, "a failed execution must not read as success")
	assert.Contains(t, err.Error(), "out of turkey", "and must carry what actually went wrong")
	assert.Empty(t, ctrl.addCalls, "a compensated saga must not quietly run again")
	assert.Empty(t, ctrl.multiplyCalls)
}

// A status resume does not recognise must not read as a finished saga. This is
// the same shape as the bug the rest of this change is about: a value nobody
// listed falling through a switch and being reported as success.
func TestExecutor_ReExecuteRejectsAnUnrecognizedStatus(t *testing.T) {
	registry := NewRegistry()

	ctrl := &testController{}
	err := Define("odd-calc").
		Using(ctrl).
		Action("add", AddNumbers).Undo(UndoAddNumbers).
		RegisterTo(registry)
	require.NoError(t, err)

	storage := NewMemoryStorage()
	ctx := context.Background()

	require.NoError(t, storage.Save(ctx, &Execution{
		ID:                "assoc-odd",
		DefinitionName:    "odd-calc",
		DefinitionVersion: 1,
		InitialInputs:     map[string]any{"a": float64(2), "b": float64(3)},
		Status:            Status("marinating"), // a state this version has never heard of
		CreatedAt:         time.Now(),
		UpdatedAt:         time.Now(),
	}))

	executor := NewExecutor(storage, WithRegistry(registry))
	err = executor.Start("odd-calc").
		Input("a", 2).
		Input("b", 3).
		WithID("assoc-odd").
		Execute(ctx)

	require.Error(t, err, "an unknown status must not be reported as a completed saga")
	assert.Contains(t, err.Error(), "marinating", "and must say what it could not make sense of")
	assert.Empty(t, ctrl.addCalls, "nor should it run actions over a record it cannot read")
}

// Two passes arriving at once must not both drive the same execution. The
// second is told the work is in progress, which is neither a failure nor a
// success, so a caller can tell it apart from a finished run.
func TestExecutor_ConcurrentExecuteDoesNotDriveTheSameRunTwice(t *testing.T) {
	registry := NewRegistry()

	ctrl := &testController{}
	release := make(chan struct{})
	entered := make(chan struct{})
	err := Define("slow-calc").
		Using(ctrl).
		Action("add", func(ctx context.Context, in AddNumbersIn) (AddNumbersOut, error) {
			close(entered)
			<-release
			return AddNumbers(ctx, in)
		}).Undo(UndoAddNumbers).
		RegisterTo(registry)
	require.NoError(t, err)

	storage := NewMemoryStorage()
	ctx := context.Background()
	executor := NewExecutor(storage, WithRegistry(registry))

	start := func() error {
		return executor.Start("slow-calc").
			Input("a", 2).
			Input("b", 3).
			WithID("assoc-concurrent").
			Execute(ctx)
	}

	first := make(chan error, 1)
	go func() { first <- start() }()

	<-entered // the first pass is inside the action and holds the claim
	assert.ErrorIs(t, start(), ErrExecutionInProgress,
		"the second pass must be told the work is already being driven")

	close(release)
	require.NoError(t, <-first)

	exec, err := storage.Get(ctx, "assoc-concurrent")
	require.NoError(t, err)
	assert.Equal(t, StatusCompleted, exec.Status)
	assert.Len(t, exec.ExecutionOrder, 1, "the action must have run exactly once")
}

// A caller that knows the recorded outcome no longer describes reality can
// retire it. Anything else is left alone, so a caller can say what it means
// without first reading the record.
func TestDropIf(t *testing.T) {
	ctx := context.Background()

	save := func(t *testing.T, s Storage, id string, status Status) {
		t.Helper()
		require.NoError(t, s.Save(ctx, &Execution{ID: id, DefinitionName: "d", Status: status}))
	}
	exists := func(t *testing.T, s Storage, id string) bool {
		t.Helper()
		_, err := s.Get(ctx, id)
		return err == nil
	}

	t.Run("failed is dropped, completed is not", func(t *testing.T) {
		s := NewMemoryStorage()
		save(t, s, "a", StatusFailed)
		save(t, s, "b", StatusCompleted)
		require.NoError(t, DropIfFailed(ctx, s, "a"))
		require.NoError(t, DropIfFailed(ctx, s, "b"))
		assert.False(t, exists(t, s, "a"))
		assert.True(t, exists(t, s, "b"))
	})

	t.Run("completed is dropped, running is not", func(t *testing.T) {
		s := NewMemoryStorage()
		save(t, s, "a", StatusCompleted)
		save(t, s, "b", StatusRunning)
		require.NoError(t, DropIfCompleted(ctx, s, "a"))
		require.NoError(t, DropIfCompleted(ctx, s, "b"))
		assert.False(t, exists(t, s, "a"))
		assert.True(t, exists(t, s, "b"), "an in-flight run must never be pulled out from under its driver")
	})

	t.Run("absent is not an error", func(t *testing.T) {
		s := NewMemoryStorage()
		assert.NoError(t, DropIfFailed(ctx, s, "nope"))
		assert.NoError(t, DropIfCompleted(ctx, s, "nope"))
	})
}

// An id collision must not run one saga's actions against another's record.
func TestExecutor_ReExecuteRejectsADifferentDefinitionUnderTheSameName(t *testing.T) {
	registry := NewRegistry()
	ctrl := &testController{}
	require.NoError(t, Define("calc-a").Using(ctrl).
		Action("add", AddNumbers).Undo(UndoAddNumbers).RegisterTo(registry))

	storage := NewMemoryStorage()
	ctx := context.Background()
	require.NoError(t, storage.Save(ctx, &Execution{
		ID:                "shared-name",
		DefinitionName:    "calc-b", // a different saga recorded under this name
		DefinitionVersion: 1,
		Status:            StatusRunning,
		CreatedAt:         time.Now(),
		UpdatedAt:         time.Now(),
	}))

	executor := NewExecutor(storage,
		WithRegistry(registry),
		WithRecoveryScope("node/runner-a"),
	)
	err := executor.Start("calc-a").Input("a", 2).Input("b", 3).
		WithID("shared-name").Execute(ctx)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "calc-b")
	assert.Empty(t, ctrl.addCalls, "and no action runs against the other saga's record")

	unchanged, getErr := storage.Get(ctx, "shared-name")
	require.NoError(t, getErr)
	assert.Empty(t, unchanged.RecoveryScope,
		"a rejected re-entry must not adopt the other saga's legacy record")
}
