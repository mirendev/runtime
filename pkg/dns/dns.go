package dns

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/miekg/dns"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/rpc/stream"

	compute_v1alpha "miren.dev/runtime/api/compute/compute_v1alpha"
	core_v1alpha "miren.dev/runtime/api/core/core_v1alpha"
	entityserver_v1alpha "miren.dev/runtime/api/entityserver/entityserver_v1alpha"
)

type Server struct {
	*dns.Server
	client       *dns.Client
	upstreams    []string
	entityClient *entityserver_v1alpha.EntityAccessClient
	log          *slog.Logger

	mu              sync.RWMutex
	ipToApp         map[string]string              // source IP → app name
	ipToService     map[string]string              // IP → service name (for PTR lookups)
	appServiceToIPs map[string]map[string][]string // app name → service name → []IPs
	entityToIP      map[string]string              // entity ID → IP address

	watchCtx    context.Context
	watchCancel context.CancelFunc
	watchWg     sync.WaitGroup
}

// New creates a new DNS forwarding server
func New(addr string, entityClient *entityserver_v1alpha.EntityAccessClient, log *slog.Logger) (*Server, error) {
	cc, err := dns.ClientConfigFromFile("/etc/resolv.conf")
	if err != nil {
		return nil, fmt.Errorf("reading resolv.conf: %w", err)
	}

	upstreams := cc.Servers

	if len(upstreams) == 0 {
		return nil, fmt.Errorf("no nameservers found in /etc/resolv.conf")
	}

	s := &Server{
		Server: &dns.Server{
			Addr: addr,
			Net:  "udp",
		},
		client:          &dns.Client{},
		upstreams:       upstreams,
		entityClient:    entityClient,
		log:             log.With("module", "dns"),
		ipToApp:         make(map[string]string),
		ipToService:     make(map[string]string),
		appServiceToIPs: make(map[string]map[string][]string),
		entityToIP:      make(map[string]string),
	}

	s.Handler = dns.HandlerFunc(s.handleRequest)
	return s, nil
}

func (s *Server) handleRequest(w dns.ResponseWriter, r *dns.Msg) {
	// Check if this is an app.miren query
	if len(r.Question) > 0 {
		question := r.Question[0]
		qname := strings.ToLower(question.Name)

		// Handle TXT query for app.miren (service discovery)
		if qname == "app.miren." && question.Qtype == dns.TypeTXT {
			s.handleServiceListQuery(w, r)
			return
		}

		// Handle queries for *.app.miren pattern
		if strings.HasSuffix(qname, ".app.miren.") {
			switch question.Qtype {
			case dns.TypeA:
				s.handleAppMirenQuery(w, r, qname)
				return
			case dns.TypeAAAA:
				// Return empty response for IPv6 queries
				response := new(dns.Msg)
				response.SetReply(r)
				response.RecursionAvailable = true
				response.Authoritative = true
				w.WriteMsg(response)
				return
			default:
				// Return empty for any other query type on app.miren domains
				response := new(dns.Msg)
				response.SetReply(r)
				response.RecursionAvailable = true
				response.Authoritative = true
				w.WriteMsg(response)
				return
			}
		}

		// Handle PTR queries for .in-addr.arpa (reverse DNS)
		if strings.HasSuffix(qname, ".in-addr.arpa.") && question.Qtype == dns.TypePTR {
			s.handlePTRQuery(w, r, qname)
			return
		}
	}

	// Not an app.miren query, forward to upstream
	var response *dns.Msg
	var err error

	for _, upstream := range s.upstreams {
		// Ensure upstream address has port 53
		upstream = net.JoinHostPort(upstream, "53")
		response, _, err = s.client.Exchange(r, upstream)
		if err == nil && response != nil {
			break
		}
	}

	if err != nil || response == nil {
		// If all upstreams failed, return SERVFAIL
		response = new(dns.Msg)
		response.SetReply(r)
		response.Rcode = dns.RcodeServerFailure
		w.WriteMsg(response)
		return
	}

	// Ensure the response has the correct id and is marked as a response
	response.Id = r.Id
	response.RecursionAvailable = true
	response.Response = true

	w.WriteMsg(response)
}

