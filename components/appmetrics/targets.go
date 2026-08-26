package appmetrics

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"sort"
	"strconv"
	"strings"
	"sync"

	"miren.dev/runtime/api/compute/compute_v1alpha"
	coreutil "miren.dev/runtime/api/core"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/indexwatch"
)

type fileSDGroup struct {
	Targets []string          `json:"targets"`
	Labels  map[string]string `json:"labels"`
}

type targetDiscovery struct {
	log       *slog.Logger
	eac       *entityserver_v1alpha.EntityAccessClient
	path      string
	clusterID string

	mu      sync.Mutex
	targets map[string]fileSDGroup
	watcher *indexwatch.Watcher
	wg      sync.WaitGroup
}

func newTargetDiscovery(log *slog.Logger, eac *entityserver_v1alpha.EntityAccessClient, path, clusterID string) *targetDiscovery {
	return &targetDiscovery{
		log:       log.With("module", "app-metrics-targets"),
		eac:       eac,
		path:      path,
		clusterID: clusterID,
		targets:   make(map[string]fileSDGroup),
	}
}

func (d *targetDiscovery) Start(ctx context.Context) error {
	d.watcher = indexwatch.New(
		d.eac,
		entity.Ref(entity.EntityKind, compute_v1alpha.KindSandbox),
		indexwatch.Options{Logger: d.log},
	)
	if err := d.watcher.Start(ctx); err != nil {
		return err
	}

	select {
	case event, ok := <-d.watcher.Updates():
		if !ok {
			return fmt.Errorf("sandbox target watch closed before initial sync")
		}
		if err := d.applyEvent(ctx, event); err != nil {
			return err
		}
	case <-ctx.Done():
		return ctx.Err()
	}

	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		d.run(ctx)
	}()
	return nil
}

func (d *targetDiscovery) Stop() {
	if d.watcher != nil {
		d.watcher.Stop()
	}
	d.wg.Wait()
}

func (d *targetDiscovery) run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-d.watcher.Updates():
			if !ok {
				return
			}
			if err := d.applyEvent(ctx, event); err != nil && ctx.Err() == nil {
				d.log.Warn("failed to update vmagent targets", "event", event.Type.String(), "sandbox", event.Id, "error", err)
			}
		}
	}
}

func (d *targetDiscovery) applyEvent(ctx context.Context, event indexwatch.Event) error {
	switch event.Type {
	case indexwatch.EventSync:
		d.mu.Lock()
		previous := make(map[string]fileSDGroup, len(d.targets))
		for id, target := range d.targets {
			previous[id] = target
		}
		d.mu.Unlock()
		next := make(map[string]fileSDGroup)
		for _, en := range event.Entities {
			var sandbox compute_v1alpha.Sandbox
			sandbox.Decode(en)
			target, eligible, err := d.targetForSandbox(ctx, en)
			if err != nil {
				// A resync proves that the sandbox still exists, but a related
				// entity lookup may fail transiently. Retain its last known target
				// until a later event can make a complete decision.
				if old, ok := previous[sandbox.ID.String()]; ok {
					next[sandbox.ID.String()] = old
				}
				d.log.Warn("failed to derive app metrics target during sync", "sandbox", sandbox.ID, "error", err)
				continue
			}
			if eligible {
				next[sandbox.ID.String()] = target
			}
		}
		d.mu.Lock()
		d.targets = next
		err := d.writeLocked()
		d.mu.Unlock()
		return err

	case indexwatch.EventAdded, indexwatch.EventUpdated:
		if event.Entity == nil {
			return nil
		}
		var sandbox compute_v1alpha.Sandbox
		sandbox.Decode(event.Entity)
		target, eligible, err := d.targetForSandbox(ctx, event.Entity)
		if err != nil {
			// Returning before the map update leaves the existing target untouched.
			// A later sandbox update or watcher resync will converge it.
			return err
		}
		d.mu.Lock()
		if eligible {
			d.targets[sandbox.ID.String()] = target
		} else {
			delete(d.targets, sandbox.ID.String())
		}
		err = d.writeLocked()
		d.mu.Unlock()
		return err

	case indexwatch.EventDeleted:
		d.mu.Lock()
		delete(d.targets, event.Id.String())
		err := d.writeLocked()
		d.mu.Unlock()
		return err
	}
	return nil
}

