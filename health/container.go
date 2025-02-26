package health

import (
	"context"
	"errors"
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
	"miren.dev/runtime/health/portreg"
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
	Status containerd.ProcessStatus
	Labels map[string]string

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

const endpointPrefix = "runtime.computer/endpoint:"

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
		var status containerd.Status

		task, err := cont.Task(ctx, nil)
		if err == nil {
			status, err = task.Status(ctx)
			if err != nil {
				c.Log.Error("failed to get task status", "error", err)
			}
		}

		c.mu.Lock()

		if cs, ok := c.status[cont.ID()]; ok {
			cs.Status = status.Status
		} else {
			// Labels aren't dynamic, so we'll just fetch them once
			lbls, err := cont.Labels(ctx)
			if err != nil {
				c.Log.Error("failed to get container labels", "error", err)
			}

			c.status[cont.ID()] = &ContainerStatus{
				Status:    status.Status,
				Labels:    lbls,
				Endpoints: setupEndpoints(lbls),
			}

			c.Log.Debug("container status created", "id", cont.ID(), "running", status.Status == containerd.Running)
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
			c.processEvent(ctx, e)
		case <-ticker.C:
			// TODO should we run this in the background?
			c.refreshStatus(ctx)
		}
	}
}

func (c *ContainerMonitor) processEvent(ctx context.Context, ev *events.Envelope) {
	v, err := typeurl.UnmarshalAny(ev.Event)
	if err != nil {
		c.Log.Error("failed to unmarshal event", "error", err)
		return
	}

	switch e := v.(type) {
	case *aevents.ContainerCreate:
		c.Log.Info("container created", "id", e.ID)
		c.refreshStatus(ctx)

	case *aevents.ContainerDelete:
		c.Log.Info("container deleted", "id", e.ID)

		c.mu.Lock()
		defer c.mu.Unlock()

		delete(c.status, e.ID)
	case *aevents.TaskExit:
		c.mu.Lock()
		defer c.mu.Unlock()

		if status, ok := c.status[e.ContainerID]; ok {
			c.Log.Info("task has exitted", "id", e.ContainerID,
				"status", e.ExitStatus, "exited-at", e.ExitedAt.AsTime())
			status.Status = containerd.Stopped
		}

		c.cond.Broadcast()
	case *aevents.TaskStart:
		c.mu.Lock()
		defer c.mu.Unlock()

		if status, ok := c.status[e.ContainerID]; ok {
			c.Log.Info("task started", "id", e.ContainerID)
			status.Status = containerd.Running
		}

		c.cond.Broadcast()
	}
}

func (c *ContainerMonitor) WaitReady(ctx context.Context, id string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	for {
		status, ok := c.status[id]

		if ok {
			switch status.Status {
			case containerd.Running:
				return nil

			case containerd.Stopped, containerd.Paused:
				c.Log.Warn("container has stopped while waiting to start", "status", status.Status)
				return fmt.Errorf("waiting for container to run, but container has stopped (%s)", status.Status)
			}
		}

		c.cond.Wait()
	}
}

func (c *ContainerMonitor) WaitForReady(ctx context.Context, id string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.Log.Info("waiting for container", "id", id)

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

		if ok {

			switch status.Status {
			case containerd.Running:
				var stillWaiting bool

				for _, ep := range status.Endpoints {
					if !ep.Ready() {
						stillWaiting = true
					}
				}

				if !stillWaiting {
					return nil
				}
			case containerd.Stopped, containerd.Paused:
				c.Log.Warn("container has stopped while waiting to start", "status", status.Status)
				return fmt.Errorf("waiting for container to run, but container has stopped (%s)", status.Status)
			}
		}

		c.cond.Wait()
	}

	return err
}

func (c *ContainerMonitor) WaitForPortActive(ctx context.Context, id string, port int) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	sub, cancel := context.WithCancel(ctx)
	defer cancel()

	var err error

	go func() {
		<-sub.Done()

		c.mu.Lock()
		err = sub.Err()
		c.cond.Broadcast()
		c.mu.Unlock()
	}()

	for err == nil {
		status, ok := c.status[id]

		if ok {
			switch status.Status {
			case containerd.Running:
				for _, ep := range status.Endpoints {
					if ep.Port == port && ep.Ready() {
						return nil
					}
				}
			case containerd.Stopped, containerd.Paused:
				c.Log.Warn("container has stopped while waiting to start", "status", status.Status)
				return fmt.Errorf("waiting for container to run, but container has stopped (%s)", status.Status)
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

const ipLabel = "runtime.computer/ip"

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

			pc := portreg.Get(curEp.Type)

			if pc != nil {
				go c.checkPort2(context.Background(), ip, 60*time.Second, curEp, pc)
				return
			} else {
				c.Log.Warn("unknown port type", "type", curEp.Type)
			}
		}

		c.cond.Broadcast()
		curEp.Status = status
	} else {
		c.Log.Warn("container not found", "id", id)
	}
}

func (c *ContainerMonitor) checkPort2(ctx context.Context, addr string, dur time.Duration, ep *Endpoint, pc portreg.PortChecker) error {
	start := time.Now()

	defer c.Log.Info("port check done", "addr", addr, "port", ep.Port)

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for time.Since(start) < dur {
		c.Log.Info("checking port", "addr", addr, "port", ep.Port)
		ok, err := pc.CheckPort(ctx, c.Log, addr, ep.Port)
		if err != nil {
			c.Log.Error("error checking port", "addr", addr, "port", ep.Port, "error", err)
			// We don't give up here, because, well, we don't have another option.
		} else if ok {
			c.mu.Lock()
			c.cond.Broadcast()
			ep.Status = observability.PortStatusActive
			c.mu.Unlock()
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			//ok
		}
	}

	return nil
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

		time.Sleep(500 * time.Millisecond)
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
		if err != nil {
			var netErr *net.OpError
			if errors.As(err, &netErr) {
				if !netErr.Temporary() && netErr.Op != "dial" {
					c.Log.Error("unable to check http port", "addr", addr, "port", ep.Port, "error", err, "op", netErr.Op)
				}
			} else {
				c.Log.Error("error checking http port", "addr", addr, "port", ep.Port, "error", err)
			}
		} else if resp.StatusCode < 400 {
			c.mu.Lock()
			c.cond.Broadcast()
			ep.Status = observability.PortStatusActive
			c.mu.Unlock()

			c.Log.Info("http port active", "addr", addr, "port", ep.Port, "status", resp.StatusCode)
			return nil
		} else {
			c.Log.Warn("http port bad status", "addr", addr, "port", ep.Port, "status", resp.StatusCode)
		}

		time.Sleep(500 * time.Millisecond)
	}

	c.Log.Warn("giving up on checking port status")
	return nil
}
