package portreg

import (
	"context"
	"log/slog"
	"sync"
)

type PortChecker interface {
	CheckPort(ctx context.Context, log *slog.Logger, address string, port int) (bool, error)
}

type Registry struct {
	mu       sync.Mutex
	checkers map[string]PortChecker
}

func (r *Registry) Register(name string, checker PortChecker) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.checkers == nil {
		r.checkers = make(map[string]PortChecker)
	}

	r.checkers[name] = checker
}

func (r *Registry) Get(name string) PortChecker {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.checkers[name]
}

var defRegistry = &Registry{}

func Register(name string, checker PortChecker) string {
	defRegistry.Register(name, checker)
	return name
}

func Get(name string) PortChecker {
	return defRegistry.Get(name)
}