func (s *Server) handleServiceListQuery(w dns.ResponseWriter, r *dns.Msg) {
	response := new(dns.Msg)
	response.SetReply(r)
	response.RecursionAvailable = true
	response.Authoritative = true

	// Get source IP from request
	remoteAddr := w.RemoteAddr()
	var sourceIP string
	switch addr := remoteAddr.(type) {
	case *net.UDPAddr:
		sourceIP = addr.IP.String()
	case *net.TCPAddr:
		sourceIP = addr.IP.String()
	default:
		s.log.Warn("unknown remote address type", "type", fmt.Sprintf("%T", remoteAddr))
		w.WriteMsg(response)
		return
	}

	// Look up which app this source IP belongs to
	s.mu.RLock()
	appName, found := s.ipToApp[sourceIP]
	if !found {
		s.mu.RUnlock()
		// Source IP not from any known sandbox, return empty response
		s.log.Debug("service list query from unknown IP", "ip", sourceIP)
		w.WriteMsg(response)
		return
	}

	// Get all services for this app
	var services []string
	if serviceMap, ok := s.appServiceToIPs[appName]; ok {
		for service := range serviceMap {
			services = append(services, service)
		}
	}
	s.mu.RUnlock()

	sort.Strings(services)

	// Return all services in a single TXT record, space-separated
	if len(services) > 0 {
		rr := &dns.TXT{
			Hdr: dns.RR_Header{
				Name:   r.Question[0].Name,
				Rrtype: dns.TypeTXT,
				Class:  dns.ClassINET,
				Ttl:    30,
			},
			Txt: []string{strings.Join(services, " ")},
		}
		response.Answer = append(response.Answer, rr)
	}

	s.log.Debug("resolved service list query", "app", appName, "source_ip", sourceIP, "services", services)
	w.WriteMsg(response)
}

func (s *Server) handlePTRQuery(w dns.ResponseWriter, r *dns.Msg, qname string) {
	response := new(dns.Msg)
	response.SetReply(r)
	response.RecursionAvailable = true

	// Parse IP from reversed .in-addr.arpa format
	// e.g., "5.0.10.10.in-addr.arpa." → "10.10.0.5"
	parts := strings.Split(qname, ".")
	if len(parts) < 6 {
		// Invalid format, forward to upstream
		s.forwardToUpstream(w, r)
		return
	}

	// Reverse the first 4 octets
	ip := fmt.Sprintf("%s.%s.%s.%s", parts[3], parts[2], parts[1], parts[0])

	// Get source IP from request to determine requesting app
	remoteAddr := w.RemoteAddr()
	var sourceIP string
	switch addr := remoteAddr.(type) {
	case *net.UDPAddr:
		sourceIP = addr.IP.String()
	case *net.TCPAddr:
		sourceIP = addr.IP.String()
	default:
		s.forwardToUpstream(w, r)
		return
	}

	// Look up which app this source IP belongs to
	s.mu.RLock()
	sourceAppName, foundSource := s.ipToApp[sourceIP]
	if !foundSource {
		s.mu.RUnlock()
		// Source IP not from any known sandbox, forward to upstream
		s.forwardToUpstream(w, r)
		return
	}

	// Look up which app the queried IP belongs to
	targetAppName, foundTarget := s.ipToApp[ip]
	if !foundTarget {
		s.mu.RUnlock()
		// Queried IP not tracked, forward to upstream
		s.forwardToUpstream(w, r)
		return
	}

	// App-scoped security: only return PTR if both IPs belong to same app
	if sourceAppName != targetAppName {
		s.mu.RUnlock()
		s.forwardToUpstream(w, r)
		return
	}

	// Look up service name for the queried IP
	serviceName, found := s.ipToService[ip]
	s.mu.RUnlock()

	if !found {
		// No service mapping found, forward to upstream
		s.forwardToUpstream(w, r)
		return
	}

	// Build PTR record pointing to service.app.miren.
	response.Authoritative = true
	ptrRecord := &dns.PTR{
		Hdr: dns.RR_Header{
			Name:   r.Question[0].Name,
			Rrtype: dns.TypePTR,
			Class:  dns.ClassINET,
			Ttl:    30,
		},
		Ptr: fmt.Sprintf("%s.app.miren.", serviceName),
	}
	response.Answer = append(response.Answer, ptrRecord)

	s.log.Debug("resolved PTR query", "ip", ip, "service", serviceName, "app", sourceAppName, "source_ip", sourceIP)
	w.WriteMsg(response)
}

