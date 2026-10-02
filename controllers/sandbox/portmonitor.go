package sandbox

import (
	"bufio"
	"context"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"go4.org/netipx"
	"miren.dev/runtime/observability"
)

// PortMonitor monitors ports for containers using polling
type PortMonitor struct {
	log    *slog.Logger
	ports  observability.PortTracker
	mu     sync.Mutex
	tasks  map[string]*monitorTask
	wg     sync.WaitGroup
	ctx    context.Context
	cancel context.CancelFunc

	// listPorts enumerates the routable and loopback-only listening ports in a
	// pid's netns, and reports whether it could read them at all. Defaults to
	// listeningPortsForPID; overridable in tests.
	listPorts func(pid int) (routable []int, loopback []int, ok bool)
}

type monitorTask struct {
	containerID string
	ip          string
	pid         int
	ports       []int
	cancel      context.CancelFunc
}

// NewPortMonitor creates a new port monitor
func NewPortMonitor(log *slog.Logger, ports observability.PortTracker) *PortMonitor {
	ctx, cancel := context.WithCancel(context.Background())
	return &PortMonitor{
		log:       log.With("module", "port-monitor"),
		ports:     ports,
		tasks:     make(map[string]*monitorTask),
		ctx:       ctx,
		cancel:    cancel,
		listPorts: listeningPortsForPID,
	}
}

// MonitorContainer starts monitoring ports for a container.
// It checks port binding by reading /proc/<pid>/net/tcp from the container's
// network namespace (via the pause container's PID) rather than doing a TCP
// dial from the host, which can be interfered with by iptables DNAT rules.
func (pm *PortMonitor) MonitorContainer(containerID string, ip string, pid int, ports []int) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	// Cancel any existing monitoring for this container
	if task, exists := pm.tasks[containerID]; exists {
		task.cancel()
	}

	// Create new monitoring task
	taskCtx, taskCancel := context.WithCancel(pm.ctx)
	task := &monitorTask{
		containerID: containerID,
		ip:          ip,
		pid:         pid,
		ports:       ports,
		cancel:      taskCancel,
	}
	pm.tasks[containerID] = task

	// Start monitoring in background
	pm.wg.Add(1)
	go pm.monitorPorts(taskCtx, task)
}

// StopMonitoring stops monitoring for a container
func (pm *PortMonitor) StopMonitoring(containerID string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if task, exists := pm.tasks[containerID]; exists {
		task.cancel()
		delete(pm.tasks, containerID)
	}
}

// Close stops all monitoring
func (pm *PortMonitor) Close() error {
	pm.cancel()
	pm.wg.Wait()
	return nil
}

// DiagnoseListening reports the ports a container is listening on inside its
// netns, split into routable (reachable from the host) and loopback-only sets.
// ok is false when the container is not being monitored (its pid is unknown)
// or its sockets couldn't be read, so an empty result with ok set really
// means the app listens on nothing.
// It is used on the port-wait timeout path: when the configured port never
// bound, this reveals what the app actually listened on so we can route to it
// or explain the failure.
func (pm *PortMonitor) DiagnoseListening(containerID string) (routable []int, loopback []int, ok bool) {
	pm.mu.Lock()
	task, exists := pm.tasks[containerID]
	pm.mu.Unlock()
	if !exists {
		return nil, nil, false
	}

	listPorts := pm.listPorts
	if listPorts == nil {
		listPorts = listeningPortsForPID
	}
	return listPorts(task.pid)
}

func (pm *PortMonitor) resolveIP(ip string) netip.Addr {
	if ip == "" {
		return netip.Addr{}
	}
	addr, _ := net.ResolveIPAddr("ip", ip)
	if addr != nil {
		if ipAddr, ok := netipx.FromStdIP(addr.IP); ok {
			return ipAddr
		}
	}
	return netip.Addr{}
}

func (pm *PortMonitor) monitorPorts(ctx context.Context, task *monitorTask) {
	defer pm.wg.Done()

	// Track which ports are currently bound
	boundPorts := make(map[int]bool)

	defer pm.log.Debug("ended port monitoring", "container", task.containerID, "bound_ports", len(boundPorts))

	// Initial delay to let container start
	select {
	case <-ctx.Done():
		return
	case <-time.After(100 * time.Millisecond):
	}

	// We run this HOT because we want to pickup the bound port as quickly as possible.
	// We used to also run this forever to pick up port changes AFTER the container
	// was running, but it's way too much traffic for that. So we modified this to only
	// run until all ports are bound.

	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()

	// Run this until we observe all the ports bound.
	for len(boundPorts) < len(task.ports) {
		select {
		case <-ctx.Done():
			// Mark all ports as unbound when stopping
			for port := range boundPorts {
				bp := observability.BoundPort{
					Port: port,
				}

				if addr := pm.resolveIP(task.ip); addr.IsValid() {
					bp.Addr = addr
				}
				pm.ports.SetPortStatus(task.containerID, bp, observability.PortStatusUnbound)
			}
			return
		case <-ticker.C:
			// Check each port
			for _, port := range task.ports {
				wasBound := boundPorts[port]
				if wasBound {
					continue // Already bound
				}

				isBound := checkPort(task.pid, port)
				if !isBound {
					continue // Still unbound
				}

				// Port became bound
				bp := observability.BoundPort{
					Port: port,
				}
				if addr := pm.resolveIP(task.ip); addr.IsValid() {
					bp.Addr = addr
				}
				pm.ports.SetPortStatus(task.containerID, bp, observability.PortStatusBound)
				boundPorts[port] = true
				pm.log.Debug("port became bound", "container", task.containerID, "port", port)
			}
		}
	}
}

