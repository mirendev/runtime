package dns

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/miekg/dns"
	"golang.org/x/net/netutil"
	"miren.dev/runtime/api/compute"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/indexwatch"

	compute_v1alpha "miren.dev/runtime/api/compute/compute_v1alpha"
	core_v1alpha "miren.dev/runtime/api/core/core_v1alpha"
	entityserver_v1alpha "miren.dev/runtime/api/entityserver/entityserver_v1alpha"
)

// sandboxMapping is one sandbox's claim on an address: the IP it holds and what that
// address answers as. The IP-keyed maps are a projection of these claims — every write
// to them goes through addSandboxMappingLocked or releaseIPLocked so an address always
// names a sandbox that still claims it.
//
// Recycled IPs mean two sandboxes can hold a claim on the same address at once (the
// outgoing one's entity outlives its container by up to an hour). seq breaks that tie:
// it increments on every registration, so the most recent claimant owns the address and
// a delete can hand it back to whoever is left. Ownership is security-relevant — the
// workload identity token server resolves a caller's identity from ipToSandbox — so the
// tie-break is deterministic rather than dependent on map iteration order (MIR-1511).
type sandboxMapping struct {
	ip      string
	app     string
	service string
	seq     uint64
}

const (
	forwardTimeout     = 1500 * time.Millisecond
	forwardLogInterval = time.Minute
	// DNS-over-TCP connections are normally brief. This leaves ample room for
	// bursts while bounding the goroutines held by buggy sandbox clients.
	maxTCPConnections = 256
)

type exchanger interface {
	ExchangeContext(context.Context, *dns.Msg, string) (*dns.Msg, time.Duration, error)
}

type forwardLogKey struct {
	upstream string
	network  string
	reason   string
}

type forwardLogState struct {
	last       time.Time
	suppressed int64
}

// forwardLogDeduper keeps a broken resolver or tenant-controlled query loop from
// flooding Warn while still periodically recording that the failure continues.
// Its key space is bounded by the server's configured upstreams and the small set
// of failure reasons below.
type forwardLogDeduper struct {
	mu       sync.Mutex
	interval time.Duration
	seen     map[forwardLogKey]*forwardLogState
	now      func() time.Time
}

func newForwardLogDeduper() *forwardLogDeduper {
	return &forwardLogDeduper{
		interval: forwardLogInterval,
		seen:     make(map[forwardLogKey]*forwardLogState),
	}
}