func (s *Server) forwardToUpstream(w dns.ResponseWriter, r *dns.Msg) {
	var response *dns.Msg
	var err error

	for _, upstream := range s.upstreams {
		upstream = net.JoinHostPort(upstream, "53")
		response, _, err = s.client.Exchange(r, upstream)
		if err == nil && response != nil {
			break
		}
	}

	if err != nil || response == nil {
		response = new(dns.Msg)
		response.SetReply(r)
		response.Rcode = dns.RcodeServerFailure
		w.WriteMsg(response)
		return
	}

	response.Id = r.Id
	response.RecursionAvailable = true
	response.Response = true
	w.WriteMsg(response)
}

func (s *Server) handleAppMirenQuery(w dns.ResponseWriter, r *dns.Msg, qname string) {
	response := new(dns.Msg)
	response.SetReply(r)
	response.RecursionAvailable = true
	response.Authoritative = true

	// Extract service name from query (e.g., "web" from "web.app.miren.")
	// qname format: "service-name.app.miren."
	parts := strings.Split(qname, ".")
	if len(parts) < 3 {
		// Invalid format, return empty response
		w.WriteMsg(response)
		return
	}
	serviceName := parts[0]

	// Get source IP from request
	remoteAddr := w.RemoteAddr()
	var sourceIP string
	switch addr := remoteAddr.(type) {
	case *net.UDPAddr:
		sourceIP = addr.IP.String()
	case *net.TCPAddr:
		sourceIP = addr.IP.String()
	default:
		s.log.Warn("unknown remote address type", "type", fmt.Sprintf("%T", remoteAddr))
		w.WriteMsg(response)
		return
	}

	// Look up which app this source IP belongs to
	s.mu.RLock()
	appName, found := s.ipToApp[sourceIP]
	if !found {
		s.mu.RUnlock()
		// Source IP not from any known sandbox, return empty response
		s.log.Debug("dns query from unknown IP", "ip", sourceIP, "query", qname)
		w.WriteMsg(response)
		return
	}

	// Get IPs for this app+service
	var ips []string
	if serviceMap, ok := s.appServiceToIPs[appName]; ok {
		ips = serviceMap[serviceName]
	}
	s.mu.RUnlock()

	if len(ips) == 0 {
		// No sandboxes found for this app+service
		s.log.Debug("no sandboxes found for app+service", "app", appName, "service", serviceName, "source_ip", sourceIP)
		w.WriteMsg(response)
		return
	}

	// Build A records for all matching sandbox IPs
	for _, ip := range ips {
		parsedIP, err := netip.ParseAddr(ip)
		if err != nil {
			s.log.Warn("invalid IP address in mapping", "ip", ip, "app", appName, "service", serviceName, "error", err)
			continue
		}

		// Only return A records for IPv4 addresses
		if parsedIP.Is4() {
			rr := &dns.A{
				Hdr: dns.RR_Header{
					Name:   r.Question[0].Name,
					Rrtype: dns.TypeA,
					Class:  dns.ClassINET,
					Ttl:    30, // Short TTL for dynamic service discovery
				},
				A: parsedIP.AsSlice(),
			}
			response.Answer = append(response.Answer, rr)
		}
	}

	s.log.Debug("resolved app.miren query", "service", serviceName, "app", appName, "source_ip", sourceIP, "result_count", len(response.Answer))
	w.WriteMsg(response)
}

