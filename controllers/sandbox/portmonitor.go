package sandbox

import (
	"context"
	"fmt"
	"log/slog"
	"net"
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
}

type monitorTask struct {
	containerID string
	ip          string
	ports       []int
	cancel      context.CancelFunc
}

// NewPortMonitor creates a new port monitor
func NewPortMonitor(log *slog.Logger, ports observability.PortTracker) *PortMonitor {
	ctx, cancel := context.WithCancel(context.Background())
	return &PortMonitor{
		log:    log.With("module", "port-monitor"),
		ports:  ports,
		tasks:  make(map[string]*monitorTask),
		ctx:    ctx,
		cancel: cancel,
	}
}

// MonitorContainer starts monitoring ports for a container
func (pm *PortMonitor) MonitorContainer(containerID string, ip string, ports []int) {
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

func (pm *PortMonitor) monitorPorts(ctx context.Context, task *monitorTask) {
	defer pm.wg.Done()

	// Track which ports are currently bound
	boundPorts := make(map[int]bool)

	// Initial delay to let container start
	select {
	case <-ctx.Done():
		return
	case <-time.After(100 * time.Millisecond):
	}

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			// Mark all ports as unbound when stopping
			for port := range boundPorts {
				bp := observability.BoundPort{
					Port: port,
				}
				if task.ip != "" {
					addr, _ := net.ResolveIPAddr("ip", task.ip)
					if addr != nil {
						if ipAddr, ok := netipx.FromStdIP(addr.IP); ok {
							bp.Addr = ipAddr
						}
					}
				}
				pm.ports.SetPortStatus(task.containerID, bp, observability.PortStatusUnbound)
			}
			return
		case <-ticker.C:
			// Check each port
			for _, port := range task.ports {
				isBound := pm.checkPort(task.ip, port)
				wasBound := boundPorts[port]

				if isBound && !wasBound {
					// Port became bound
					bp := observability.BoundPort{
						Port: port,
					}
					if task.ip != "" {
						addr, _ := net.ResolveIPAddr("ip", task.ip)
						if addr != nil {
							if ipAddr, ok := netipx.FromStdIP(addr.IP); ok {
								bp.Addr = ipAddr
							}
						}
					}
					pm.ports.SetPortStatus(task.containerID, bp, observability.PortStatusBound)
					boundPorts[port] = true
					pm.log.Debug("port became bound", "container", task.containerID, "port", port)
				} else if !isBound && wasBound {
					// Port became unbound
					bp := observability.BoundPort{
						Port: port,
					}
					if task.ip != "" {
						addr, _ := net.ResolveIPAddr("ip", task.ip)
						if addr != nil {
							if ipAddr, ok := netipx.FromStdIP(addr.IP); ok {
								bp.Addr = ipAddr
							}
						}
					}
					pm.ports.SetPortStatus(task.containerID, bp, observability.PortStatusUnbound)
					delete(boundPorts, port)
					pm.log.Debug("port became unbound", "container", task.containerID, "port", port)
				}
			}
		}
	}
}

func (pm *PortMonitor) checkPort(ip string, port int) bool {
	address := net.JoinHostPort(ip, fmt.Sprintf("%d", port))
	conn, err := net.DialTimeout("tcp", address, 100*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}