func (d *forwardLogDeduper) record(key forwardLogKey) (bool, int64) {
	if d == nil {
		return true, 0
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	now := time.Now()
	if d.now != nil {
		now = d.now()
	}
	if state, ok := d.seen[key]; ok {
		if now.Sub(state.last) < d.interval {
			state.suppressed++
			return false, 0
		}
		suppressed := state.suppressed
		state.last = now
		state.suppressed = 0
		return true, suppressed
	}

	d.seen[key] = &forwardLogState{last: now}
	return true, 0
}

type Server struct {
	addr          string
	boundAddr     string
	udpServer     *dns.Server
	tcpServer     *dns.Server
	udpClient     exchanger
	tcpClient     exchanger
	forwardBudget time.Duration
	forwardLogs   *forwardLogDeduper
	upstreams     []string
	entityClient  *entityserver_v1alpha.EntityAccessClient
	log           *slog.Logger
	ready         chan struct{}
	listenDone    chan struct{}
	listenStarted atomic.Bool
	stopRequested atomic.Bool
	shutdownOnce  sync.Once
	shutdownErr   error

	mu              sync.RWMutex
	ipToApp         map[string]string              // source IP → app name
	ipToSandbox     map[string]string              // source IP → sandbox entity ID
	ipToService     map[string]string              // IP → service name (for PTR lookups)
	appServiceToIPs map[string]map[string][]string // app name → service name → []IPs
	sandboxes       map[string]sandboxMapping      // sandbox entity ID → what it claims
	seq             uint64                         // registration counter; see sandboxMapping.seq

	watchCtx    context.Context
	watchCancel context.CancelFunc
	watchWg     sync.WaitGroup
	watcher     *indexwatch.Watcher
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

	return newServer(addr, upstreams, entityClient, log), nil
}

func newServer(addr string, upstreams []string, entityClient *entityserver_v1alpha.EntityAccessClient, log *slog.Logger) *Server {
	s := &Server{
		addr:            addr,
		udpClient:       &dns.Client{Net: "udp", Timeout: forwardTimeout},
		tcpClient:       &dns.Client{Net: "tcp", Timeout: forwardTimeout},
		forwardBudget:   forwardTimeout,
		forwardLogs:     newForwardLogDeduper(),
		upstreams:       upstreams,
		entityClient:    entityClient,
		log:             log.With("module", "dns"),
		ready:           make(chan struct{}),
		listenDone:      make(chan struct{}),
		ipToApp:         make(map[string]string),
		ipToSandbox:     make(map[string]string),
		ipToService:     make(map[string]string),
		appServiceToIPs: make(map[string]map[string][]string),
		sandboxes:       make(map[string]sandboxMapping),
	}

	handler := dns.HandlerFunc(s.handleRequest)
	s.udpServer = &dns.Server{Handler: handler}
	s.tcpServer = &dns.Server{Handler: handler}
	return s
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

	s.forwardToUpstream(w, r)
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
		// Try resolving via entity store lookup
		if s.resolveUnknownIP(sourceIP) {
			s.mu.RLock()
			appName = s.ipToApp[sourceIP]
			s.mu.RUnlock()
		} else {
			s.log.Debug("service list query from unknown IP", "ip", sourceIP)
			w.WriteMsg(response)
			return
		}
	} else {
		s.mu.RUnlock()
	}

	// Get all services for this app
	s.mu.RLock()
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
		// Try resolving via entity store lookup
		if s.resolveUnknownIP(sourceIP) {
			s.mu.RLock()
			sourceAppName = s.ipToApp[sourceIP]
			s.mu.RUnlock()
		} else {
			s.forwardToUpstream(w, r)
			return
		}
	} else {
		s.mu.RUnlock()
	}

	// Look up which app the queried IP belongs to
	s.mu.RLock()
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
	budget := s.forwardBudget
	if budget <= 0 {
		budget = forwardTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), budget)
	defer cancel()

	var truncatedFallback *dns.Msg
	deadline, _ := ctx.Deadline()
	for i, upstream := range s.upstreams {
		if ctx.Err() != nil {
			break
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			break
		}
		upstreamsLeft := len(s.upstreams) - i
		upstreamCtx, upstreamCancel := context.WithTimeout(ctx, remaining/time.Duration(upstreamsLeft))

		endpoint := net.JoinHostPort(upstream, "53")
		response, ok := s.exchange(upstreamCtx, r, w, s.udpClient, endpoint, "udp")
		if !ok {
			upstreamCancel()
			continue
		}

		if response.Truncated {
			if truncatedFallback == nil {
				truncatedFallback = response
			}
			s.log.Debug("DNS upstream response truncated; retrying over TCP",
				s.queryAttrs(r, w, endpoint, "tcp")...)
			response, ok = s.exchange(upstreamCtx, r, w, s.tcpClient, endpoint, "tcp")
			if !ok {
				upstreamCancel()
				continue
			}
		}

		upstreamCancel()
		s.writeForwardedResponse(w, r, response)
		return
	}

	if truncatedFallback != nil && responseWriterNetwork(w) == "udp" {
		s.writeForwardedResponse(w, r, truncatedFallback)
		return
	}

	qname, qtype := queryNameAndType(r)
	s.logForwardWarning("DNS forwarding failed; returning SERVFAIL",
		forwardLogKey{reason: "exhausted"},
		"qname", qname,
		"qtype", qtype,
		"client_ip", responseWriterIP(w),
		"upstreams", s.upstreams,
	)
	response := new(dns.Msg)
	response.SetReply(r)
	response.Rcode = dns.RcodeServerFailure
	w.WriteMsg(response)
}

