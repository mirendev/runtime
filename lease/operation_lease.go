package lease

import (
	"context"
	"path/filepath"
	"time"

	"miren.dev/runtime/app"
	"miren.dev/runtime/network"
	"miren.dev/runtime/pkg/idgen"
	"miren.dev/runtime/pkg/set"
	"miren.dev/runtime/run"
)

type leaseOperation struct {
	*LaunchContainer
	name string
	opts leaseOptions
	pool *pool

	ac  *app.AppConfig
	mrv *app.AppVersion

	pc *pendingContainer
}

func (l *leaseOperation) tryAvailableWindow() (*LeasedContainer, error) {
	l.pool.mu.Lock()
	defer l.pool.mu.Unlock()

	win := l.pool.availableWindow()
	if win != nil {
		start, err := win.container.cpuUsage()
		if err != nil {
			return nil, err
		}

		l.Log.Info("adding lease to existing window", "window", win.Id)

		lc := &LeasedContainer{
			lc:        l.LaunchContainer,
			Pool:      l.pool,
			StartTime: time.Now(),
			Window:    win,
			Start:     start,
		}

		win.TotalLeases++
		win.Leases.Add(lc)

		l.rif.Add(1)

		return lc, nil
	}

	return nil, nil
}

func (l *leaseOperation) tryAvailableIdleContainer() (*LeasedContainer, error) {
	l.pool.mu.Lock()
	defer l.pool.mu.Unlock()

	// No windows, but we might still have a container kicking around
	// we can reuse.
	rc := l.pool.availableIdleContainer()
	if rc != nil {
		start, err := rc.cpuUsage()
		if err != nil {
			return nil, err
		}

		l.pool.idle.Remove(rc)

		l.Log.Debug("beginning new usage window", "app", l.pool.app.name, "pool", l.pool.name, "start", start)

		win := &UsageWindow{
			App:         l.pool.app.name,
			Id:          idgen.Gen("w"),
			Start:       start,
			WallStart:   time.Now(),
			Leases:      set.New[*LeasedContainer](),
			TotalLeases: 1,
			Version:     rc.version,

			maxLeasesPerWindow: rc.maxConcurrency,

			container: rc,
		}

		l.pool.windows.Add(win)

		lc := &LeasedContainer{
			lc:            l.LaunchContainer,
			Pool:          l.pool,
			StartTime:     time.Now(),
			Window:        win,
			Start:         start,
			StartedWindow: true,
		}

		win.Leases.Add(lc)

		l.rif.Add(1)

		return lc, nil
	}

	return nil, nil
}

func (l *leaseOperation) setupPending() (*pendingContainer, chan *UsageWindow) {
	l.pool.mu.Lock()
	defer l.pool.mu.Unlock()

	pool := l.pool

	if !pool.pending.Empty() {
		for pc := range pool.pending {
			if pc.waiters.Len() < l.MaxLeasesPerContainer {
				// So that the sender doesn't block.
				pendingCh := make(chan *UsageWindow, 1)
				pc.waiters.Add(pendingCh)

				return pc, pendingCh
			}
		}
	}

	return nil, nil
}

func (l *leaseOperation) tryPendingContainer(ctx context.Context) (*LeasedContainer, bool, error) {
	// If there are any pending containers, find one with room and attach ourselves
	// to it.

	pc, pendingCh := l.setupPending()

	if pc == nil {
		return nil, false, nil
	}

	defer func() {
		l.pool.mu.Lock()
		defer l.pool.mu.Unlock()
		pc.waiters.Remove(pendingCh)
	}()

	select {
	case <-ctx.Done():
		return nil, false, ctx.Err()
	case win, ok := <-pendingCh:

		if !ok {
			l.Log.Warn("pending container wait failed, retrying lease")
			// I don't love recursing here, but it's rare (famous last words)
			//pool.mu.Unlock()
			//lc, err := l.Lease(ctx, name, opts...)
			//pool.mu.Lock()

			return nil, true, nil
		}

		l.pool.mu.Lock()
		defer l.pool.mu.Unlock()

		lc := &LeasedContainer{
			lc:        l.LaunchContainer,
			Pool:      l.pool,
			StartTime: time.Now(),
			Window:    win,
			Start:     win.Start,
		}

		win.Leases.Add(lc)
		l.rif.Add(1)

		return lc, false, nil
	}
}

