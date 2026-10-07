package saga

import (
	"context"
	"fmt"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"sync"
)

// Definition describes a saga's structure - its actions and their dependencies.
// Definitions are stateless and registered at application startup.
type Definition struct {
	// Name uniquely identifies this saga definition.
	Name string

	// Version is incremented for breaking changes. Defaults to 1.
	Version int

	// ResumesFrom lists the older versions whose executions this definition
	// has been declared safe to resume. Anything else recorded at another
	// version is refused rather than driven against a graph it never ran.
	ResumesFrom []int

	// Actions in this saga, keyed by action name.
	Actions map[string]*ActionNode

	// executionOrder is the topologically sorted order for execution.
	executionOrder []string

	// dependencies is stored to inject into context during execution.
	dependencies []any
}

// ExecutionOrder returns the computed execution order for the saga's actions.
func (d *Definition) ExecutionOrder() []string {
	return d.executionOrder
}

// ActionNode describes a single action within a saga definition.
type ActionNode struct {
	// Name is the unique name of this action within the saga.
	Name string

	// Action is the stateless action implementation.
	Action Action

	// InputKeys are the saga keys this action reads from.
	InputKeys []string

	// OutputKeys are the saga keys this action writes to.
	OutputKeys []string

	// Dependencies are action names that must complete before this action.
	// Computed from InputKeys and other actions' OutputKeys.
	Dependencies []string
}

// Builder provides a fluent API for defining sagas.
type Builder struct {
	name         string
	version      int
	resumesFrom  []int
	resumesSet   bool
	actions      []*pendingAction
	dependencies []any
	err          error
}

// pendingAction holds action info during builder construction.
type pendingAction struct {
	name     string
	execute  any
	undo     any
	typedAct *typedAction
}

// Define starts building a new saga definition with the given name.
func Define(name string) *Builder {
	return &Builder{
		name:    name,
		version: 1,
	}
}

// Version sets the definition version (defaults to 1). Any version above 1
// must also say which older versions it can resume, via ResumesFrom.
func (b *Builder) Version(v int) *Builder {
	b.version = v
	return b
}

// ResumesFrom declares which older versions' executions this definition can
// safely resume. Calling it with no arguments is the explicit way to say none:
// executions recorded at an older version will be refused, not resumed.
//
// Listing a version is a claim that every in-flight execution recorded at it,
// whether still running forward or partway through undo, behaves correctly
// against this definition's current action graph. See the package docs for
// which changes keep that claim true.
func (b *Builder) ResumesFrom(versions ...int) *Builder {
	b.resumesFrom = versions
	b.resumesSet = true
	return b
}

// Using adds dependencies that will be injected into the context during execution.
// These can be retrieved using Get[T](ctx) within action functions.
// Note: Dependencies are keyed by their concrete type. To key by an interface type,
// use UsingAs[T] instead.
func (b *Builder) Using(deps ...any) *Builder {
	b.dependencies = append(b.dependencies, deps...)
	return b
}

// typedDep wraps a dependency with its intended key type.
type typedDep struct {
	keyType string
	value   any
}

// UsingAs adds a dependency keyed by type T, allowing retrieval via Get[T](ctx).
// This is useful for injecting implementations that should be retrieved by interface type.
// For example: UsingAs[MyInterface](b, impl) allows Get[MyInterface](ctx).
func UsingAs[T any](b *Builder, dep T) *Builder {
	var zero T
	b.dependencies = append(b.dependencies, typedDep{
		keyType: fmt.Sprintf("%T", zero),
		value:   dep,
	})
	return b
}

// ActionBuilder provides a fluent API for defining a single action.
type ActionBuilder struct {
	builder *Builder
	pending *pendingAction
}

// Action adds an action to the saga using a typed execute function.
// The function signature must be: func(ctx context.Context, in InType) (OutType, error)
//
// Can be called two ways:
//   - Action(GetBread) - name derived from function name ("getbread")
//   - Action("custom-name", GetBread) - explicit name
func (b *Builder) Action(args ...any) *ActionBuilder {
	var name string
	var execute any

	switch len(args) {
	case 1:
		// Action(fn) - derive name from function
		execute = args[0]
		t := reflect.TypeOf(execute)
		if t == nil || t.Kind() != reflect.Func {
			b.err = fmt.Errorf("Action argument must be a function")
			return &ActionBuilder{builder: b, pending: &pendingAction{}}
		}
		name = funcName(execute)
	case 2:
		// Action("name", fn) - explicit name
		var ok bool
		name, ok = args[0].(string)
		if !ok {
			b.err = fmt.Errorf("Action first argument must be a string name or a function")
			return &ActionBuilder{builder: b, pending: &pendingAction{}}
		}
		execute = args[1]
		t := reflect.TypeOf(execute)
		if t == nil || t.Kind() != reflect.Func {
			b.err = fmt.Errorf("Action second argument must be a function")
			return &ActionBuilder{builder: b, pending: &pendingAction{}}
		}
	default:
		b.err = fmt.Errorf("Action requires 1 or 2 arguments")
		return &ActionBuilder{builder: b, pending: &pendingAction{}}
	}

	pending := &pendingAction{
		name:    name,
		execute: execute,
	}
	b.actions = append(b.actions, pending)
	return &ActionBuilder{
		builder: b,
		pending: pending,
	}
}