func (s *Server) writeForwardedResponse(w dns.ResponseWriter, request, response *dns.Msg) {
	response.Id = request.Id
	response.RecursionAvailable = true
	response.Response = true
	if responseWriterNetwork(w) == "udp" {
		size := dns.MinMsgSize
		if opt := request.IsEdns0(); opt != nil {
			size = int(opt.UDPSize())
		}
		response.Truncate(size)
	}
	w.WriteMsg(response)
}

func (s *Server) exchange(ctx context.Context, r *dns.Msg, w dns.ResponseWriter, client exchanger, upstream, network string) (*dns.Msg, bool) {
	response, _, err := client.ExchangeContext(ctx, r, upstream)
	if err != nil {
		s.logForwardWarning("DNS upstream query failed",
			forwardLogKey{upstream: upstream, network: network, reason: "exchange"},
			append(s.queryAttrs(r, w, upstream, network), "error", err)...)
		return nil, false
	}
	if response == nil {
		s.logForwardWarning("DNS upstream query returned no response",
			forwardLogKey{upstream: upstream, network: network, reason: "empty"},
			s.queryAttrs(r, w, upstream, network)...)
		return nil, false
	}
	// SERVFAIL from one upstream may be a resolver failure rather than an
	// authoritative answer, so try the next one. When every resolver agrees,
	// this trades some latency for protection against a broken upstream.
	if response.Rcode == dns.RcodeServerFailure {
		s.logForwardWarning("DNS upstream returned SERVFAIL",
			forwardLogKey{upstream: upstream, network: network, reason: "servfail"},
			s.queryAttrs(r, w, upstream, network)...)
		return nil, false
	}

	return response, true
}

func (s *Server) logForwardWarning(message string, key forwardLogKey, attrs ...any) {
	shouldLog, suppressed := s.forwardLogs.record(key)
	if !shouldLog {
		return
	}
	if suppressed > 0 {
		attrs = append(attrs, "suppressed", suppressed)
	}
	s.log.Warn(message, attrs...)
}

func (s *Server) queryAttrs(r *dns.Msg, w dns.ResponseWriter, upstream, network string) []any {
	qname, qtype := queryNameAndType(r)
	return []any{
		"qname", qname,
		"qtype", qtype,
		"client_ip", responseWriterIP(w),
		"upstream", upstream,
		"network", network,
	}
}

func queryNameAndType(r *dns.Msg) (string, string) {
	if r == nil || len(r.Question) == 0 {
		return "", ""
	}
	question := r.Question[0]
	qtype, ok := dns.TypeToString[question.Qtype]
	if !ok {
		qtype = fmt.Sprintf("TYPE%d", question.Qtype)
	}
	return strings.ToLower(question.Name), qtype
}

func responseWriterIP(w dns.ResponseWriter) string {
	if w == nil || w.RemoteAddr() == nil {
		return ""
	}
	switch addr := w.RemoteAddr().(type) {
	case *net.UDPAddr:
		return addr.IP.String()
	case *net.TCPAddr:
		return addr.IP.String()
	default:
		host, _, err := net.SplitHostPort(w.RemoteAddr().String())
		if err == nil {
			return host
		}
		return w.RemoteAddr().String()
	}
}

func responseWriterNetwork(w dns.ResponseWriter) string {
	if w == nil {
		return "unknown"
	}
	if addr := w.RemoteAddr(); addr != nil {
		network := addr.Network()
		if strings.HasPrefix(network, "udp") {
			return "udp"
		}
		if strings.HasPrefix(network, "tcp") {
			return "tcp"
		}
	}
	return "unknown"
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
		// Try resolving via entity store lookup
		if s.resolveUnknownIP(sourceIP) {
			s.mu.RLock()
			appName = s.ipToApp[sourceIP]
			s.mu.RUnlock()
		} else {
			s.log.Debug("dns query from unknown IP", "ip", sourceIP, "query", qname)
			w.WriteMsg(response)
			return
		}
	} else {
		s.mu.RUnlock()
	}

	// Get IPs for this app+service
	s.mu.RLock()
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