// Watch starts watching sandbox entities and maintains in-memory DNS mappings
func (s *Server) Watch(ctx context.Context) error {
	// First, recover existing sandboxes to populate initial state
	if err := s.recoverSandboxes(ctx); err != nil {
		return fmt.Errorf("failed to recover sandboxes: %w", err)
	}

	// Create a child context that we can cancel independently
	s.watchCtx, s.watchCancel = context.WithCancel(ctx)

	// Start watching for sandbox changes
	s.watchWg.Add(1)
	go func() {
		defer s.watchWg.Done()
		s.watchSandboxes(s.watchCtx)
	}()

	return nil
}

func (s *Server) watchSandboxes(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			s.log.Info("sandbox watch stopped due to context cancellation")
			return
		}

		index := entity.Ref(entity.EntityKind, compute_v1alpha.KindSandbox)
		_, err := s.entityClient.WatchIndex(ctx, index, stream.Callback(func(op *entityserver_v1alpha.EntityOp) error {
			if op == nil {
				return nil
			}

			switch op.OperationType() {
			case entityserver_v1alpha.EntityOperationCreate, entityserver_v1alpha.EntityOperationUpdate:
				if op.Entity() == nil {
					return nil
				}
				en := op.Entity().Entity()
				var sb compute_v1alpha.Sandbox
				sb.Decode(en)
				s.handleSandboxUpdate(ctx, &sb, en)

			case entityserver_v1alpha.EntityOperationDelete:
				// For DELETE, entity data is nil but we have the ID
				entityID := op.EntityId()
				s.handleSandboxDeleteByID(entityID)
			}

			return nil
		}))

		if err != nil {
			if ctx.Err() != nil {
				s.log.Info("sandbox watch stopped due to context cancellation")
				return
			}
			s.log.Error("sandbox watch ended with error, will restart", "error", err)
			time.Sleep(5 * time.Second)
			continue
		}

		s.log.Warn("sandbox watch ended unexpectedly, restarting")
		time.Sleep(5 * time.Second)
	}
}

func (s *Server) handleSandboxUpdate(ctx context.Context, sb *compute_v1alpha.Sandbox, en *entity.Entity) {
	s.mu.Lock()
	_, tracked := s.entityToIP[sb.ID.String()]
	s.mu.Unlock()

	if tracked {
		// Already tracked, skip
		return
	}

	// Get sandbox IP
	if len(sb.Network) == 0 {
		return
	}

	// Extract IP from address (may be in CIDR format like "10.0.0.5/24")
	ipAddr := sb.Network[0].Address
	if strings.Contains(ipAddr, "/") {
		ipAddr = strings.Split(ipAddr, "/")[0]
	}

	// Get service label from metadata
	var md core_v1alpha.Metadata
	md.Decode(en)

	service, _ := md.Labels.Get("service")
	if service == "" {
		return // Skip sandboxes without service label
	}

	// Get app version to determine app name
	verResp, err := s.entityClient.Get(ctx, sb.Spec.Version.String())
	if err != nil {
		s.log.Error("failed to get version for sandbox", "sandbox", sb.ID, "version", sb.Spec.Version, "error", err)
		return
	}

	var appVer core_v1alpha.AppVersion
	appVer.Decode(verResp.Entity().Entity())

	// Get app entity to get app name from metadata
	appResp, err := s.entityClient.Get(ctx, appVer.App.String())
	if err != nil {
		s.log.Error("failed to get app for sandbox", "sandbox", sb.ID, "app", appVer.App, "error", err)
		return
	}

	var appMD core_v1alpha.Metadata
	appMD.Decode(appResp.Entity().Entity())

	appName := appMD.Name

	s.log.Info("derived sandbox app and service for DNS mapping",
		"sandbox", sb.ID,
		"app", appName,
		"service", service,
		"ver", sb.Spec.Version.String(),
		"app-id", appVer.App.String(),
	)

	// Update in-memory mappings
	s.mu.Lock()
	defer s.mu.Unlock()

	// Track entity ID -> IP mapping for DELETE operations
	s.entityToIP[sb.ID.String()] = ipAddr

	// Update ipToApp mapping
	s.ipToApp[ipAddr] = appName

	// Update ipToService mapping for PTR queries
	s.ipToService[ipAddr] = service

	// Update appServiceToIPs mapping
	if s.appServiceToIPs[appName] == nil {
		s.appServiceToIPs[appName] = make(map[string][]string)
	}

	// Check if IP already exists in the list for this app+service
	existingIPs := s.appServiceToIPs[appName][service]
	found := false
	for _, existingIP := range existingIPs {
		if existingIP == ipAddr {
			found = true
			break
		}
	}

	if !found {
		s.appServiceToIPs[appName][service] = append(existingIPs, ipAddr)
		s.log.Info("added sandbox to DNS mapping", "sandbox", sb.ID, "app", appName, "service", service, "ip", ipAddr)
	}
}