// funcName extracts a clean function name for use as an action name.
// "github.com/foo/bar.GetBread" -> "get-bread"
// "github.com/foo/bar.(*Type).Method" -> "method"
func funcName(fn any) string {
	ptr := reflect.ValueOf(fn).Pointer()
	fullName := runtime.FuncForPC(ptr).Name()

	// Get just the function/method name after the last dot
	name := fullName
	if idx := strings.LastIndex(fullName, "."); idx >= 0 {
		name = fullName[idx+1:]
	}

	// Handle method receivers: "(*Type).Method" or "Type.Method"
	if idx := strings.LastIndex(name, ")"); idx >= 0 {
		name = name[idx+1:]
		name = strings.TrimPrefix(name, ".")
	}

	// Strip "-fm" suffix that Go adds for method values
	name = strings.TrimSuffix(name, "-fm")

	return camelToKebab(name)
}

// camelToKebab converts CamelCase to kebab-case.
// "GetBread" -> "get-bread"
// "AddHTTPHeader" -> "add-h-t-t-p-header" (consecutive caps get individual hyphens)
func camelToKebab(s string) string {
	var result strings.Builder
	for i, r := range s {
		if i > 0 && r >= 'A' && r <= 'Z' {
			result.WriteByte('-')
		}
		result.WriteRune(r)
	}
	return strings.ToLower(result.String())
}

// Undo sets the undo function for the action.
// The function signature must be: func(ctx context.Context, in InType, out OutType) error
func (ab *ActionBuilder) Undo(undo any) *Builder {
	ab.pending.undo = undo
	return ab.builder
}

// Register validates and registers the saga definition with the global registry.
// Returns an error if validation fails (cycles, duplicate outputs, type mismatches).
func (b *Builder) Register() error {
	return b.RegisterTo(globalRegistry)
}

// RegisterTo validates and registers the saga definition with the given registry.
// Useful for testing to avoid global state.
func (b *Builder) RegisterTo(r *Registry) error {
	if b.err != nil {
		return b.err
	}

	def, err := b.Build()
	if err != nil {
		return err
	}

	return r.Register(def)
}

// Build constructs and validates the Definition without registering it.
// Useful for testing.
func (b *Builder) Build() (*Definition, error) {
	if b.err != nil {
		return nil, b.err
	}

	resumesFrom, err := b.validateVersioning()
	if err != nil {
		return nil, err
	}

	def := &Definition{
		Name:         b.name,
		Version:      b.version,
		ResumesFrom:  resumesFrom,
		Actions:      make(map[string]*ActionNode),
		dependencies: b.dependencies,
	}

	// Track which keys are produced by which actions
	outputProducers := make(map[string]string)

	// First pass: wrap all actions and collect their input/output keys
	for _, pending := range b.actions {
		if pending.undo == nil {
			return nil, fmt.Errorf("action %q has no undo function", pending.name)
		}

		typed, err := wrapTypedAction(pending.name, pending.execute, pending.undo)
		if err != nil {
			return nil, fmt.Errorf("action %q: %w", pending.name, err)
		}
		pending.typedAct = typed

		// Check for duplicate action names
		if _, exists := def.Actions[pending.name]; exists {
			return nil, fmt.Errorf("duplicate action name %q", pending.name)
		}

		// Check for duplicate output keys
		for _, key := range typed.outputKeys() {
			if producer, exists := outputProducers[key]; exists {
				return nil, fmt.Errorf("output key %q produced by both %q and %q",
					key, producer, pending.name)
			}
			outputProducers[key] = pending.name
		}

		def.Actions[pending.name] = &ActionNode{
			Name:       pending.name,
			Action:     typed,
			InputKeys:  typed.inputKeys(),
			OutputKeys: typed.outputKeys(),
		}
	}

	// Second pass: compute dependencies from input/output keys
	for _, node := range def.Actions {
		deps := make(map[string]bool)
		for _, inputKey := range node.InputKeys {
			if producer, exists := outputProducers[inputKey]; exists {
				if producer != node.Name {
					deps[producer] = true
				}
			}
			// If no producer exists, it must come from InitialInputs (validated at runtime)
		}
		for dep := range deps {
			node.Dependencies = append(node.Dependencies, dep)
		}
	}

	// Third pass: topological sort to get execution order and detect cycles
	order, err := topologicalSort(def.Actions)
	if err != nil {
		return nil, err
	}
	def.executionOrder = order

	return def, nil
}

