package observability

import (
	"log/slog"
	"net/netip"
	"sync"
)

type PortStatus string

const (
	PortStatusBound   PortStatus = "bound"
	PortStatusUnbound PortStatus = "unbound"
	PortStatusActive  PortStatus = "active"
)

type BoundPort struct {
	Addr netip.Addr
	Port int
}

type EntityStatus struct {
	id         string
	mu         sync.Mutex
	boundPorts map[BoundPort]struct{}
}

func (e *EntityStatus) Id() string {
	return e.id
}

type StatusMonitor struct {
	Log *slog.Logger

	mu       sync.Mutex
	entities map[string]*EntityStatus
}

func (s *StatusMonitor) SetPortStatus(entity string, port BoundPort, status PortStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.entities == nil {
		s.entities = make(map[string]*EntityStatus)
	}

	es, ok := s.entities[entity]
	if !ok {
		es = &EntityStatus{
			boundPorts: make(map[BoundPort]struct{}),
		}
		s.entities[entity] = es
	}

	es.mu.Lock()
	defer es.mu.Unlock()

	switch status {
	case PortStatusBound:
		es.boundPorts[port] = struct{}{}
	case PortStatusUnbound:
		delete(es.boundPorts, port)
	}
}

func (s *StatusMonitor) FindBoundPort(bp BoundPort) ([]*EntityStatus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var res []*EntityStatus

	for _, es := range s.entities {
		es.mu.Lock()
		_, ok := es.boundPorts[bp]
		es.mu.Unlock()

		if ok {
			res = append(res, es)
		}
	}

	return res, nil
}

func (s *StatusMonitor) EntityBoundPorts(entity string) ([]BoundPort, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	es, ok := s.entities[entity]
	if !ok {
		return nil, nil
	}

	es.mu.Lock()
	defer es.mu.Unlock()

	var res []BoundPort
	for bp := range es.boundPorts {
		res = append(res, bp)
	}

	return res, nil
}
