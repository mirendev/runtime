package netresolve

import (
	"net/netip"
	"sync"
)

type Resolver interface {
	LookupHost(host string) (netip.Addr, error)
}

type HostMapper interface {
	SetHost(host string, addr netip.Addr) error
}

type localResolver struct {
	mu    sync.Mutex
	hosts map[string]netip.Addr
}

var _ Resolver = (*localResolver)(nil)
var _ HostMapper = (*localResolver)(nil)

func NewLocalResolver() (Resolver, HostMapper) {
	lr := &localResolver{
		hosts: make(map[string]netip.Addr),
	}

	return lr, lr
}

func (l *localResolver) LookupHost(host string) (netip.Addr, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	addr, ok := l.hosts[host]
	if !ok {
		return netip.Addr{}, nil
	}

	return addr, nil
}

func (l *localResolver) SetHost(host string, addr netip.Addr) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.hosts[host] = addr
	return nil
}
