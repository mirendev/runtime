package appmetrics

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"miren.dev/runtime/api/compute/compute_v1alpha"
	coreutil "miren.dev/runtime/api/core"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/indexwatch"
)

// DisabledReporter watches for applications that asked to be scraped while
// the cluster has no remote-write destination. It makes the disabled state
// visible without starting vmagent or touching the private scrape endpoint.
type DisabledReporter struct {
	log     *slog.Logger
	eac     *entityserver_v1alpha.EntityAccessClient
	watcher *indexwatch.Watcher

	mu       sync.Mutex
	resolved map[string]struct{}
	wg       sync.WaitGroup
}

func NewDisabledReporter(log *slog.Logger, eac *entityserver_v1alpha.EntityAccessClient) *DisabledReporter {
	return &DisabledReporter{
		log:      log.With("module", "app-metrics"),
		eac:      eac,
		resolved: make(map[string]struct{}),
	}
}

func (r *DisabledReporter) Start(ctx context.Context) error {
	r.watcher = indexwatch.New(
		r.eac,
		entity.Ref(entity.EntityKind, compute_v1alpha.KindSandbox),
		indexwatch.Options{Logger: r.log},
	)
	if err := r.watcher.Start(ctx); err != nil {
		return err
	}
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		r.run(ctx)
	}()
	return nil
}

func (r *DisabledReporter) Stop() {
	if r.watcher != nil {
		r.watcher.Stop()
	}
	r.wg.Wait()
}

func (r *DisabledReporter) run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-r.watcher.Updates():
			if !ok {
				return
			}
			switch event.Type {
			case indexwatch.EventSync:
				for _, en := range event.Entities {
					r.reportSandbox(ctx, en)
				}
			case indexwatch.EventAdded, indexwatch.EventUpdated:
				r.reportSandbox(ctx, event.Entity)
			case indexwatch.EventDeleted:
				// Resolutions are cached by immutable version and service. A
				// deleted sandbox needs no corresponding state change.
			}
		}
	}
}

func (r *DisabledReporter) reportSandbox(ctx context.Context, en *entity.Entity) {
	if en == nil {
		return
	}
	versionID, service, ok := metricsResolutionInputs(en)
	if !ok {
		return
	}
	key := versionID.String() + "/" + service
	r.mu.Lock()
	_, alreadyResolved := r.resolved[key]
	r.mu.Unlock()
	if alreadyResolved {
		return
	}

	version, enabled, err := enabledMetricsForSandbox(ctx, r.eac, versionID, service)
	if err != nil {
		r.log.Warn("could not inspect sandbox metrics configuration", "error", err)
		return
	}

	r.mu.Lock()
	if _, ok := r.resolved[key]; ok {
		r.mu.Unlock()
		return
	}
	r.resolved[key] = struct{}{}
	r.mu.Unlock()
	if !enabled {
		return
	}

	r.log.Warn("application metrics are enabled but no remote-write destination is configured",
		"version", version.Version,
		"service", service,
	)
}

func metricsResolutionInputs(en *entity.Entity) (entity.Id, string, bool) {
	var sandbox compute_v1alpha.Sandbox
	sandbox.Decode(en)
	if sandbox.Status != compute_v1alpha.RUNNING || sandbox.Spec.Version == "" {
		return "", "", false
	}
	var metadata core_v1alpha.Metadata
	metadata.Decode(en)
	service, ok := metadata.Labels.Get("service")
	if !ok || service == "" {
		return "", "", false
	}
	return sandbox.Spec.Version, service, true
}

func enabledMetricsForSandbox(ctx context.Context, eac *entityserver_v1alpha.EntityAccessClient, versionID entity.Id, service string) (*core_v1alpha.AppVersion, bool, error) {
	response, err := eac.Get(ctx, versionID.String())
	if err != nil {
		return nil, false, fmt.Errorf("reading app version %s: %w", versionID, err)
	}
	var version core_v1alpha.AppVersion
	version.Decode(response.Entity().Entity())
	config, err := coreutil.ResolveRuntimeConfig(ctx, eac, &version)
	if err != nil {
		return nil, false, err
	}
	for _, configuredService := range config.Services {
		if configuredService.Name == service {
			return &version, configuredService.Metrics.Enabled, nil
		}
	}
	return &version, false, nil
}