// checkPort checks whether the given port is in LISTEN state inside the
// network namespace of the given PID by reading /proc/<pid>/net/tcp{,6}.
// This avoids TCP dials from the host which can be interfered with by
// iptables DNAT rules for node_port mappings.
func checkPort(pid int, port int) bool {
	for _, proto := range []string{"tcp", "tcp6"} {
		path := fmt.Sprintf("/proc/%d/net/%s", pid, proto)
		if portListening(path, port) {
			return true
		}
	}
	return false
}

// portListening parses a /proc/net/tcp{,6} file and returns true if any entry
// has the given local port in LISTEN state (0A).
//
// The file format has a header line followed by entries like:
//
//	sl  local_address rem_address   st tx_queue rx_queue ...
//	0: 00000000:0CEA 00000000:0000 0A 00000000:00000000 ...
//
// Field 1 (local_address) contains host:port in hex; field 3 (st) is the state.
func portListening(path string, port int) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	portHex := fmt.Sprintf("%04X", port)

	scanner := bufio.NewScanner(f)
	// Skip header line
	if !scanner.Scan() {
		return false
	}

	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 4 {
			continue
		}

		// fields[1] is local_address "ADDR:PORT", fields[3] is state
		localAddr := fields[1]
		state := fields[3]

		// State 0A = LISTEN
		if state != "0A" {
			continue
		}

		// Extract port from local_address (everything after the last colon)
		idx := strings.LastIndex(localAddr, ":")
		if idx < 0 {
			continue
		}

		if strings.EqualFold(localAddr[idx+1:], portHex) {
			return true
		}
	}

	return false
}

// listenSocket is a single socket observed in LISTEN state inside a netns.
type listenSocket struct {
	addr netip.Addr // bind address; unspecified (0.0.0.0/::) means "all interfaces"
	port int
}

// listListeningPorts parses a /proc/net/tcp{,6} file and returns every entry in
// LISTEN state (0A) with its bind address and port. Unlike portListening, which
// answers "is this one port listening?", this enumerates whatever the process
// actually bound so we can detect an app that ignored $PORT.
//
// ok is false when the file couldn't be opened, which is different from a
// process listening on nothing.
func listListeningPorts(path string) (sockets []listenSocket, ok bool) {
	f, err := os.Open(path)
	if err != nil {
		return nil, false
	}
	defer f.Close()

	var out []listenSocket

	scanner := bufio.NewScanner(f)
	// Skip header line
	if !scanner.Scan() {
		return nil, true
	}

	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 4 {
			continue
		}

		// State 0A = LISTEN
		if fields[3] != "0A" {
			continue
		}

		localAddr := fields[1]
		idx := strings.LastIndex(localAddr, ":")
		if idx < 0 {
			continue
		}

		port, err := strconv.ParseInt(localAddr[idx+1:], 16, 32)
		if err != nil {
			continue
		}

		addr, _ := parseHexAddr(localAddr[:idx])
		out = append(out, listenSocket{addr: addr, port: int(port)})
	}

	return out, true
}

// parseHexAddr decodes the hex local address from /proc/net/tcp{,6}. The bytes
// are stored little-endian within each 32-bit word, so an IPv4 "0100007F" is
// 127.0.0.1. Returns ok=false if the hex doesn't decode to a v4 or v6 address.
func parseHexAddr(hexAddr string) (netip.Addr, bool) {
	b, err := hex.DecodeString(hexAddr)
	if err != nil {
		return netip.Addr{}, false
	}

	switch len(b) {
	case 4:
		var a [4]byte
		a[0], a[1], a[2], a[3] = b[3], b[2], b[1], b[0]
		return netip.AddrFrom4(a), true
	case 16:
		var a [16]byte
		for w := range 4 {
			a[w*4+0] = b[w*4+3]
			a[w*4+1] = b[w*4+2]
			a[w*4+2] = b[w*4+1]
			a[w*4+3] = b[w*4+0]
		}
		return netip.AddrFrom16(a), true
	}

	return netip.Addr{}, false
}

// listeningPortsForPID enumerates all LISTEN sockets in the network namespace
// of pid, splitting them into routable ports (bound on a non-loopback address,
// reachable from the host) and loopback-only ports (127.0.0.0/8, ::1). A port
// that listens on any routable address is reported as routable even if it also
// has a loopback socket. Results are de-duplicated and sorted. ok is false
// when the IPv4 table couldn't be read; tcp6 is allowed to be missing, since
// a host without IPv6 has no such file.
func listeningPortsForPID(pid int) (routable []int, loopback []int, ok bool) {
	seenRoutable := map[int]bool{}
	seenLoopback := map[int]bool{}

	for _, proto := range []string{"tcp", "tcp6"} {
		path := fmt.Sprintf("/proc/%d/net/%s", pid, proto)
		sockets, read := listListeningPorts(path)
		if proto == "tcp" {
			ok = read
		}
		for _, s := range sockets {
			if s.addr.IsValid() && s.addr.IsLoopback() {
				seenLoopback[s.port] = true
			} else {
				seenRoutable[s.port] = true
			}
		}
	}

	for p := range seenRoutable {
		routable = append(routable, p)
	}
	for p := range seenLoopback {
		if !seenRoutable[p] {
			loopback = append(loopback, p)
		}
	}

	sort.Ints(routable)
	sort.Ints(loopback)
	return routable, loopback, ok
}
