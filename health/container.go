package health

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"

	aevents "github.com/containerd/containerd/api/events"
	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/core/events"
	"github.com/containerd/containerd/v2/pkg/namespaces"
	"github.com/containerd/typeurl/v2"
	"miren.dev/runtime/observability"
)

type Endpoint struct {
	Name string
	Type string
	Port int
	Addr netip.Addr

	Status observability.PortStatus
}

func (ep *Endpoint) Ready() bool {
	switch ep.Status {
	case observability.PortStatusBound, observability.PortStatusActive:
		return true
	}

	return false
}

type ContainerStatus struct {
	Running bool
	Labels  map[string]string

	Endpoints map[string]*Endpoint
}

func (c *ContainerStatus) GetLabel(key string) string {
	if c.Labels == nil {
		return ""
	}

	return c.Labels[key]
}

type LabelKV struct {
	Name, Value string
}

func parseLabelValue(val string) []LabelKV {
	var labels []LabelKV

	for _, kv := range strings.Split(val, ",") {
		parts := strings.SplitN(kv, "=", 2)
		if len(parts) != 2 {
			labels = append(labels, LabelKV{
				Name:  "",
				Value: kv,
			})
		} else {
			labels = append(labels, LabelKV{
				Name:  parts[0],
				Value: parts[1],
			})
		}
	}

	return labels
}

const endpointPrefix = "miren.dev/endpoint:"

func setupEndpoints(labels map[string]string) map[string]*Endpoint {
	endpoints := make(map[string]*Endpoint)

	for k, v := range labels {
		if strings.HasPrefix(k, endpointPrefix) {
			name := strings.TrimPrefix(k, endpointPrefix)

			kvs := parseLabelValue(v)

			var (
				portep    *LabelKV
				defaultep *LabelKV
				typeep    *LabelKV
			)

			for _, kv := range kvs {
				switch kv.Name {
				case "port":
					portep = &kv
				case "type":
					typeep = &kv
				case "":
					defaultep = &kv
				}
			}

			if portep == nil && defaultep != nil {
				portep = defaultep
			}

			if portep != nil {
				if port, err := strconv.ParseInt(portep.Value, 10, 32); err == nil {
					var ep Endpoint
					ep.Name = name
					ep.Port = int(port)

					if typeep != nil {
						ep.Type = typeep.Value
					} else {
						ep.Type = name
					}

					endpoints[ep.Name] = &ep
				}
			}
		}
	}

	return endpoints
}

type ContainerMonitor struct {
	Log       *slog.Logger
	CC        *containerd.Client
	Namespace string `asm:"namespace"`

	mu     sync.Mutex
	status map[string]*ContainerStatus
	cond   *sync.Cond
}

func (c *ContainerMonitor) Populated() error {
	c.status = make(map[string]*ContainerStatus)
	c.cond = sync.NewCond(&c.mu)

	return nil
}

func (c *ContainerMonitor) refreshStatus(ctx context.Context) {
	ctx = namespaces.WithNamespace(ctx, c.Namespace)

	containers, err := c.CC.Containers(ctx)
	if err != nil {
		c.Log.Error("failed to list containers", "error", err)
		return
	}

	for _, cont := range containers {
		task, err := cont.Task(ctx, nil)
		if err != nil {
			c.Log.Error("failed to get task", "error", err)
			continue
		}

		status, err := task.Status(ctx)
		if err != nil {
			c.Log.Error("failed to get task status", "error", err)
			continue
		}

		c.mu.Lock()

		if cs, ok := c.status[cont.ID()]; ok {
			cs.Running = status.Status == containerd.Running
		} else {
			// Labels aren't dynamic, so we'll just fetch them once
			lbls, err := cont.Labels(ctx)
			if err != nil {
				c.Log.Error("failed to get container labels", "error", err)
			}

			c.status[cont.ID()] = &ContainerStatus{
				Running:   status.Status == containerd.Running,
				Labels:    lbls,
				Endpoints: setupEndpoints(lbls),
			}
		}

		c.mu.Unlock()
	}

	c.cond.Broadcast()
}

func (c *ContainerMonitor) MonitorEvents(ctx context.Context) error {
	ctx = namespaces.WithNamespace(ctx, c.Namespace)

	envelopes, errs := c.CC.EventService().Subscribe(ctx)

	c.refreshStatus(ctx)

	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-errs:
			c.Log.Error("containerd event error", "error", err)
			return err
		case e := <-envelopes:
			c.Log.Info("containerd event", "event", e)
			c.processEvent(e)
		case <-ticker.C:
			// TODO should we run this in the background?
			c.refreshStatus(ctx)
		}
	}
}

