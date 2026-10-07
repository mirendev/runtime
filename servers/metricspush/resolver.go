package metricspush

import (
	"context"
	"fmt"
	"sync"
	"time"

	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
)

// Sandbox is what the runtime knows about a pushing sandbox, and so what its
// samples are labeled with.
type Sandbox struct {
	ID      string
	App     string
	Version string
	Service string
	Runner  string
}

// labels renders the extra_label values for scope. They are the scrape path's
// labels, less the per-sandbox ones at app scope. An empty value is left out
// rather than sent, since vmagent would store it as a real, empty label.
func (s Sandbox) labels(scope Scope, clusterID string) []string {
	pairs := [][2]string{
		{"miren_app", s.App},
		{"miren_service", s.Service},
		{"miren_cluster", clusterID},
	}
	if scope == ScopeSandbox {
		pairs = append(pairs,
			[2]string{"miren_app_version", s.Version},
			[2]string{"miren_sandbox", s.ID},
			[2]string{"miren_runner", s.Runner},
		)
	}

	labels := make([]string, 0, len(pairs))
	for _, p := range pairs {
		if p[1] != "" {
			labels = append(labels, p[0]+"="+p[1])
		}
	}
	return labels
}

// SandboxResolver looks up the sandbox a verified token names.
type SandboxResolver interface {
	Resolve(ctx context.Context, sandboxID string) (Sandbox, error)
}

// resolveCacheTTL bounds how long a lookup is reused. Everything resolved is
// fixed for a sandbox's life except its node, which only changes if the
// sandbox is rescheduled, and a sandbox that moves gets a new ID.
const resolveCacheTTL = 5 * time.Minute

// EntityResolver resolves sandboxes from the entity store, caching each for a
// few minutes so a workload pushing every few seconds costs a handful of
// lookups per sandbox rather than four per push.
type EntityResolver struct {
	eac *entityserver_v1alpha.EntityAccessClient

	mu    sync.Mutex
	cache map[string]cachedSandbox
	now   func() time.Time
}

type cachedSandbox struct {
	sandbox Sandbox
	expires time.Time
}

func NewEntityResolver(eac *entityserver_v1alpha.EntityAccessClient) *EntityResolver {
	return &EntityResolver{
		eac:   eac,
		cache: make(map[string]cachedSandbox),
		now:   time.Now,
	}
}

func (r *EntityResolver) Resolve(ctx context.Context, sandboxID string) (Sandbox, error) {
	now := r.now()
	r.mu.Lock()
	cached, ok := r.cache[sandboxID]
	r.mu.Unlock()
	if ok && now.Before(cached.expires) {
		return cached.sandbox, nil
	}

	sandbox, err := r.lookup(ctx, sandboxID)
	if err != nil {
		return Sandbox{}, err
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	// Sweep on write so sandboxes that stopped pushing do not accumulate.
	for id, c := range r.cache {
		if !now.Before(c.expires) {
			delete(r.cache, id)
		}
	}
	r.cache[sandboxID] = cachedSandbox{sandbox: sandbox, expires: now.Add(resolveCacheTTL)}
	return sandbox, nil
}

// lookup follows the same path the scrape targets do (sandbox, then its
// version, app and node), so a pushed series and a scraped one from the same
// sandbox carry identical labels.
func (r *EntityResolver) lookup(ctx context.Context, sandboxID string) (Sandbox, error) {
	resp, err := r.eac.Get(ctx, sandboxID)
	if err != nil {
		return Sandbox{}, fmt.Errorf("reading sandbox: %w", err)
	}
	en := resp.Entity().Entity()

	var sb compute_v1alpha.Sandbox
	sb.Decode(en)
	if sb.Spec.Version == "" {
		return Sandbox{}, fmt.Errorf("sandbox %s has no app version", sandboxID)
	}

	var metadata core_v1alpha.Metadata
	metadata.Decode(en)
	service, _ := metadata.Labels.Get("service")

	versionResp, err := r.eac.Get(ctx, sb.Spec.Version.String())
	if err != nil {
		return Sandbox{}, fmt.Errorf("reading app version %s: %w", sb.Spec.Version, err)
	}
	var version core_v1alpha.AppVersion
	version.Decode(versionResp.Entity().Entity())

	appResp, err := r.eac.Get(ctx, version.App.String())
	if err != nil {
		return Sandbox{}, fmt.Errorf("reading app %s: %w", version.App, err)
	}
	var appMetadata core_v1alpha.Metadata
	appMetadata.Decode(appResp.Entity().Entity())

	var runner string
	var schedule compute_v1alpha.Schedule
	schedule.Decode(en)
	if schedule.Key.Node != "" {
		nodeResp, err := r.eac.Get(ctx, schedule.Key.Node.String())
		if err != nil {
			return Sandbox{}, fmt.Errorf("reading node %s: %w", schedule.Key.Node, err)
		}
		var node compute_v1alpha.Node
		node.Decode(nodeResp.Entity().Entity())
		runner = node.RunnerId
	}

	return Sandbox{
		ID:      sandboxID,
		App:     appMetadata.Name,
		Version: version.Version,
		Service: service,
		Runner:  runner,
	}, nil
}