func (d *targetDiscovery) targetForSandbox(ctx context.Context, en *entity.Entity) (fileSDGroup, bool, error) {
	var sandbox compute_v1alpha.Sandbox
	sandbox.Decode(en)
	if sandbox.Status != compute_v1alpha.RUNNING || len(sandbox.Network) == 0 || sandbox.Spec.Version == "" {
		return fileSDGroup{}, false, nil
	}

	var metadata core_v1alpha.Metadata
	metadata.Decode(en)
	serviceName, ok := metadata.Labels.Get("service")
	if !ok || serviceName == "" {
		return fileSDGroup{}, false, nil
	}

	versionResponse, err := d.eac.Get(ctx, sandbox.Spec.Version.String())
	if err != nil {
		return fileSDGroup{}, false, fmt.Errorf("reading app version %s for sandbox %s: %w", sandbox.Spec.Version, sandbox.ID, err)
	}
	var version core_v1alpha.AppVersion
	version.Decode(versionResponse.Entity().Entity())

	config, err := coreutil.ResolveRuntimeConfig(ctx, d.eac, &version)
	if err != nil {
		return fileSDGroup{}, false, fmt.Errorf("resolving config for sandbox %s: %w", sandbox.ID, err)
	}
	var metrics *core_v1alpha.ConfigSpecServicesMetrics
	for i := range config.Services {
		service := &config.Services[i]
		if service.Name == serviceName {
			metrics = &service.Metrics
			break
		}
	}
	if metrics == nil || !metrics.Enabled {
		return fileSDGroup{}, false, nil
	}
	if metrics.Port == 0 || metrics.Path == "" || metrics.Interval == "" {
		return fileSDGroup{}, false, fmt.Errorf("sandbox %s has unresolved metrics configuration for service %s", sandbox.ID, serviceName)
	}

	appResponse, err := d.eac.Get(ctx, version.App.String())
	if err != nil {
		return fileSDGroup{}, false, fmt.Errorf("reading app %s for sandbox %s: %w", version.App, sandbox.ID, err)
	}
	var appMetadata core_v1alpha.Metadata
	appMetadata.Decode(appResponse.Entity().Entity())

	var schedule compute_v1alpha.Schedule
	schedule.Decode(en)
	if schedule.Key.Node == "" {
		return fileSDGroup{}, false, nil
	}
	nodeResponse, err := d.eac.Get(ctx, schedule.Key.Node.String())
	if err != nil {
		return fileSDGroup{}, false, fmt.Errorf("reading node %s for sandbox %s: %w", schedule.Key.Node, sandbox.ID, err)
	}
	var node compute_v1alpha.Node
	node.Decode(nodeResponse.Entity().Entity())

	ip := strings.SplitN(sandbox.Network[0].Address, "/", 2)[0]
	if net.ParseIP(ip) == nil {
		return fileSDGroup{}, false, fmt.Errorf("sandbox %s has invalid network address %q", sandbox.ID, sandbox.Network[0].Address)
	}

	return fileSDGroup{
		Targets: []string{net.JoinHostPort(ip, strconv.FormatInt(metrics.Port, 10))},
		Labels: map[string]string{
			"__metrics_path__":    metrics.Path,
			"__scrape_interval__": metrics.Interval,
			"miren_app":           appMetadata.Name,
			"miren_app_version":   version.Version,
			"miren_service":       serviceName,
			"miren_sandbox":       sandbox.ID.String(),
			"miren_runner":        node.RunnerId,
			"miren_cluster":       d.clusterID,
		},
	}, true, nil
}

func (d *targetDiscovery) writeLocked() error {
	ids := make([]string, 0, len(d.targets))
	for id := range d.targets {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	targets := make([]fileSDGroup, 0, len(ids))
	for _, id := range ids {
		targets = append(targets, d.targets[id])
	}
	data, err := json.MarshalIndent(targets, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return writeFileAtomic(d.path, data, 0644)
}
