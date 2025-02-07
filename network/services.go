package network

import (
	"fmt"
	"log/slog"
	"net/netip"
	"slices"
	"sync"

	"miren.dev/runtime/pkg/asm/autoreg"
	"miren.dev/runtime/pkg/dns"
)

// ServiceManager handles network services (DNS, etc) for bridges
type ServiceManager struct {
	Log *slog.Logger

	mu      sync.Mutex
	bridges map[string]*BridgeServices
}

var _ = autoreg.Register[ServiceManager]()

func (s *ServiceManager) Populated() error {
	s.bridges = make(map[string]*BridgeServices)
	return nil
}

// BridgeServices holds the services running for a specific bridge
type BridgeServices struct {
	dns []*dns.Server
	ips []netip.Prefix
}

// SetupDNS ensures a DNS server is running for the given bridge
func (sm *ServiceManager) SetupDNS(bc *BridgeConfig) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	bridgeName := bc.Name

	// Check if we already have services for this bridge
	if services, exists := sm.bridges[bridgeName]; exists && services.dns != nil {
		return nil // DNS already configured
	}

	bs := &BridgeServices{
		ips: slices.Clone(bc.Addresses),
	}

	// Create new services entry if needed
	sm.bridges[bridgeName] = bs

	for _, addr := range bs.ips {
		// Create and start DNS server
		server, err := dns.New(fmt.Sprintf("%s:53", addr.Addr().String()))
		if err != nil {
			return fmt.Errorf("creating DNS server for bridge %s: %w", bridgeName, err)
		}

		go func() {
			if err := server.ListenAndServe(); err != nil {
				// TODO: proper error handling/logging
				sm.Log.Error("DNS server error", "bridge", bridgeName, "error", err)
			}
		}()

		sm.Log.Debug("DNS server started", "bridge", bridgeName, "addr", addr.String())

		sm.bridges[bridgeName].dns = append(sm.bridges[bridgeName].dns, server)
	}

	return nil
}

// ShutdownBridge stops all services for a given bridge
func (sm *ServiceManager) ShutdownBridge(bridgeName string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	services, exists := sm.bridges[bridgeName]
	if !exists {
		return nil
	}

	for _, server := range services.dns {
		if err := server.Shutdown(); err != nil {
			return fmt.Errorf("shutting down DNS server for bridge %s: %w", bridgeName, err)
		}
	}

	delete(sm.bridges, bridgeName)
	return nil
}

// ShutdownAll stops all services on all bridges
func (sm *ServiceManager) ShutdownAll() error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	var lastErr error
	for bridgeName := range sm.bridges {
		if err := sm.ShutdownBridge(bridgeName); err != nil {
			lastErr = err
		}
	}

	return lastErr
}
