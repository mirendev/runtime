package secret

import (
	"context"
	"fmt"
	"sort"
	"sync"
)

// ClusterBackendName is the instance name of the built-in in-cluster backend.
// It is always registered, so a cluster can hold secrets without an operator
// standing up an external manager first.
const ClusterBackendName = "cluster"

// Registry maps a backend instance name to the code that resolves it. Runtime
// and build-time materialization both go through it, so neither has its own
// notion of where secrets come from.
//
// A backend *type* is the code that talks to a kind of store; a backend
// *instance* is a named, configured registration of one. A cluster might run
// the built-in "cluster" instance alongside "prod-vault" and "staging-vault",
// two instances of the same Vault type. A reference names the instance.
type Registry struct {
	mu       sync.RWMutex
	backends map[string]SecretBackend
}

// NewRegistry creates an empty registry.
func NewRegistry() *Registry {
	return &Registry{backends: make(map[string]SecretBackend)}
}

// Register adds a backend under its own name, replacing any previous
// registration for that instance.
func (r *Registry) Register(b SecretBackend) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.backends[b.Name()] = b
}

// Get returns the backend registered under name.
func (r *Registry) Get(name string) (SecretBackend, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	b, ok := r.backends[name]
	return b, ok
}

// Writable returns the backend registered under name if it supports writes.
// External managers resolve but do not accept writes, so this is how a caller
// asks "may Miren store into this one?" without asserting the type itself.
func (r *Registry) Writable(name string) (WritableBackend, error) {
	b, ok := r.Get(name)
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownBackend, name)
	}
	w, ok := b.(WritableBackend)
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrReadOnlyBackend, name)
	}
	return w, nil
}

// Names returns the registered instance names, sorted, for display.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.backends))
	for name := range r.backends {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// ResolveRef resolves a reference against the named backend, so a Registry can
// serve as a Resolver wherever the key material is local.
//
// The error deliberately names the backend and reference but never the value
// or the store's own error text.
func (r *Registry) ResolveRef(ctx context.Context, backend, ref string) (SecretValue, error) {
	b, ok := r.Get(backend)
	if !ok {
		return SecretValue{}, fmt.Errorf("%w: %q", ErrUnknownBackend, backend)
	}

	val, err := b.Resolve(ctx, ref)
	if err != nil {
		return SecretValue{}, fmt.Errorf("resolving %s:%s: %w", backend, ref, err)
	}
	return val, nil
}
