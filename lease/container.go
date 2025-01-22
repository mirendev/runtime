package lease

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/pkg/namespaces"
	"github.com/moby/buildkit/identity"
	"miren.dev/runtime/app"
	"miren.dev/runtime/discovery"
	"miren.dev/runtime/health"
	"miren.dev/runtime/network"
	"miren.dev/runtime/pkg/netdb"
	"miren.dev/runtime/pkg/set"
	"miren.dev/runtime/run"
)

type LaunchContainer struct {
	Log       *slog.Logger
	AppAccess *app.AppAccess
	CR        *run.ContainerRunner
	CC        *client.Client
	CD        *discovery.Containerd
	Subnet    *netdb.Subnet
	Health    *health.ContainerMonitor
	DB        *sql.DB `asm:"clickhouse"`

	Namespace string `asm:"namespace"`

	Bridge string `asm:"bridge-iface"`

	MaxLeasesPerContainer int           `asm:"max_leases_per_container,optional"`
	MaxContainersPerApp   int           `asm:"max_containers_per_app,optional"`
	IdleTimeout           time.Duration `asm:"container_idle_timeout,optional"`

	mu sync.Mutex

	applications map[string]*application

	pending map[string]chan struct{}

	lattrack *LatencyTracker
	rif      atomic.Int32
}

func (l *LaunchContainer) Populated() error {
	l.applications = make(map[string]*application)

	// TODO make the alpha configurable?
	l.lattrack = NewLatencyTracker(DefaultAlpha)

	if l.MaxLeasesPerContainer == 0 {
		l.MaxLeasesPerContainer = 80
	}

	if l.MaxContainersPerApp == 0 {
		l.MaxContainersPerApp = 80
	}

	if l.IdleTimeout == 0 {
		l.IdleTimeout = 1 * time.Hour
	}

	_, err := l.DB.Exec(
		`
CREATE TABLE IF NOT EXISTS container_usage (
    timestamp DateTime64(9) CODEC(Delta(8), ZSTD(1)),
    app LowCardinality(String) CODEC(ZSTD(1)),
		usage UInt64,
		leases UInt32,
) 
		ENGINE = MergeTree
		ORDER BY (app, toUnixTimestamp(timestamp))
`)

	return err
}

func (l *LaunchContainer) LatencyEstimate() (int32, float64) {
	rif := l.rif.Load()
	return rif, l.lattrack.GetLatencyEstimate(rif)
}

type pendingContainer struct {
	waiters set.Set[chan *UsageWindow]
}

type application struct {
	name  string
	pools map[string]*pool
}

type pool struct {
	app  *application
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
		name:  name,
		pools: make(map[string]*pool),
	}

	l.Log.Info("tracking application", "app", name, "max-leases", l.MaxLeasesPerContainer)

	return app
}

