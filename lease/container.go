package lease

import (
	"bytes"
	"context"
	"log/slog"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"sync"

	"github.com/moby/buildkit/identity"
	"miren.dev/runtime/app"
	"miren.dev/runtime/discovery"
	"miren.dev/runtime/health"
	"miren.dev/runtime/network"
	"miren.dev/runtime/pkg/set"
	"miren.dev/runtime/run"
)

type LaunchContainer struct {
	Log       *slog.Logger
	AppAccess *app.AppAccess
	CR        *run.ContainerRunner
	CD        *discovery.Containerd
	IPPool    *network.IPPool
	Health    *health.ContainerMonitor

	mu         sync.Mutex
	containers map[string]*runningContainer
	windows    map[string]*UsageWindow
	pending    map[string]chan struct{}
}

func (l *LaunchContainer) Populated() error {
	l.containers = make(map[string]*runningContainer)
	l.windows = make(map[string]*UsageWindow)
	l.pending = make(map[string]chan struct{})
	return nil
}

type runningContainer struct {
	id          string
	cpuStatPath string

	buf [128]byte
}

func (r *runningContainer) cpuUsage() (uint64, error) {
	f, err := os.Open(r.cpuStatPath)
	if err != nil {
		return 0, err
	}

	defer f.Close()

	data := r.buf[:]

	n, err := f.Read(data)
	if err != nil {
		return 0, err
	}

	data = data[11:n]

	i := bytes.IndexByte(data, '\n')

	return strconv.ParseUint(string(data[:i]), 10, 64)
}

type UsageWindow struct {
	App string
	Id  string

	Start, End uint64

	Leases set.Set[*LeasedContainer]

	container *runningContainer
}

type LeasedContainer struct {
	Start  uint64
	Window *UsageWindow

	StartedWindow bool
}

func (l *LeasedContainer) Container() string {
	return l.Window.container.id
}

func (l *LaunchContainer) findRunningContainer(ctx context.Context, app string) (*LeasedContainer, error) {
	var (
		start uint64
		err   error
	)

	win, ok := l.windows[app]
	if !ok {
		rc, ok := l.containers[app]
		if !ok {
			return nil, nil
		}

		start, err = win.container.cpuUsage()
		if err != nil {
			return nil, err
		}

		l.Log.Debug("beginning new usage window", "app", app, "start", start)

		win = &UsageWindow{
			App:    app,
			Id:     identity.NewID(),
			Start:  start,
			Leases: set.New[*LeasedContainer](),

			container: rc,
		}
	} else {
		start, err = win.container.cpuUsage()
		if err != nil {
			return nil, err
		}

		l.Log.Debug("starting lease within existing usage window", "app", app, "start", start)
	}

	lc := &LeasedContainer{
		Window: win,
		Start:  start,
	}

	win.Leases.Add(lc)

	return lc, nil
}

func (l *LaunchContainer) existingOrSetupPending(ctx context.Context, app string) (*LeasedContainer, chan struct{}, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	// If there is a pending app start, then wait for it to finish.
	// FIXME this presumes we're mapping an app to one container, which is wrong.
	if ch, ok := l.pending[app]; ok {
		l.mu.Unlock()

		l.Log.Debug("waiting for pending container", "app", app)

		select {
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		case <-ch:
			// ok
		}

		l.mu.Lock()
	}

	lc, err := l.findRunningContainer(ctx, app)
	if err != nil {
		return nil, nil, err
	}

	if lc != nil {
		return lc, nil, nil
	}

	ch := make(chan struct{})

	l.pending[app] = ch
	l.Log.Debug("registered for pending container", "app", app)

	return nil, ch, nil
}

func (l *LaunchContainer) Lease(ctx context.Context, app string) (*LeasedContainer, error) {
	lc, pendingCh, err := l.existingOrSetupPending(ctx, app)
	if lc != nil {
		return lc, nil
	}

	defer close(pendingCh)

	ac, err := l.AppAccess.LoadApp(ctx, app)
	if err != nil {
		return nil, err
	}

	mrv, err := l.AppAccess.MostRecentVersion(ctx, ac)
	if err != nil {
		return nil, err
	}

	l.Log.Info("launching container", "app", ac.Name, "version", mrv.Version)

	rc, err := l.launch(ctx, ac, mrv)
	if err != nil {
		l.Log.Error("failed to launch container", "app", ac.Name, "version", mrv.Version, "error", err)
	} else {
		l.Log.Info("launched container", "app", ac.Name, "version", mrv.Version)
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	// FIXME this is wrong, we should be mapping app to a set of containers.
	l.containers[ac.Name] = rc

	win := &UsageWindow{
		App:    app,
		Id:     identity.NewID(),
		Start:  0,
		Leases: set.New[*LeasedContainer](),

		container: rc,
	}

	l.windows[app] = win

	lc = &LeasedContainer{
		Window:        win,
		Start:         win.Start,
		StartedWindow: true,
	}

	win.Leases.Add(lc)

	return lc, nil
}

type LeaseInfo struct {
	Usage uint64
}

func (l *LaunchContainer) ReleaseLease(ctx context.Context, lc *LeasedContainer) (*LeaseInfo, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	lc.Window.Leases.Remove(lc)

	ts, err := lc.Window.container.cpuUsage()
	if err != nil {
		return nil, err
	}

	if lc.Window.Leases.Empty() {
		lc.Window.End = ts
		l.Log.Info("usage window closed", "window", lc.Window.Id, "app", lc.Window.App)
	}

	i := &LeaseInfo{
		Usage: ts - lc.Start,
	}

	return i, nil
}

func (l *LaunchContainer) launch(
	ctx context.Context,
	ac *app.AppConfig,
	mrv *app.AppVersion,
) (*runningContainer, error) {

	sa := l.IPPool.Router()

	ca, err := l.IPPool.Allocate()
	if err != nil {
		return nil, err
	}

	config := &run.ContainerConfig{
		App:   ac.Name,
		Image: mrv.ImageName(),
		IPs:   []netip.Prefix{ca},
		Subnet: &run.Subnet{
			Id:     "sub",
			IP:     []netip.Prefix{sa},
			OSName: "mtest",
		},
	}

	_, err = l.CR.RunContainer(ctx, config)
	if err != nil {
		return nil, err
	}

	err = l.Health.WaitForReady(ctx, config.Id)
	if err != nil {
		return nil, err
	}

	rc := &runningContainer{
		id:          config.Id,
		cpuStatPath: filepath.Join("/sys/fs/cgroup", config.CGroupPath, "cpu.stat"),
	}

	return rc, nil
}
