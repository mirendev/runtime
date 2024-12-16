package lease

import (
	"context"
	"fmt"
	"log/slog"
	"net/netip"
	"os"
	"path/filepath"
	"sync"
	"time"

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

	MaxLeasesPerContainer int           `asm:"max_leases_per_container,optional"`
	MaxContainersPerApp   int           `asm:"max_containers_per_app,optional"`
	IdleTimeout           time.Duration `asm:"container_idle_timeout,optional"`

	mu sync.Mutex

	applications map[string]*application

	pending map[string]chan struct{}
}

func (l *LaunchContainer) Populated() error {
	l.applications = make(map[string]*application)

	if l.MaxLeasesPerContainer == 0 {
		l.MaxLeasesPerContainer = 80
	}

	if l.MaxContainersPerApp == 0 {
		l.MaxContainersPerApp = 80
	}

	if l.IdleTimeout == 0 {
		l.IdleTimeout = 1 * time.Hour
	}

	return nil
}

type pendingContainer struct {
	waiters set.Set[chan *UsageWindow]
}

type application struct {
	name string

	mu   sync.Mutex
	cond *sync.Cond

	maxLeasesPerWindow int

	windows set.Set[*UsageWindow]
	idle    set.Set[*runningContainer]
	pending set.Set[*pendingContainer]
}

func (l *LaunchContainer) newApplication(name string) *application {
	app := &application{
		name:               name,
		maxLeasesPerWindow: l.MaxLeasesPerContainer,
		windows:            set.New[*UsageWindow](),
		idle:               set.New[*runningContainer](),
		pending:            set.New[*pendingContainer](),
	}

	app.cond = sync.NewCond(&app.mu)

	l.Log.Info("tracking application", "app", name, "max-leases", app.maxLeasesPerWindow, "up", l.MaxLeasesPerContainer)

	return app
}

func (a *application) availableWindow() *UsageWindow {
	if a.windows.Empty() {
		return nil
	}

	// TODO we end up load balancing across windows because Go varies the iteration
	// order of the for here. Not the greatest way to load balance, but it's better
	// than nothing.
	for w := range a.windows {
		if w.Leases.Len() < a.maxLeasesPerWindow {
			return w
		}
	}

	return nil
}

func (a *application) availableIdleContainer() *runningContainer {
	if a.idle.Empty() {
		return nil
	}

	// TODO we end up load balancing across containers because Go varies the iteration
	// order of the for here. Not the greatest way to load balance, but it's better
	// than nothing.

	// NOTE this is only run when there are no windows available, so we
	// presume that the leases on any container is 0.
	for c := range a.idle {
		if c.windows.Empty() {
			return c
		}
	}

	return nil
}

func (l *LaunchContainer) lookupApp(app string) *application {
	l.mu.Lock()
	defer l.mu.Unlock()

	a, ok := l.applications[app]
	if !ok {
		a = l.newApplication(app)
		l.applications[app] = a
	}

	return a
}

type runningContainer struct {
	id          string
	cpuStatPath string

	idleSince time.Time

	windows set.Set[*UsageWindow]

	buf [128]byte
}

func parseInt(b []byte) (uint64, error) {
	n := uint64(0)
	for _, v := range b {
		if v == '\n' {
			break
		}
		n = n*10 + uint64(v-'0')
	}
	return n, nil
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

	return parseInt(data)

	//i := bytes.IndexByte(data, '\n')

	//return strconv.ParseUint(string(data[:i]), 10, 64)
}

type UsageWindow struct {
	App string
	Id  string

	Start, End uint64

	Leases set.Set[*LeasedContainer]

	container *runningContainer
}

type LeasedContainer struct {
	App *application

	Start  uint64
	Window *UsageWindow

	StartedWindow bool
}

func (l *LeasedContainer) Container() string {
	return l.Window.container.id
}