func (s *Server) handleSandboxDeleteByID(entityID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Look up the IP address from entity ID
	ipAddr, found := s.entityToIP[entityID]
	if !found {
		// Not tracked, nothing to do
		return
	}

	// Remove from entityToIP map
	delete(s.entityToIP, entityID)

	// Get app name from ipToApp mapping
	appName, found := s.ipToApp[ipAddr]
	if !found {
		return // Inconsistent state, but continue
	}

	// Remove from ipToApp
	delete(s.ipToApp, ipAddr)

	// Remove from ipToService
	delete(s.ipToService, ipAddr)

	// Remove from appServiceToIPs - need to find and remove IP from all services
	if serviceMap, ok := s.appServiceToIPs[appName]; ok {
		for service, ips := range serviceMap {
			for i, ip := range ips {
				if ip == ipAddr {
					// Remove this IP from the slice
					s.appServiceToIPs[appName][service] = append(ips[:i], ips[i+1:]...)
					s.log.Info("removed sandbox from DNS mapping", "entity_id", entityID, "app", appName, "service", service, "ip", ipAddr)
					break
				}
			}

			// Clean up empty service entries
			if len(s.appServiceToIPs[appName][service]) == 0 {
				delete(s.appServiceToIPs[appName], service)
			}
		}

		// Clean up empty app entries
		if len(s.appServiceToIPs[appName]) == 0 {
			delete(s.appServiceToIPs, appName)
		}
	}
}

func (s *Server) recoverSandboxes(ctx context.Context) error {
	resp, err := s.entityClient.List(ctx, entity.Ref(entity.EntityKind, compute_v1alpha.KindSandbox))
	if err != nil {
		return fmt.Errorf("failed to list sandboxes: %w", err)
	}

	s.log.Info("recovering sandboxes on startup", "total_sandboxes", len(resp.Values()))

	recoveredCount := 0
	for _, ent := range resp.Values() {
		var sb compute_v1alpha.Sandbox
		sb.Decode(ent.Entity())

		// Only recover RUNNING sandboxes
		if sb.Status != compute_v1alpha.RUNNING {
			continue
		}

		// Process the sandbox to add to mappings
		s.handleSandboxUpdate(ctx, &sb, ent.Entity())
		recoveredCount++
	}

	s.log.Info("sandbox recovery complete", "recovered_count", recoveredCount)
	return nil
}

// ListenAndServe starts the DNS server
func (s *Server) ListenAndServe() error {
	return s.Server.ListenAndServe()
}

// Shutdown gracefully shuts down the server
func (s *Server) Shutdown() error {
	// Cancel the watch context to stop the watcher goroutine
	if s.watchCancel != nil {
		s.watchCancel()
	}

	// Wait for the watcher goroutine to finish
	s.watchWg.Wait()

	// Shutdown the DNS server
	return s.Server.Shutdown()
}