// Watch starts watching sandbox entities and maintains in-memory DNS mappings.
// It processes the initial snapshot synchronously so DNS is warm before
// returning, then consumes live changes in the background.
func (s *Server) Watch(ctx context.Context) error {
	// Create a child context that we can cancel independently
	s.watchCtx, s.watchCancel = context.WithCancel(ctx)

	index := entity.Ref(entity.EntityKind, compute_v1alpha.KindSandbox)
	s.watcher = indexwatch.New(s.entityClient, index, indexwatch.Options{Logger: s.log})
	if err := s.watcher.Start(s.watchCtx); err != nil {
		return err
	}

	// Process the initial snapshot synchronously (the watcher always delivers an
	// EventSync first) so DNS mappings are populated before Watch returns.
	select {
	case ev, ok := <-s.watcher.Updates():
		if ok {
			s.dispatchSandboxEvent(s.watchCtx, ev)
		}
	case <-s.watchCtx.Done():
		return s.watchCtx.Err()
	}

	s.watchWg.Go(func() {
		for {
			select {
			case <-s.watchCtx.Done():
				return
			case ev, ok := <-s.watcher.Updates():
				if !ok {
					return
				}
				s.dispatchSandboxEvent(s.watchCtx, ev)
			}
		}
	})

	return nil
}

// dispatchSandboxEvent applies a single watcher event to the DNS mappings.
func (s *Server) dispatchSandboxEvent(ctx context.Context, ev indexwatch.Event) {
	switch ev.Type {
	case indexwatch.EventSync:
		s.reconcileSandboxes(ctx, ev.Entities)
	case indexwatch.EventAdded, indexwatch.EventUpdated:
		if ev.Entity == nil {
			return
		}
		var sb compute_v1alpha.Sandbox
		sb.Decode(ev.Entity)
		s.handleSandboxUpdate(ctx, &sb, ev.Entity)
	case indexwatch.EventDeleted:
		s.handleSandboxDeleteByID(string(ev.Id))
	}
}

// reconcileSandboxes processes a full snapshot: it applies every sandbox and
// then removes any tracked sandbox that is no longer present in the index
// (deleted while the watch was down).
func (s *Server) reconcileSandboxes(ctx context.Context, entities []*entity.Entity) {
	present := make(map[string]struct{}, len(entities))
	for _, en := range entities {
		var sb compute_v1alpha.Sandbox
		sb.Decode(en)
		present[sb.ID.String()] = struct{}{}
		s.handleSandboxUpdate(ctx, &sb, en)
	}

	s.mu.RLock()
	var stale []string
	for id := range s.sandboxes {
		if _, ok := present[id]; !ok {
			stale = append(stale, id)
		}
	}
	s.mu.RUnlock()

	for _, id := range stale {
		s.handleSandboxDeleteByID(id)
	}
}