func (c *ContainerMonitor) processEvent(ev *events.Envelope) {
	v, err := typeurl.UnmarshalAny(ev.Event)
	if err != nil {
		c.Log.Error("failed to unmarshal event", "error", err)
		return
	}

	switch e := v.(type) {
	case *aevents.TaskExit:
		c.mu.Lock()
		defer c.mu.Unlock()

		if status, ok := c.status[e.ContainerID]; ok {
			status.Running = false
		}

		c.cond.Broadcast()
	case *aevents.TaskStart:
		c.mu.Lock()
		defer c.mu.Unlock()

		if status, ok := c.status[e.ContainerID]; ok {
			status.Running = true
		}

		c.cond.Broadcast()
	}
}

func (c *ContainerMonitor) WaitReady(ctx context.Context, id string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	for {
		status, ok := c.status[id]

		if ok && status.Running {
			return nil
		}

		c.cond.Wait()
	}
}

func (c *ContainerMonitor) WaitForPortActive(ctx context.Context, id string, port int) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	sub, cancel := context.WithCancel(ctx)
	defer cancel()

	var err error

	go func() {
		<-sub.Done()
		err = sub.Err()

		c.mu.Lock()
		c.cond.Broadcast()
		c.mu.Unlock()
	}()

	for err == nil {
		status, ok := c.status[id]

		if ok && status.Running {
			for _, ep := range status.Endpoints {
				if ep.Port == port && ep.Ready() {
					return nil
				}
			}
		}

		c.cond.Wait()
	}

	return err
}

func (c *ContainerMonitor) Status(ctx context.Context, id string) (*ContainerStatus, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.status[id], nil
}

const ipLabel = "miren.dev/ip"

func (c *ContainerMonitor) SetPortStatus(id string, bp observability.BoundPort, status observability.PortStatus) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.Log.Info("setting port status", "id", id, "port", bp.Port, "status", status)

	if cs, ok := c.status[id]; ok {
		var curEp *Endpoint

		for _, ep := range cs.Endpoints {
			if ep.Port == bp.Port {
				curEp = ep
				break
			}
		}

		if curEp == nil {
			c.Log.Warn("port not registered, inventing new port", "id", id, "port", bp.Port)

			name := strconv.Itoa(bp.Port)

			curEp = &Endpoint{
				Name: name,
				Port: bp.Port,
			}

			cs.Endpoints[name] = curEp
		}

		ip := cs.Labels[ipLabel]

		if status == observability.PortStatusBound && ip != "" {
			switch curEp.Type {
			case "http":
				c.Log.Info("checking http port", "addr", ip, "port", curEp.Port)
				go c.checkHTTP(context.Background(), ip, 10*time.Second, curEp)
				return

			case "", "tcp":
				c.Log.Info("checking tcp port", "addr", ip, "port", curEp.Port)
				go c.checkPort(context.Background(), ip, 10*time.Second, curEp)

				return
			case "udp":
				// nothing needed
			default:
				c.Log.Warn("unknown port type", "type", curEp.Type)
			}
		}

		c.cond.Broadcast()
		curEp.Status = status
	} else {
		c.Log.Warn("container not found", "id", id)
	}
}

func (c *ContainerMonitor) checkPort(ctx context.Context, addr string, dur time.Duration, ep *Endpoint) error {
	start := time.Now()

	if strings.IndexByte(addr, ':') != -1 {
		addr = fmt.Sprintf("[%s]:%d", addr, ep.Port)
	} else {
		addr = fmt.Sprintf("%s:%d", addr, ep.Port)
	}

	for time.Since(start) < dur {
		conn, err := net.Dial("tcp", addr)
		if err == nil {
			conn.Close()

			c.Log.Info("generic port active", "addr", addr, "port", ep.Port)

			c.mu.Lock()
			c.cond.Broadcast()
			ep.Status = observability.PortStatusActive
			c.mu.Unlock()
			return nil
		}
	}

	return nil
}

func (c *ContainerMonitor) checkHTTP(ctx context.Context, addr string, dur time.Duration, ep *Endpoint) error {
	start := time.Now()

	var url string

	if strings.IndexByte(addr, ':') != -1 {
		url = fmt.Sprintf("http://[%s]:%d/", addr, ep.Port)
	} else {
		url = fmt.Sprintf("http://%s:%d/", addr, ep.Port)
	}

	for time.Since(start) < dur {
		resp, err := http.Get(url)
		if err == nil && resp.StatusCode < 400 {
			c.Log.Info("http port active", "addr", addr, "port", ep.Port, "status", resp.StatusCode)

			c.mu.Lock()
			c.cond.Broadcast()
			ep.Status = observability.PortStatusActive
			c.mu.Unlock()
			return nil
		}
	}

	return nil
}
