package lease

import (
	"context"
	"log/slog"
	"os"
	"sync"
	"time"

	"miren.dev/runtime/metrics"
	"miren.dev/runtime/pkg/asm/autoreg"
	"miren.dev/runtime/pkg/multierror"
	"miren.dev/runtime/pkg/set"
	"miren.dev/runtime/pkg/units"
)

type containerCPUUsage struct {
	lastReading time.Time
	lastUsage   units.Microseconds
}

type ContainerStatsTracker struct {
	Log      *slog.Logger
	CPUUsage *metrics.CPUUsage
	MemUsage *metrics.MemoryUsage

	mu               sync.Mutex
	activeContainers set.Set[*runningContainer]

	prevUsage map[string]*containerCPUUsage
}

func (c *ContainerStatsTracker) Populated() error {
	c.activeContainers = set.New[*runningContainer]()
	c.prevUsage = make(map[string]*containerCPUUsage)

	return nil
}

var _ = autoreg.Register[ContainerStatsTracker]()

func (c *ContainerStatsTracker) Monitor(ctx context.Context) {
	timer := time.NewTicker(10 * time.Second)

	c.Log.Info("starting container stats tracker")
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			err := c.readActiveContainers(ctx)
			if err != nil {
				c.Log.Error("failed to read active containers", "err", err)
			}
		}
	}
}

func (c *ContainerStatsTracker) retireContainer(ctx context.Context, rc *runningContainer) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	err := c.writeContainerStats(ctx, rc)
	if err != nil {
		return err
	}

	c.activeContainers.Remove(rc)

	return nil
}

func (c *ContainerStatsTracker) activateContainer(rc *runningContainer) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	ts, err := rc.cpuUsage()
	if err != nil {
		return err
	}

	c.prevUsage[rc.id] = &containerCPUUsage{
		lastReading: time.Now(),
		lastUsage:   ts,
	}

	c.activeContainers.Add(rc)

	return nil
}

func (c *ContainerStatsTracker) readActiveContainers(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	var rerr error

	for rc := range c.activeContainers {
		err := c.writeContainerStats(ctx, rc)
		if err != nil {
			rerr = multierror.Append(rerr, err)
		}
	}

	return rerr
}

func (c *ContainerStatsTracker) writeContainerStats(ctx context.Context, rc *runningContainer) error {
	wallEnd := time.Now()

	ts, err := rc.cpuUsage()
	if err != nil {
		return err
	}

	prev, ok := c.prevUsage[rc.id]
	if !ok {
		return nil
	}

	usage := ts - prev.lastUsage
	start := prev.lastReading

	rc.usage += usage

	err = c.CPUUsage.RecordUsage(ctx, rc.app, start, wallEnd, usage)
	if err != nil {
		return err
	}

	prev.lastReading = wallEnd
	prev.lastUsage = ts

	mem, err := c.readMemoryUsage(rc)
	if err != nil {
		c.Log.Error("failed to read memory usage", "err", err)
	} else {
		err = c.MemUsage.RecordUsage(ctx, rc.app, wallEnd, mem)
		if err != nil {
			c.Log.Error("failed to record memory usage", "err", err)
		}
	}

	c.Log.Debug("recorded container stats", "app", rc.app, "usage", usage, "mem", mem)

	return nil
}

func (c *ContainerStatsTracker) readMemoryUsage(rc *runningContainer) (units.Bytes, error) {
	bigbuf := make([]byte, 4096)

	f, err := os.Open(rc.memCurPath)
	if err != nil {
		return 0, err
	}

	defer f.Close()

	n, err := f.Read(bigbuf)
	if err != nil {
		return 0, err
	}

	num, err := parseInt(bigbuf[:n])
	if err != nil {
		return 0, err
	}

	return units.Bytes(num), nil
}