func (s *Server) handleSandboxUpdate(ctx context.Context, sb *compute_v1alpha.Sandbox, en *entity.Entity) {
	// Track PENDING and RUNNING sandboxes so DNS works during startup.
	// Containers can make DNS queries while still in PENDING state, before
	// the sandbox transitions to RUNNING.
	if !compute.SandboxActive(sb.Status) {
		// Sandbox is stopped/dead - remove from DNS if we were tracking it
		s.handleSandboxDeleteByID(sb.ID.String())
		return
	}

	// A sandbox with no address yet is in its pre-assignment state, not one giving
	// an address up: leave any existing claim alone and wait for the patch that
	// fills Network in, which arrives as another update.
	if len(sb.Network) == 0 {
		return
	}

	// Extract IP from address (may be in CIDR format like "10.0.0.5/24")
	ipAddr := sb.Network[0].Address
	if strings.Contains(ipAddr, "/") {
		ipAddr = strings.Split(ipAddr, "/")[0]
	}

	// Re-derive the mapping unless this sandbox is already the recorded owner of this
	// exact address. Checking ownership rather than mere presence is the point: a
	// sandbox that is tracked but whose address now names someone else has to be able
	// to reclaim it, otherwise a stale entry survives until the process restarts
	// (MIR-1511). The common case — an unrelated update for a sandbox that already
	// owns its address — still returns before the two entity lookups below.
	s.mu.RLock()
	prev, tracked := s.sandboxes[sb.ID.String()]
	owner := s.ipToSandbox[ipAddr]
	s.mu.RUnlock()

	if tracked && prev.ip == ipAddr && owner == sb.ID.String() {
		return
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

	s.AddSandboxMapping(sb.ID.String(), ipAddr, appName, service)
}

// NewTestServer creates a minimal Server for testing without binding to a port.
func NewTestServer() *Server {
	return &Server{
		log:             slog.Default(),
		ready:           make(chan struct{}),
		listenDone:      make(chan struct{}),
		ipToApp:         make(map[string]string),
		ipToSandbox:     make(map[string]string),
		ipToService:     make(map[string]string),
		appServiceToIPs: make(map[string]map[string][]string),
		sandboxes:       make(map[string]sandboxMapping),
	}
}

// LookupSandboxByIP returns the sandbox entity ID and app name for a given IP.
// On a cache miss, falls back to an entity store lookup to handle watcher lag.
func (s *Server) LookupSandboxByIP(ip string) (sandboxID, appName string, ok bool) {
	s.mu.RLock()
	sandboxID, ok = s.ipToSandbox[ip]
	if ok {
		appName = s.ipToApp[ip]
		s.mu.RUnlock()
		return sandboxID, appName, true
	}
	s.mu.RUnlock()

	// Cache miss — try resolving via entity store (populates the maps on success)
	if !s.resolveUnknownIP(ip) {
		return "", "", false
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	sandboxID, ok = s.ipToSandbox[ip]
	if !ok {
		return "", "", false
	}
	appName = s.ipToApp[ip]
	return sandboxID, appName, true
}

// AddSandboxMapping registers a sandbox's IP address for DNS resolution.
func (s *Server) AddSandboxMapping(sandboxID, ipAddr, appName, service string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.addSandboxMappingLocked(sandboxID, ipAddr, appName, service)
}

func (s *Server) addSandboxMappingLocked(sandboxID, ipAddr, appName, service string) {
	prev, tracked := s.sandboxes[sandboxID]

	// The sandbox moved: give up its previous address before claiming this one, so
	// the address it left behind stops resolving to it.
	if tracked && prev.ip != ipAddr {
		s.releaseIPLocked(prev.ip, sandboxID)
	}

	owner, owned := s.ipToSandbox[ipAddr]
	if tracked && prev.ip == ipAddr && prev.app == appName && prev.service == service && owner == sandboxID {
		return // already recorded exactly this way
	}

	if owned && owner != sandboxID {
		// Two sandboxes claiming one address is either a recycled IP whose previous
		// owner has not been cleaned up yet or a genuine duplicate assignment
		// (MIR-1238). The newest claim wins; log it either way, because before
		// MIR-1511 this transition was silent and left us unable to tell which of
		// the two a stale mapping had come from.
		s.log.Warn("sandbox IP mapping reassigned",
			"ip", ipAddr,
			"previous_sandbox", owner, "previous_app", s.ipToApp[ipAddr],
			"sandbox", sandboxID, "app", appName)
	}

	// Withdraw whatever this address currently advertises as before re-advertising
	// it: when an IP moves between apps the old app must stop handing it out.
	s.withdrawServiceRecordLocked(ipAddr)

	s.seq++
	s.sandboxes[sandboxID] = sandboxMapping{ip: ipAddr, app: appName, service: service, seq: s.seq}
	s.pointIPLocked(ipAddr, sandboxID, appName, service)

	s.log.Info("added sandbox to DNS mapping", "sandbox", sandboxID, "app", appName, "service", service, "ip", ipAddr)
}

// pointIPLocked makes ipAddr resolve to the given sandbox and advertises it under
// app+service. Callers are responsible for withdrawing any previous advertisement.
func (s *Server) pointIPLocked(ipAddr, sandboxID, appName, service string) {
	s.ipToApp[ipAddr] = appName
	s.ipToSandbox[ipAddr] = sandboxID
	s.ipToService[ipAddr] = service

	if s.appServiceToIPs[appName] == nil {
		s.appServiceToIPs[appName] = make(map[string][]string)
	}
	if !slices.Contains(s.appServiceToIPs[appName][service], ipAddr) {
		s.appServiceToIPs[appName][service] = append(s.appServiceToIPs[appName][service], ipAddr)
	}
}

// releaseIPLocked gives up ignoreID's claim on ipAddr. If another sandbox still claims
// the address — the usual case for a recycled IP, where the outgoing sandbox's entity
// is deleted well after the new sandbox has taken the address — the maps are re-pointed
// at the newest remaining claimant instead of being left naming the sandbox that just
// went away. Only when nothing claims the address any more is it withdrawn.
//
// ignoreID must be excluded rather than assumed absent: addSandboxMappingLocked calls
// this for a sandbox that has moved, and that sandbox is still in s.sandboxes under its
// old address at the time. handleSandboxDeleteByID has already removed it, so there the
// exclusion is redundant.
func (s *Server) releaseIPLocked(ipAddr, ignoreID string) {
	var (
		heirID string
		heir   sandboxMapping
	)
	for id, m := range s.sandboxes {
		if id == ignoreID || m.ip != ipAddr {
			continue
		}
		if heirID == "" || m.seq > heir.seq {
			heirID, heir = id, m
		}
	}

	if heirID == "" {
		s.withdrawServiceRecordLocked(ipAddr)
		delete(s.ipToApp, ipAddr)
		delete(s.ipToSandbox, ipAddr)
		delete(s.ipToService, ipAddr)
		s.log.Info("removed sandbox from DNS mapping", "entity_id", ignoreID, "ip", ipAddr)
		return
	}

	if s.ipToSandbox[ipAddr] == heirID {
		return // the heir already owns it; nothing to re-point
	}

	s.withdrawServiceRecordLocked(ipAddr)
	s.pointIPLocked(ipAddr, heirID, heir.app, heir.service)
	s.log.Info("re-pointed IP to surviving sandbox",
		"ip", ipAddr, "entity_id", ignoreID, "sandbox", heirID, "app", heir.app, "service", heir.service)
}

// withdrawServiceRecordLocked stops every app+service advertising ipAddr. It sweeps the
// whole table rather than trusting ipToApp, so an address that ended up advertised under
// more than one app can only be listed by its current owner afterwards.
func (s *Server) withdrawServiceRecordLocked(ipAddr string) {
	for appName, serviceMap := range s.appServiceToIPs {
		for service, ips := range serviceMap {
			filtered := slices.DeleteFunc(slices.Clone(ips), func(ip string) bool { return ip == ipAddr })
			if len(filtered) == len(ips) {
				continue
			}
			if len(filtered) == 0 {
				delete(serviceMap, service)
				continue
			}
			serviceMap[service] = filtered
		}
		if len(serviceMap) == 0 {
			delete(s.appServiceToIPs, appName)
		}
	}
}

// resolveUnknownIP searches the entity store for a sandbox with the given IP address
// and registers it for DNS resolution if found. This handles the race where a sandbox
// container starts making DNS queries before the entity watcher processes the RUNNING
// status update — PENDING sandboxes count, since the container is clearly running if it
// is making DNS queries.
//
// It considers only active sandboxes, and among several picks the most recently created.
// A sandbox that has stopped can keep its entity — address and all — for up to an hour
// after its container is gone, and taking the first address match regardless of status
// meant a dead sandbox could be installed as the owner of an address a live one was
// already using, which the token server then reads as the caller's identity (MIR-1511).
func (s *Server) resolveUnknownIP(sourceIP string) bool {
	if s.entityClient == nil {
		return false
	}

	// Give the watcher a moment to catch up — if it processes the sandbox
	// update in this window we avoid the full entity scan.
	time.Sleep(200 * time.Millisecond)

	s.mu.RLock()
	_, found := s.ipToApp[sourceIP]
	s.mu.RUnlock()
	if found {
		return true
	}

	return s.resolveIPFromStore(sourceIP)
}

// RefreshSandboxByIP re-derives an address's owner from the entity store and returns the
// result, ignoring what is cached. Callers use it when they have evidence the cached
// answer is wrong — the token server, for instance, when a caller presents a secret the
// resolved sandbox does not own.
//
// It is deliberately non-destructive: if the store cannot answer, the existing mapping is
// left in place rather than dropped, so a transient entity-store failure during a request
// that was already going to be rejected cannot also blank out an address's DNS.
func (s *Server) RefreshSandboxByIP(ip string) (sandboxID, appName string, ok bool) {
	if !s.resolveIPFromStore(ip) {
		return "", "", false
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	sandboxID, ok = s.ipToSandbox[ip]
	if !ok {
		return "", "", false
	}
	return sandboxID, s.ipToApp[ip], true
}

// resolveIPFromStore scans the sandbox index for the address's rightful owner and
// registers it, replacing whatever was mapped before. Among several claimants it takes
// the most recently created active one; see resolveUnknownIP for why.
func (s *Server) resolveIPFromStore(sourceIP string) bool {
	if s.entityClient == nil {
		return false
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := s.entityClient.List(ctx, entity.Ref(entity.EntityKind, compute_v1alpha.KindSandbox))
	if err != nil {
		s.log.Debug("failed to list sandboxes for unknown IP resolution", "ip", sourceIP, "error", err)
		return false
	}

	var (
		best      *compute_v1alpha.Sandbox
		bestEnt   *entity.Entity
		bestBirth int64
	)

	for _, ent := range resp.Values() {
		var sb compute_v1alpha.Sandbox
		sb.Decode(ent.Entity())

		if len(sb.Network) == 0 || !compute.SandboxActive(sb.Status) {
			continue
		}

		ipAddr := sb.Network[0].Address
		if strings.Contains(ipAddr, "/") {
			ipAddr = strings.Split(ipAddr, "/")[0]
		}

		if ipAddr != sourceIP {
			continue
		}

		if best == nil || ent.CreatedAt() > bestBirth {
			candidate := sb
			best, bestEnt, bestBirth = &candidate, ent.Entity(), ent.CreatedAt()
		}
	}

	if best == nil {
		return false
	}

	var md core_v1alpha.Metadata
	md.Decode(bestEnt)

	service, _ := md.Labels.Get("service")
	if service == "" {
		return false
	}

	verResp, err := s.entityClient.Get(ctx, best.Spec.Version.String())
	if err != nil {
		s.log.Debug("failed to get version for unknown IP sandbox", "ip", sourceIP, "error", err)
		return false
	}

	var appVer core_v1alpha.AppVersion
	appVer.Decode(verResp.Entity().Entity())

	appResp, err := s.entityClient.Get(ctx, appVer.App.String())
	if err != nil {
		s.log.Debug("failed to get app for unknown IP sandbox", "ip", sourceIP, "error", err)
		return false
	}

	var appMD core_v1alpha.Metadata
	appMD.Decode(appResp.Entity().Entity())

	s.AddSandboxMapping(best.ID.String(), sourceIP, appMD.Name, service)
	s.log.Info("resolved unknown IP to sandbox via entity lookup", "ip", sourceIP, "sandbox", best.ID, "app", appMD.Name, "service", service)
	return true
}

// lookupAppForIP returns the app name associated with the given IP address.
// Returns empty string if the IP is not registered.
func (s *Server) lookupAppForIP(ip string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.ipToApp[ip]
}

func (s *Server) handleSandboxDeleteByID(entityID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	m, found := s.sandboxes[entityID]
	if !found {
		// Not tracked, nothing to do
		return
	}

	delete(s.sandboxes, entityID)
	s.releaseIPLocked(m.ip, entityID)
}

// RemoveSandboxMapping withdraws a sandbox's claim on its address. It is called by the
// sandbox controller when a sandbox is torn down, so a local teardown takes effect
// immediately rather than waiting on the entity watcher.
func (s *Server) RemoveSandboxMapping(sandboxID string) {
	s.handleSandboxDeleteByID(sandboxID)
}

// ListenAndServe starts the DNS server
func (s *Server) ListenAndServe() error {
	if !s.listenStarted.CompareAndSwap(false, true) {
		return errors.New("dns server already started")
	}
	defer close(s.listenDone)
	if s.stopRequested.Load() {
		return nil
	}

	tcpListener, err := net.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("listening for TCP DNS: %w", err)
	}
	tcpListener = netutil.LimitListener(tcpListener, maxTCPConnections)

	// Binding TCP first lets an address ending in :0 pick one port that both
	// transports share. Production addresses name port 53 explicitly, but the
	// shared ephemeral port keeps the same invariant in tests.
	boundAddr := tcpListener.Addr().String()
	udpConn, err := net.ListenPacket("udp", boundAddr)
	if err != nil {
		tcpListener.Close()
		return fmt.Errorf("listening for UDP DNS: %w", err)
	}
	s.boundAddr = boundAddr

	started := make(chan struct{}, 2)
	s.udpServer.PacketConn = udpConn
	s.udpServer.NotifyStartedFunc = func() { started <- struct{}{} }
	s.tcpServer.Listener = tcpListener
	s.tcpServer.NotifyStartedFunc = func() { started <- struct{}{} }

	errs := make(chan error, 2)
	go func() { errs <- s.udpServer.ActivateAndServe() }()
	go func() { errs <- s.tcpServer.ActivateAndServe() }()

	for range 2 {
		select {
		case <-started:
		case serveErr := <-errs:
			udpConn.Close()
			tcpListener.Close()
			return serveErr
		}
	}
	close(s.ready)
	if s.stopRequested.Load() {
		s.shutdownListeners()
	}

	firstErr := <-errs
	shutdownErr := s.shutdownListeners()
	secondErr := <-errs
	return errors.Join(firstErr, secondErr, shutdownErr)
}

func (s *Server) shutdownListeners() error {
	s.shutdownOnce.Do(func() {
		s.shutdownErr = errors.Join(s.udpServer.Shutdown(), s.tcpServer.Shutdown())
	})
	return s.shutdownErr
}

// Shutdown gracefully shuts down the server
func (s *Server) Shutdown() error {
	s.stopRequested.Store(true)

	// Cancel the watch context to stop the watcher goroutine
	if s.watchCancel != nil {
		s.watchCancel()
	}

	if s.watcher != nil {
		s.watcher.Stop()
	}

	// Wait for the watcher goroutine to finish
	s.watchWg.Wait()

	if s.udpServer == nil || s.tcpServer == nil {
		return nil
	}
	if !s.listenStarted.Load() {
		return nil
	}
	select {
	case <-s.ready:
		return s.shutdownListeners()
	case <-s.listenDone:
		return nil
	}
}