func (l *leaseOperation) setupLaunch(ctx context.Context) error {
	l.pool.mu.Lock()
	defer l.pool.mu.Unlock()

	// ok, we need to launch a container.

	pool := l.pool

	ac, err := l.AppAccess.LoadAppByXid(ctx, pool.app.name)
	if err != nil {
		return err
	}

	l.ac = ac

	mrv, err := l.AppAccess.MostRecentVersion(ctx, ac)
	if err != nil {
		return err
	}

	l.mrv = mrv

	pc := &pendingContainer{
		waiters: set.New[chan *UsageWindow](),
	}

	pool.pending.Add(pc)

	l.pc = pc

	return nil
}

func (l *leaseOperation) cleanupLaunch() {
	l.pool.mu.Lock()
	defer l.pool.mu.Unlock()

	if l.pc != nil {
		l.pc.Close()
		l.pool.pending.Remove(l.pc)
	}
}

func (l *leaseOperation) launchContainer(ctx context.Context) (*LeasedContainer, error) {
	err := l.setupLaunch(ctx)
	if err != nil {
		return nil, err
	}
	defer l.cleanupLaunch()

	winId := idgen.Gen("w")
	l.Log.Info("launching container", "app", l.ac.Name, "pool", l.opts.poolName, "version", l.mrv.Version, "window", winId)

	rc, err := l.launch(ctx)

	if err != nil {
		l.Log.Error("failed to launch container", "app", l.ac.Name, "version", l.mrv.Version, "error", err)
		return nil, err
	} else {
		l.Log.Info("launched container", "app", l.ac.Name, "version", l.mrv.Version, "window", winId)
	}

	l.pool.mu.Lock()
	defer l.pool.mu.Unlock()

	win := &UsageWindow{
		App:         l.pool.app.name,
		Id:          winId,
		Start:       0,
		WallStart:   time.Now(),
		Leases:      set.New[*LeasedContainer](),
		TotalLeases: 1,
		Version:     l.mrv.Version,

		maxLeasesPerWindow: l.MaxLeasesPerContainer,

		container: rc,
	}

	if l.mrv.Configuration.HasConcurrency() {
		win.maxLeasesPerWindow = int(l.mrv.Configuration.Concurrency())
	}

	rc.maxConcurrency = win.maxLeasesPerWindow

	l.pool.windows.Add(win)

	lc := &LeasedContainer{
		lc:            l.LaunchContainer,
		Pool:          l.pool,
		StartTime:     time.Now(),
		Window:        win,
		Start:         win.Start,
		StartedWindow: true,
	}

	win.Leases.Add(lc)
	l.rif.Add(1)

loop:
	for ch := range l.pc.waiters {
		select {
		case <-ctx.Done():
			l.Log.Warn("context cancelled while sending window to pending waiters")
			// we've got everything setup already, let's return the lease.
			// defers will cleanup the pending waiters and then can make other arrangements.
			break loop
		case ch <- win:
			// ok
		}
	}

	return lc, nil
}

func (l *leaseOperation) launch(
	ctx context.Context,
) (*runningContainer, error) {
	ec, err := network.AllocateOnBridge(l.Bridge, l.Subnet)
	if err != nil {
		return nil, err
	}

	l.Log.Debug("allocated network endpoint", "bridge", l.Bridge, "addresses", ec.Addresses)

	config := &run.ContainerConfig{
		App:      l.ac.Name,
		Image:    l.mrv.ImageName(),
		Endpoint: ec,
		Env: map[string]string{
			"MIREN_APP":     l.ac.Name,
			"MIREN_POOL":    l.opts.poolName,
			"MIREN_VERSION": l.mrv.Version,
		},
		Labels: map[string]string{
			"miren.dev/pool":    l.pool.name,
			"miren.dev/version": l.mrv.Version,
		},
	}

	for _, nv := range l.mrv.Configuration.EnvVars() {
		config.Env[nv.Key()] = nv.Value()
	}

	_, err = l.CR.RunContainer(ctx, config)
	if err != nil {
		return nil, err
	}

	if l.opts.dontWaitNetwork {
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
		image:       config.Image,
		app:         l.ac.Xid,
		version:     l.mrv.Version,
		cpuStatPath: filepath.Join("/sys/fs/cgroup", config.CGroupPath, "cpu.stat"),
		memCurPath:  filepath.Join("/sys/fs/cgroup", config.CGroupPath, "memory.current"),
	}

	err = l.ConStats.activateContainer(rc)
	if err != nil {
		l.Log.Error("failed to activate container", "container", rc.id, "error", err)
		l.CR.StopContainer(ctx, rc.id)
		return nil, err
	}

	return rc, nil
}