func (a *pool) availableWindow() *UsageWindow {
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

func (a *pool) availableIdleContainer() *runningContainer {
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

func (l *LaunchContainer) lookupPool(app, name string) *pool {
	l.mu.Lock()
	defer l.mu.Unlock()

	a, ok := l.applications[app]
	if !ok {
		a = l.newApplication(app)
		l.applications[app] = a
	}

	p, ok := a.pools[name]
	if !ok {
		p = &pool{
			app:                a,
			name:               name,
			maxLeasesPerWindow: l.MaxLeasesPerContainer,
			windows:            set.New[*UsageWindow](),
			idle:               set.New[*runningContainer](),
			pending:            set.New[*pendingContainer](),
		}

		p.cond = sync.NewCond(&p.mu)
		a.pools[name] = p
	}

	return p
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

	Leases      set.Set[*LeasedContainer]
	TotalLeases uint32

	container *runningContainer
}

type LeasedContainer struct {
	lc        *LaunchContainer
	Pool      *pool
	StartTime time.Time

	Start  uint64
	Window *UsageWindow

	StartedWindow bool
}

func (l *LeasedContainer) Container() string {
	return l.Window.container.id
}

func (l *LeasedContainer) Obj(ctx context.Context) (client.Container, error) {
	return l.lc.CC.LoadContainer(ctx, l.Container())
}

type leaseOptions struct {
	dontWaitNetwork bool
	poolName        string
}

func DontWaitNetwork() LeaseOption {
	return func(lc *leaseOptions) {
		lc.dontWaitNetwork = true
	}
}

func Pool(name string) LeaseOption {
	return func(lc *leaseOptions) {
		lc.poolName = name
	}
}

type LeaseOption func(*leaseOptions)

func (l *LaunchContainer) Lease(ctx context.Context, name string, opts ...LeaseOption) (*LeasedContainer, error) {
	var lo leaseOptions

	for _, opt := range opts {
		opt(&lo)
	}

	pool := l.lookupPool(name, lo.poolName)

	var (
		err error
	)

	pool.mu.Lock()
	defer pool.mu.Unlock()

	win := pool.availableWindow()
	if win != nil {
		start, err := win.container.cpuUsage()
		if err != nil {
			return nil, err
		}

		l.Log.Info("adding lease to existing window", "window", win.Id)

		lc := &LeasedContainer{
			lc:        l,
			Pool:      pool,
			StartTime: time.Now(),
			Window:    win,
			Start:     start,
		}

		win.TotalLeases++
		win.Leases.Add(lc)

		l.rif.Add(1)

		return lc, nil
	}

	// No windows, but we might still have a container kicking around
	// we can reuse.
	rc := pool.availableIdleContainer()
	if rc != nil {
		start, err := rc.cpuUsage()
		if err != nil {
			return nil, err
		}

		pool.idle.Remove(rc)

		l.Log.Debug("beginning new usage window", "app", pool.app.name, "start", start)

		win = &UsageWindow{
			App:         pool.app.name,
			Id:          identity.NewID(),
			Start:       start,
			Leases:      set.New[*LeasedContainer](),
			TotalLeases: 1,

			container: rc,
		}

		lc := &LeasedContainer{
			lc:            l,
			Pool:          pool,
			StartTime:     time.Now(),
			Window:        win,
			Start:         start,
			StartedWindow: true,
		}

		win.Leases.Add(lc)

		l.rif.Add(1)

		return lc, nil
	}

	// If there are any pending containers, find one with room and attach ourselves
	// to it.

	var pendingCh chan *UsageWindow

	if !pool.pending.Empty() {
		for pc := range pool.pending {
			if pc.waiters.Len() < l.MaxLeasesPerContainer {
				// So that the sender doesn't block.
				pendingCh = make(chan *UsageWindow, 1)
				pc.waiters.Add(pendingCh)
				break
			}
		}
	}

	if pendingCh != nil {
		pool.mu.Unlock()

		select {
		case <-ctx.Done():
			pool.mu.Lock()
			return nil, ctx.Err()
		case win, ok := <-pendingCh:
			pool.mu.Lock()

			if !ok {
				return nil, fmt.Errorf("pending container failed")
			}

			lc := &LeasedContainer{
				lc:        l,
				Pool:      pool,
				StartTime: time.Now(),
				Window:    win,
				Start:     win.Start,
			}

			win.Leases.Add(lc)
			l.rif.Add(1)

			return lc, nil
		}
	}

	// ok, we need to launch a container.

	ac, err := l.AppAccess.LoadApp(ctx, pool.app.name)
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

	pool.pending.Add(pc)
	defer pool.pending.Remove(pc)

	winId := identity.NewID()
	l.Log.Info("launching container", "app", ac.Name, "pool", lo.poolName, "version", mrv.Version, "window", winId)

	pool.mu.Unlock()
	rc, err = l.launch(ctx, ac, mrv, &lo)
	pool.mu.Lock()

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
		App:         pool.app.name,
		Id:          winId,
		Start:       0,
		Leases:      set.New[*LeasedContainer](),
		TotalLeases: 1,

		container: rc,
	}

	pool.windows.Add(win)

	lc := &LeasedContainer{
		lc:            l,
		Pool:          pool,
		StartTime:     time.Now(),
		Window:        win,
		Start:         win.Start,
		StartedWindow: true,
	}

	win.Leases.Add(lc)
	l.rif.Add(1)

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
	lc.Pool.mu.Lock()
	defer lc.Pool.mu.Unlock()

	lc.Window.Leases.Remove(lc)

	l.lattrack.RecordLatency(l.rif.Load(), time.Since(lc.StartTime).Seconds())

	l.rif.Add(-1)

	ts, err := lc.Window.container.cpuUsage()
	if err != nil {
		return nil, err
	}

	if lc.Window.Leases.Empty() {
		err = l.closeWindow(ctx, lc.Window, ts)
		lc.Pool.idle.Add(lc.Window.container)
		lc.Pool.windows.Remove(lc.Window)
		l.Log.Info("usage window closed", "window", lc.Window.Id, "app", lc.Window.App)
	}

	i := &LeaseInfo{
		Usage: ts - lc.Start,
	}

	return i, nil
}

func (l *LaunchContainer) closeWindow(ctx context.Context, w *UsageWindow, ts uint64) error {
	l.Log.Info("closing window", "window", w.Id, "app", w.App)

	w.End = ts
	w.container.idleSince = time.Now()

	usage := ts - w.Start

	_, err := l.DB.Exec(
		"INSERT INTO container_usage (app, usage, leases) VALUES (?, ?, ?)",
		w.App, usage, w.TotalLeases,
	)

	return err
}

func (l *LaunchContainer) launch(
	ctx context.Context,
	ac *app.AppConfig,
	mrv *app.AppVersion,
	lo *leaseOptions,
) (*runningContainer, error) {
	ec, err := network.AllocateOnBridge(l.Bridge, l.Subnet)
	if err != nil {
		return nil, err
	}

	l.Log.Debug("allocated network endpoint", "bridge", l.Bridge, "addresses", ec.Addresses)

	config := &run.ContainerConfig{
		App:      ac.Name,
		Image:    mrv.ImageName(),
		Endpoint: ec,
	}

	_, err = l.CR.RunContainer(ctx, config)
	if err != nil {
		return nil, err
	}

	if lo.dontWaitNetwork {
		l.Log.Info("not waiting for network", "container", config.Id)
		err = l.Health.WaitReady(ctx, config.Id)
		if err != nil {
			return nil, err
		}
	} else {
		err = l.Health.WaitForReady(ctx, config.Id)
		if err != nil {
			return nil, err
		}
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
		for _, pool := range app.pools {
			pool.mu.Lock()

			var toDelete []*runningContainer

			for rc := range pool.idle {
				idle := time.Since(rc.idleSince)
				if idle >= l.IdleTimeout {
					l.Log.Info("shutting down idle container", "container", rc.id)

					err := l.CR.StopContainer(ctx, rc.id)
					if err != nil {
						l.Log.Error("failed to stop container", "container", rc.id, "error", err)
					} else {
						toDelete = append(toDelete, rc)
					}
				} else {
					l.Log.Debug("skipping idle container, not yet reached idle max", "container", rc.id, "left", l.IdleTimeout-idle)
				}
			}

			cnt += len(toDelete)

			for _, rc := range toDelete {
				pool.idle.Remove(rc)
			}

			pool.mu.Unlock()
		}
	}

	return cnt, nil
}

// RecoverContainers scans containerd for running containers and adds them to the idle pool
func (l *LaunchContainer) RecoverContainers(ctx context.Context) error {
	l.Log.Info("recovering containers", "namespace", l.Namespace)

	ctx = namespaces.WithNamespace(ctx, l.Namespace)

	containers, err := l.CC.Containers(ctx)
	if err != nil {
		return fmt.Errorf("listing containers: %w", err)
	}

	for _, container := range containers {
		labels, err := container.Labels(ctx)
		if err != nil {
			l.Log.Warn("failed to get labels for container", "container", container.ID(), "error", err)
			continue
		}

		appName := labels["miren.dev/app"]
		if appName == "" {
			l.Log.Warn("container missing app label", "container", container.ID())
			continue
		}

		poolName := labels["miren.dev/pool"]
		if appName == "" {
			l.Log.Warn("container missing app label", "container", container.ID())
			continue
		}

		spec, err := container.Spec(ctx)
		if err != nil {
			l.Log.Warn("failed to get container spec", "container", container.ID(), "error", err)
			continue
		}

		// Check if container is actually running
		task, err := container.Task(ctx, nil)
		if err != nil {
			l.Log.Debug("container has no task", "container", container.ID())
			continue
		}

		status, err := task.Status(ctx)
		if err != nil {
			l.Log.Warn("failed to get task status", "container", container.ID(), "error", err)
			continue
		}

		if status.Status != client.Running {
			l.Log.Debug("container not running", "container", container.ID(), "status", status.Status)
			continue
		}

		pool := l.lookupPool(appName, poolName)

		rc := &runningContainer{
			id:          container.ID(),
			cpuStatPath: filepath.Join("/sys/fs/cgroup", spec.Linux.CgroupsPath, "cpu.stat"),
			idleSince:   time.Now(), // We assume recovered containers are idle
			windows:     set.New[*UsageWindow](),
		}

		pool.mu.Lock()
		pool.idle.Add(rc)
		l.Log.Info("recovered container", "container", container.ID(), "app", appName)
		pool.mu.Unlock()
	}

	return nil
}

func (l *LaunchContainer) Shutdown(ctx context.Context) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	// TODO: we're only shutting down idle containers. Add logic to pick
	// up non-idle containers from containerd on start.

	var cnt int

	for _, app := range l.applications {
		for _, pool := range app.pools {
			pool.mu.Lock()

			var toDelete []*runningContainer

			for rc := range pool.idle {
				idle := time.Since(rc.idleSince)

				l.Log.Info("shutting down idle container", "container", rc.id, "idle", idle)

				err := l.CR.StopContainer(ctx, rc.id)
				if err != nil {
					l.Log.Error("failed to stop container", "container", rc.id, "error", err)
				} else {
					toDelete = append(toDelete, rc)
				}
			}

			cnt += len(toDelete)

			for _, rc := range toDelete {
				pool.idle.Remove(rc)
			}

			pool.mu.Unlock()
		}
	}

	return cnt, nil
}