func (l *LaunchContainer) Lease(ctx context.Context, name string) (*LeasedContainer, error) {
	app := l.lookupApp(name)

	var (
		err error
	)

	app.mu.Lock()
	defer app.mu.Unlock()

	win := app.availableWindow()
	if win != nil {
		start, err := win.container.cpuUsage()
		if err != nil {
			return nil, err
		}

		l.Log.Info("adding lease to existing window", "window", win.Id)

		lc := &LeasedContainer{
			App:    app,
			Window: win,
			Start:  start,
		}

		win.Leases.Add(lc)

		return lc, nil
	}

	// No windows, but we might still have a container kicking around
	// we can reuse.
	rc := app.availableIdleContainer()
	if rc != nil {
		start, err := rc.cpuUsage()
		if err != nil {
			return nil, err
		}

		app.idle.Remove(rc)

		l.Log.Debug("beginning new usage window", "app", app.name, "start", start)

		win = &UsageWindow{
			App:    app.name,
			Id:     identity.NewID(),
			Start:  start,
			Leases: set.New[*LeasedContainer](),

			container: rc,
		}

		lc := &LeasedContainer{
			App:           app,
			Window:        win,
			Start:         start,
			StartedWindow: true,
		}

		win.Leases.Add(lc)

		return lc, nil
	}

	// If there are any pending containers, find one with room and attach ourselves
	// to it.

	var pendingCh chan *UsageWindow

	if !app.pending.Empty() {
		for pc := range app.pending {
			if pc.waiters.Len() < l.MaxLeasesPerContainer {
				// So that the sender doesn't block.
				pendingCh = make(chan *UsageWindow, 1)
				pc.waiters.Add(pendingCh)
				break
			}
		}
	}

	if pendingCh != nil {
		app.mu.Unlock()

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case win, ok := <-pendingCh:
			app.mu.Lock()

			if !ok {
				return nil, fmt.Errorf("pending container failed")
			}

			lc := &LeasedContainer{
				App:    app,
				Window: win,
				Start:  win.Start,
			}

			win.Leases.Add(lc)

			return lc, nil
		}
	}

	// ok, we need to launch a container.

	ac, err := l.AppAccess.LoadApp(ctx, app.name)
	if err != nil {
		return nil, err
	}

	mrv, err := l.AppAccess.MostRecentVersion(ctx, ac)
	if err != nil {
		return nil, err
	}

	pc := &pendingContainer{
		waiters: set.New[chan *UsageWindow](),
	}

	app.pending.Add(pc)
	defer app.pending.Remove(pc)

	winId := identity.NewID()
	l.Log.Info("launching container", "app", ac.Name, "version", mrv.Version, "window", winId)

	app.mu.Unlock()
	rc, err = l.launch(ctx, ac, mrv)
	app.mu.Lock()

	if err != nil {
		for ch := range pc.waiters {
			close(ch)
		}

		l.Log.Error("failed to launch container", "app", ac.Name, "version", mrv.Version, "error", err)
		return nil, err
	} else {
		l.Log.Info("launched container", "app", ac.Name, "version", mrv.Version, "window", winId)
	}

	win = &UsageWindow{
		App:    app.name,
		Id:     winId,
		Start:  0,
		Leases: set.New[*LeasedContainer](),

		container: rc,
	}

	app.windows.Add(win)

	lc := &LeasedContainer{
		App:           app,
		Window:        win,
		Start:         win.Start,
		StartedWindow: true,
	}

	win.Leases.Add(lc)

	for ch := range pc.waiters {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case ch <- win:
			// ok
		}
	}

	return lc, nil
}

type LeaseInfo struct {
	Usage uint64
}

func (l *LaunchContainer) ReleaseLease(ctx context.Context, lc *LeasedContainer) (*LeaseInfo, error) {
	lc.App.mu.Lock()
	defer lc.App.mu.Unlock()

	lc.Window.Leases.Remove(lc)

	ts, err := lc.Window.container.cpuUsage()
	if err != nil {
		return nil, err
	}

	if lc.Window.Leases.Empty() {
		lc.Window.End = ts
		lc.App.idle.Add(lc.Window.container)
		lc.Window.container.idleSince = time.Now()
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

func (l *LaunchContainer) ShutdownIdle(ctx context.Context) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	var cnt int

	for _, app := range l.applications {
		app.mu.Lock()

		var toDelete []*runningContainer

		for rc := range app.idle {
			if time.Since(rc.idleSince) >= l.IdleTimeout {
				l.Log.Info("shutting down idle container", "container", rc.id)

				err := l.CR.StopContainer(ctx, rc.id)
				if err != nil {
					l.Log.Error("failed to stop container", "container", rc.id, "error", err)
				} else {
					toDelete = append(toDelete, rc)
				}
			}
		}

		cnt += len(toDelete)

		for _, rc := range toDelete {
			app.idle.Remove(rc)
		}

		app.mu.Unlock()
	}

	return cnt, nil
}
