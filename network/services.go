package network

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"slices"
	"sync"

	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/dns"
)

// ServiceManager handles network services (DNS, etc) for bridges
type ServiceManager struct {
	Log *slog.Logger
	EAC *entityserver_v1alpha.EntityAccessClient

	mu      sync.RWMutex
	bridges map[string]*BridgeServices
	ctx     context.Context
}

// NewServiceManager creates a new ServiceManager.
func NewServiceManager(log *slog.Logger, eac *entityserver_v1alpha.EntityAccessClient) *ServiceManager {
	return &ServiceManager{
		Log:     log,
		EAC:     eac,
		bridges: make(map[string]*BridgeServices),
	}
}

// BridgeServices holds the services running for a specific bridge
type BridgeServices struct {
	dns []*dns.Server
	ips []netip.Prefix
}

// SetupDNS ensures a DNS server is running for the given bridge
func (sm *ServiceManager) SetupDNS(ctx context.Context, bc *BridgeConfig) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	bridgeName := bc.Name

	// Check if we already have services for this bridge
	if services, exists := sm.bridges[bridgeName]; exists && services.dns != nil {
		return nil // DNS already configured
	}

	sm.Log.Info("Starting DNS services for bridge", "bridge", bridgeName)

	bs := &BridgeServices{
		ips: slices.Clone(bc.Addresses),
	}

	// Create new services entry if needed
	sm.bridges[bridgeName] = bs

	// Store context for DNS watcher
	sm.ctx = ctx

	for _, addr := range bs.ips {
		// Create and start DNS server with entity client and logger
		server, err := dns.New(net.JoinHostPort(addr.Addr().String(), "53"), sm.EAC, sm.Log)
		if err != nil {
			return fmt.Errorf("creating DNS server for bridge %s: %w", bridgeName, err)
		}

		sm.Log.Info("Binding DNS server", "bridge", bridgeName, "addr", addr.String())

		// Start DNS entity watcher if entity client is available
		if sm.EAC != nil {
			if err := server.Watch(ctx); err != nil {
				return fmt.Errorf("starting DNS watcher for bridge %s: %w", bridgeName, err)
			}
			sm.Log.Info("DNS watcher started", "bridge", bridgeName)
		}

		go func() {
			if err := server.ListenAndServe(); err != nil {
				sm.Log.Error("DNS server error", "bridge", bridgeName, "error", err)
			}
		}()

		sm.Log.Debug("DNS server started", "bridge", bridgeName, "addr", addr.String())

		sm.bridges[bridgeName].dns = append(sm.bridges[bridgeName].dns, server)
	}

	return nil
}

// shutdownBridgeUnlocked stops all services for a given bridge (caller must hold lock)
func (sm *ServiceManager) shutdownBridgeUnlocked(bridgeName string) error {
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

// ShutdownBridge stops all services for a given bridge
func (sm *ServiceManager) ShutdownBridge(bridgeName string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	return sm.shutdownBridgeUnlocked(bridgeName)
}

// ShutdownAll stops all services on all bridges
func (sm *ServiceManager) ShutdownAll() error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	var lastErr error
	for bridgeName := range sm.bridges {
		if err := sm.shutdownBridgeUnlocked(bridgeName); err != nil {
			sm.Log.Error("failed to shutdown bridge services", "bridge", bridgeName, "error", err)
			lastErr = err
		}
	}

	return lastErr
}

// AddTestDNSServer adds a DNS server to the ServiceManager for testing.
// The setup function is called with the server to populate test data.
func (sm *ServiceManager) AddTestDNSServer(t interface{ Helper() }, setup func(*dns.Server)) {
	t.Helper()
	sm.mu.Lock()
	defer sm.mu.Unlock()

	s := dns.NewTestServer()
	setup(s)
	if sm.bridges["test"] == nil {
		sm.bridges["test"] = &BridgeServices{}
	}
	sm.bridges["test"].dns = append(sm.bridges["test"].dns, s)
}

// AddSandboxMapping registers a sandbox's address with every DNS server, so the mapping
// is in place before the sandbox's container starts rather than whenever the entity
// watcher gets to it. Every server watches the whole sandbox index and LookupSandboxByIP
// searches all of them, so the mappings are cluster-wide on each server either way.
//
// The caller here is the controller that allocated the address, which makes it the
// authority on who owns it — the watcher-derived view remains as reconciliation.
func (sm *ServiceManager) AddSandboxMapping(sandboxID, ip, appName, service string) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	for _, bs := range sm.bridges {
		for _, server := range bs.dns {
			server.AddSandboxMapping(sandboxID, ip, appName, service)
		}
	}
}

// RemoveSandboxMapping withdraws a sandbox's address claim from every DNS server. If
// another sandbox has already taken the address (a recycled IP), that sandbox keeps it.
func (sm *ServiceManager) RemoveSandboxMapping(sandboxID string) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	for _, bs := range sm.bridges {
		for _, server := range bs.dns {
			server.RemoveSandboxMapping(sandboxID)
		}
	}
}

// LookupSandboxByIP searches across all bridge DNS servers for a sandbox matching the given IP.
func (sm *ServiceManager) LookupSandboxByIP(ip string) (sandboxID, appName string, ok bool) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	for _, bs := range sm.bridges {
		for _, server := range bs.dns {
			if sandboxID, appName, ok = server.LookupSandboxByIP(ip); ok {
				return sandboxID, appName, true
			}
		}
	}
	return "", "", false
}

// RefreshSandboxByIP re-derives an address's owner from the entity store on every DNS
// server, ignoring what they have cached, and returns the corrected answer. Every server
// is refreshed rather than stopping at the first hit, so a mapping that has gone stale
// cannot survive on one of them.
func (sm *ServiceManager) RefreshSandboxByIP(ip string) (sandboxID, appName string, ok bool) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	for _, bs := range sm.bridges {
		for _, server := range bs.dns {
			id, app, found := server.RefreshSandboxByIP(ip)
			if found && !ok {
				sandboxID, appName, ok = id, app, true
			}
		}
	}
	return sandboxID, appName, ok
}
