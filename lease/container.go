package lease

import (
	"context"
	"database/sql"
	"fmt"
	"iter"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/pkg/namespaces"
	"miren.dev/runtime/app"
	"miren.dev/runtime/discovery"
	"miren.dev/runtime/health"
	"miren.dev/runtime/pkg/multierror"
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

func (p *pendingContainer) Close() {
	for ch := range p.waiters {
		close(ch)
	}
}

type application struct {
	name string

	mu    sync.Mutex
	pools map[string]*pool
}

func (a *application) Pools() iter.Seq[*pool] {
	return func(yield func(*pool) bool) {
		a.mu.Lock()
		defer a.mu.Unlock()

		for _, p := range a.pools {
			if !yield(p) {
				return
			}
		}
	}
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
		if w.retire {
			continue
		}

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

	a.mu.Lock()
	defer a.mu.Unlock()

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
		l.Log.Info("tracking new pool", "app", app, "pool", name)
	}

	return p
}

type runningContainer struct {
	id          string
	cpuStatPath string

	idleSince time.Time

	image string

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

	Version *app.AppVersion

	container *runningContainer

	// Inidcates tha that the window is for a container version that has
	// been cleared. When this window closes, we won't return the container
	// to the idle pool.
	retire bool
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

	operation := leaseOperation{
		LaunchContainer: l,
		name:            name,
		opts:            lo,
		pool:            pool,
	}

	for {
		lc, err := operation.tryAvailableWindow()
		if err != nil {
			return nil, err
		}

		if lc != nil {
			return lc, nil
		}

		lc, err = operation.tryAvailableIdleContainer()
		if err != nil {
			return nil, err
		}

		if lc != nil {
			return lc, nil
		}

		lc, retry, err := operation.tryPendingContainer(ctx)
		if err != nil {
			return nil, err
		}

		if lc != nil {
			return lc, nil
		}

		if retry {
			continue
		}

		return operation.launchContainer(ctx)
	}
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
		if err != nil {
			return nil, err
		}

		lc.Pool.windows.Remove(lc.Window)

		if lc.Window.retire {
			err := l.CR.StopContainer(ctx, lc.Window.container.id)
			if err != nil {
				l.Log.Error("failed to stop container", "container", lc.Window.container.id, "error", err)
			}
		} else {
			l.Log.Info("returning container to idle pool", "container", lc.Window.container.id)
			lc.Pool.idle.Add(lc.Window.container)
		}

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

	_, err := l.DB.ExecContext(ctx,
		"INSERT INTO container_usage (app, usage, leases) VALUES (?, ?, ?)",
		w.App, usage, w.TotalLeases,
	)

	return err
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

	toSave := set.New[string]()

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

		aa, err := l.AppAccess.LoadApp(ctx, appName)
		if err != nil {
			l.Log.Warn("failed to load app", "app", appName, "error", err)
			continue
		}

		mrv, err := l.AppAccess.MostRecentVersion(ctx, aa)
		if err != nil {
			l.Log.Warn("failed to get most recent version", "app", appName, "error", err)
			continue
		}

		if mrv.Version != labels["miren.dev/version"] {
			l.Log.Warn("container version mismatch", "container", container.ID(), "expected", mrv.Version, "actual", labels["miren.dev/version"])
			continue
		}

		pool := l.lookupPool(appName, poolName)

		img, err := container.Image(ctx)
		if err != nil {
			l.Log.Warn("failed to get container image", "container", container.ID(), "error", err)
			continue
		}

		rc := &runningContainer{
			id:          container.ID(),
			image:       img.Name(),
			cpuStatPath: filepath.Join("/sys/fs/cgroup", spec.Linux.CgroupsPath, "cpu.stat"),
			idleSince:   time.Now(), // We assume recovered containers are idle
			windows:     set.New[*UsageWindow](),
		}

		pool.mu.Lock()
		pool.idle.Add(rc)
		l.Log.Info("recovered container", "container", container.ID(), "app", appName)
		pool.mu.Unlock()

		toSave.Add(container.ID())
	}

	for _, container := range containers {
		if !toSave.Contains(container.ID()) {
			l.Log.Info("stopping unrecovered container", "container", container.ID())
			err := l.CR.NukeContainer(ctx, container.ID())
			if err != nil {
				l.Log.Error("failed to stop container", "container", container.ID(), "error", err)
			}
		}
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

func (l *LaunchContainer) imageInUseInPool(p *pool, image string) (bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for rc := range p.idle {
		if rc.image == image {
			return true, nil
		}
	}

	for w := range p.windows {
		if w.container.image == image {
			return true, nil
		}
	}

	return false, nil
}

func (l *LaunchContainer) ImageInUse(ctx context.Context, image string) (bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	for _, app := range l.applications {
		for pool := range app.Pools() {
			ok, err := l.imageInUseInPool(pool, image)
			if err != nil {
				return true, err
			}

			if ok {
				return true, nil
			}
		}
	}

	l.Log.Debug("image not in use", "image", image)

	return false, nil
}

func (l *LaunchContainer) clearOldInPool(ctx context.Context, pool *pool, imageName string) error {
	pool.mu.Lock()
	defer pool.mu.Unlock()

	var toDelete []*runningContainer

	l.Log.Info("clearing old versions in pool",
		"app", pool.app.name,
		"pool", pool.name,
		"new-image", imageName,
		"windows", len(pool.windows),
		"idle-containers", len(pool.idle),
	)

	for rc := range pool.idle.Each() {
		if rc.image != imageName {
			l.Log.Info("stopping idle container with version",
				"container", rc.id,
				"app", pool.app.name,
				"pool", pool.name,
				"image", imageName)

			err := l.CR.StopContainer(ctx, rc.id)
			if err != nil {
				l.Log.Error("failed to stop container",
					"container", rc.id,
					"error", err)
				return fmt.Errorf("stopping container %s: %w", rc.id, err)
			}

			toDelete = append(toDelete, rc)
		}
	}

	for _, rc := range toDelete {
		pool.idle.Remove(rc)
	}

	for w := range pool.windows.Each() {
		if w.container.image != imageName {
			l.Log.Info("retiring window with version",
				"window", w.Id, "app", pool.app.name, "pool", pool.name, "image", imageName,
			)
			w.retire = true
		}
	}

	return nil
}

func (l *LaunchContainer) findApp(name string) (*application, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()

	app, ok := l.applications[name]
	return app, ok
}

func (l *LaunchContainer) ClearOldVersions(ctx context.Context, cur *app.AppVersion) error {
	imageName := cur.ImageName()
	l.Log.Info("clearing use of older versions",
		"app", cur.App.Name,
		"current", cur.Version, "image", imageName)

	app, ok := l.findApp(cur.App.Name)
	if !ok {
		l.Log.Debug("app not found", "app", cur.App.Name)
		return nil
	}

	var rerr error

	for pool := range app.Pools() {
		err := l.clearOldInPool(ctx, pool, imageName)
		if err != nil {
			l.Log.Error("failed to clear old versions in pool", "app", app.name, "pool", pool.name, "error", err)
			rerr = multierror.Append(rerr, err)
		}
	}

	return rerr
}