// topologicalSort returns actions in dependency order, or an error if there's a cycle.
// Actions at the same dependency level are sorted alphabetically for determinism.
func topologicalSort(actions map[string]*ActionNode) ([]string, error) {
	// Collect sorted action names for deterministic iteration
	actionNames := make([]string, 0, len(actions))
	for name := range actions {
		actionNames = append(actionNames, name)
	}
	slices.Sort(actionNames)

	// Kahn's algorithm - compute in-degree (number of dependencies) for each node
	inDegree := make(map[string]int)
	for _, node := range actions {
		inDegree[node.Name] = len(node.Dependencies)
	}

	// Find all nodes with no dependencies (sorted)
	var queue []string
	for _, name := range actionNames {
		if inDegree[name] == 0 {
			queue = append(queue, name)
		}
	}

	var order []string
	for len(queue) > 0 {
		// Take from queue
		name := queue[0]
		queue = queue[1:]
		order = append(order, name)

		// Reduce in-degree of dependents, collect newly ready ones sorted
		var ready []string
		for _, depName := range actionNames {
			node := actions[depName]
			for _, dep := range node.Dependencies {
				if dep == name {
					inDegree[depName]--
					if inDegree[depName] == 0 {
						ready = append(ready, depName)
					}
				}
			}
		}
		queue = append(queue, ready...)
	}

	if len(order) != len(actions) {
		return nil, fmt.Errorf("dependency cycle detected in saga definition")
	}

	return order, nil
}

// validateVersioning makes a version bump carry an explicit compatibility
// decision. Without it, bumping the version would silently mean "refuse every
// older execution", which is a fine answer but not one to reach by omission.
func (b *Builder) validateVersioning() ([]int, error) {
	if b.version < 1 {
		return nil, fmt.Errorf("saga %q: version must be at least 1, got %d", b.name, b.version)
	}
	if b.version > 1 && !b.resumesSet {
		return nil, fmt.Errorf("saga %q: version %d must declare which older versions it can resume "+
			"(ResumesFrom(), with no arguments, refuses them all)", b.name, b.version)
	}

	var out []int
	for _, v := range b.resumesFrom {
		if v < 1 || v >= b.version {
			return nil, fmt.Errorf("saga %q: ResumesFrom(%d) must name a version below the current version %d",
				b.name, v, b.version)
		}
		if !slices.Contains(out, v) {
			out = append(out, v)
		}
	}
	slices.Sort(out)
	return out, nil
}

// CanResume reports whether an execution recorded at version v may be driven
// by this definition.
func (d *Definition) CanResume(v int) bool {
	return v == d.Version || slices.Contains(d.ResumesFrom, v)
}

// Registry holds registered saga definitions.
type Registry struct {
	mu          sync.RWMutex
	definitions map[string]*Definition
}

// NewRegistry creates a new empty registry.
// Useful for testing to avoid global state.
func NewRegistry() *Registry {
	return &Registry{
		definitions: make(map[string]*Definition),
	}
}

// globalRegistry is the default registry used by Define().Register().
var globalRegistry = NewRegistry()

// Register adds a definition to the registry.
func (r *Registry) Register(def *Definition) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.definitions[def.Name]; exists {
		return fmt.Errorf("saga %q already registered", def.Name)
	}

	r.definitions[def.Name] = def
	return nil
}

// Get retrieves a definition by name.
func (r *Registry) Get(name string) (*Definition, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	def, ok := r.definitions[name]
	return def, ok
}

// Definitions returns every registered definition, sorted by name.
func (r *Registry) Definitions() []*Definition {
	r.mu.RLock()
	defer r.mu.RUnlock()

	defs := make([]*Definition, 0, len(r.definitions))
	for _, def := range r.definitions {
		defs = append(defs, def)
	}
	slices.SortFunc(defs, func(a, b *Definition) int { return strings.Compare(a.Name, b.Name) })
	return defs
}

// GetDefinition retrieves a saga definition from the global registry.
func GetDefinition(name string) (*Definition, bool) {
	return globalRegistry.Get(name)
}

// contextKey is used to store dependencies in context.
type contextKey struct {
	typ string
}

// injectDependencies adds all dependencies to the context.
func injectDependencies(ctx context.Context, deps []any) context.Context {
	for _, dep := range deps {
		if td, ok := dep.(typedDep); ok {
			// Use the explicitly specified key type
			ctx = context.WithValue(ctx, contextKey{typ: td.keyType}, td.value)
		} else {
			// Use the concrete type as key
			ctx = context.WithValue(ctx, contextKey{typ: fmt.Sprintf("%T", dep)}, dep)
		}
	}
	return ctx
}

// Get retrieves a dependency of type T from the context.
// Panics if the dependency is not found.
func Get[T any](ctx context.Context) T {
	var zero T
	key := contextKey{typ: fmt.Sprintf("%T", zero)}
	val := ctx.Value(key)
	if val == nil {
		panic(fmt.Sprintf("saga dependency %T not found in context", zero))
	}
	return val.(T)
}

// TryGet retrieves a dependency of type T from the context.
// Returns the zero value and false if not found.
func TryGet[T any](ctx context.Context) (T, bool) {
	var zero T
	key := contextKey{typ: fmt.Sprintf("%T", zero)}
	val := ctx.Value(key)
	if val == nil {
		return zero, false
	}
	return val.(T), true
}
