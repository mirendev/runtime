package discovery

import (
	"context"
	"net/http"
	"sync"
)

type BackgroundLookup struct {
	Endpoint Endpoint
	Error    error
}

type Lookup interface {
	Lookup(ctx context.Context, name string) (Endpoint, chan BackgroundLookup, error)
}

type Memory struct {
	mu        sync.Mutex
	endpoints map[string]Endpoint
}

func (m *Memory) Lookup(ctx context.Context, name string) (Endpoint, chan BackgroundLookup, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ep := m.endpoints[name]
	return ep, nil, nil
}

func (m *Memory) Register(name string, ep Endpoint) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.endpoints[name] = ep
}

type EndpointFunc func(w http.ResponseWriter, req *http.Request)

func (e EndpointFunc) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	e(w, req)
}
