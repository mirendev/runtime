package usage

import (
	"context"
	"fmt"
	"strings"

	"miren.dev/runtime/api/compute"
	computev1 "miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/usage/usage_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/rpc/standard"
	"miren.dev/runtime/pkg/ui"
)

// sandboxRow is one sandbox with everything needed to describe it, gathered
// before any metric is read.
//
// The entity pass drives the row set and the metric pass only decorates it.
// That ordering matters: a sandbox that is running but reporting nothing has to
// appear with blank usage, because a metrics pipeline that has stopped is
// itself the thing an operator needs to see. Sorting rows by what came back
// from the time-series database would hide exactly that case.
type sandboxRow struct {
	ref    *usage_v1alpha.SandboxRef
	poolID string
}

// directory is the resolved entity view of the cluster: every sandbox, joined
// to the app, service and node that explain it.
type directory struct {
	sandboxes []sandboxRow
	nodes     map[entity.Id]*nodeInfo
}

type nodeInfo struct {
	id         entity.Id
	name       string
	runnerID   string
	role       string
	status     string
	scheduling string
}

// displayName is what an operator would recognize the node by. It falls back
// through the identifiers most-human-first, because an unnamed node is still
// better identified by a truncated runner id than by nothing.
func (n *nodeInfo) displayName() string {
	if n.name != "" {
		return n.name
	}
	if n.runnerID != "" {
		if len(n.runnerID) > 12 {
			return n.runnerID[:12]
		}
		return n.runnerID
	}
	return string(n.id)
}

// filter narrows a listing. Every field is exact-match, and an empty field
// means unconstrained.
type filter struct {
	app     string
	service string
	node    string
	kind    string
	status  string

	// includeSystem admits addon and platform sandboxes to a listing that would
	// otherwise show only app services.
	includeSystem bool

	// includeAddons applies to app rollups only: whether an app's dedicated
	// addons count toward its total.
	includeAddons bool
}

// matches reports whether a sandbox belongs in the listing.
func (f filter) matches(ref *usage_v1alpha.SandboxRef) bool {
	if f.app != "" && ref.App() != f.app {
		return false
	}
	if f.service != "" && ref.Service() != f.service {
		return false
	}
	if f.kind != "" && ref.Kind() != f.kind {
		return false
	}
	if f.status != "" && !strings.EqualFold(ui.CleanStatus(ref.Status()), f.status) {
		return false
	}

	// Addons and one-off runs are the platform's own sandboxes. They are hidden
	// by default so the listing answers "what are my apps doing", but an
	// explicit --kind for one of them is a direct request and overrides that.
	if !f.includeSystem && f.kind == "" {
		switch ref.Kind() {
		case sandboxKindAddon, sandboxKindRun, sandboxKindOther:
			return false
		}
	}

	return true
}

// Sandbox kinds, matching the miren.kind attribute the sandbox controller
// stamps on every metric series.
const (
	sandboxKindApp   = "app"
	sandboxKindAddon = "addon"
	sandboxKindRun   = "run"
	sandboxKindOther = "other"
)

// loadDirectory resolves the entity view of the cluster.
//
// When the caller has narrowed to one node this reads only that node's
// sandboxes, through the schedule index, rather than scanning every sandbox in
// the cluster. That is the difference between a cheap repeated call and an
// expensive one, and a watch loop makes this call every few seconds.
func (s *Server) loadDirectory(ctx context.Context, f filter) (*directory, error) {
	nodes, err := s.loadNodes(ctx)
	if err != nil {
		return nil, err
	}

	var scopeNode *nodeInfo
	if f.node != "" {
		scopeNode = matchNode(nodes, f.node)
		if scopeNode == nil {
			// An unknown node is an empty listing, not an error: the caller may
			// be watching a runner that has just been removed.
			return &directory{nodes: nodes}, nil
		}
	}

	index := entity.Ref(entity.EntityKind, computev1.KindSandbox)
	if scopeNode != nil {
		index = computev1.Index(computev1.KindSandbox, scopeNode.id)
	}

	sandboxes, err := s.EC.List(ctx, index)
	if err != nil {
		return nil, fmt.Errorf("listing sandboxes: %w", err)
	}

	pools, err := s.loadPools(ctx)
	if err != nil {
		return nil, err
	}

	apps, err := s.loadVersionApps(ctx)
	if err != nil {
		return nil, err
	}

	dir := &directory{nodes: nodes}

	for sandboxes.Next() {
		var sb computev1.Sandbox
		if err := sandboxes.Read(&sb); err != nil {
			continue
		}

		ent := sandboxes.Entity()

		var md core_v1alpha.Metadata
		md.Decode(ent)

		var sch computev1.Schedule
		sch.Decode(ent)

		// A dead sandbox is not a row. Listing it would show a sandbox using no
		// resources, which reads as idle rather than as gone.
		if compute.SandboxDead(sb.Status) {
			continue
		}

		poolID, _ := md.Labels.Get("pool")

		dir.sandboxes = append(dir.sandboxes, sandboxRow{
			ref:    s.buildRef(&sb, &md, &sch, ent, pools, apps, nodes),
			poolID: poolID,
		})
	}

	return dir, nil
}

// buildRef assembles one sandbox's identity from the several entities that
// each hold a piece of it.
func (s *Server) buildRef(
	sb *computev1.Sandbox,
	md *core_v1alpha.Metadata,
	sch *computev1.Schedule,
	ent *entity.Entity,
	pools map[string]*poolInfo,
	apps map[string]*appInfo,
	nodes map[entity.Id]*nodeInfo,
) *usage_v1alpha.SandboxRef {
	var ref usage_v1alpha.SandboxRef

	ref.SetSandbox(sb.ID.String())
	ref.SetSandboxShortId(ent.ShortId())
	ref.SetStatus(ui.CleanStatus(string(sb.Status)))
	ref.SetStartedAt(standard.ToTimestamp(ent.GetCreatedAt()))

	poolID, _ := md.Labels.Get("pool")
	ref.SetPool(poolID)

	// Service has two sources. The label is one lookup and is present on every
	// pool-managed sandbox; the pool entity is the fallback for anything that
	// predates it.
	service, _ := md.Labels.Get("service")
	if service == "" {
		if p := pools[poolID]; p != nil {
			service = p.service
		}
	}
	ref.SetService(service)

	// The app name likewise has two sources. The label is the app's name
	// directly; the version reference resolves to the same app but survives a
	// sandbox created without the label.
	appName, _ := md.Labels.Get("app")
	if a := apps[sb.Spec.Version.String()]; a != nil {
		ref.SetAppId(a.id)
		ref.SetVersion(a.version)
		ref.SetVersionShortId(a.shortID)
		if appName == "" {
			appName = ui.CleanEntityID(a.id)
		}
	}
	ref.SetApp(appName)

	ref.SetKind(classifySandbox(sb, md))

	if !entity.Empty(sch.Key.Node) {
		ref.SetNode(sch.Key.Node.String())
		if n := nodes[sch.Key.Node]; n != nil {
			ref.SetNodeName(n.displayName())
			ref.SetRunnerId(n.runnerID)
		}
	}

	return &ref
}

// classifySandbox separates a user's own services from the platform running
// underneath them.
//
// This deliberately mirrors sandboxWorkloadKind in the sandbox controller,
// which stamps the same value onto the metric series. The two must agree, or a
// --kind filter would select different rows than the metrics behind them.
func classifySandbox(sb *computev1.Sandbox, md *core_v1alpha.Metadata) string {
	if _, ok := md.Labels.Get("addon"); ok {
		return sandboxKindAddon
	}
	if _, ok := md.Labels.Get("run"); ok {
		return sandboxKindRun
	}

	var stage string
	for _, lbl := range sb.Spec.LogAttribute {
		switch lbl.Key {
		case "addon":
			return sandboxKindAddon
		case "miren.stage":
			stage = lbl.Value
		}
	}

	switch stage {
	case "run":
		return sandboxKindRun
	case "app-run":
		return sandboxKindApp
	}

	if !entity.Empty(sb.Spec.Version) {
		return sandboxKindApp
	}

	return sandboxKindOther
}

type poolInfo struct {
	service            string
	consecutiveCrashes int32
}

func (s *Server) loadPools(ctx context.Context) (map[string]*poolInfo, error) {
	res, err := s.EC.List(ctx, entity.Ref(entity.EntityKind, computev1.KindSandboxPool))
	if err != nil {
		return nil, fmt.Errorf("listing sandbox pools: %w", err)
	}

	pools := map[string]*poolInfo{}
	for res.Next() {
		var p computev1.SandboxPool
		if err := res.Read(&p); err != nil {
			continue
		}
		pools[p.ID.String()] = &poolInfo{
			service:            p.Service,
			consecutiveCrashes: int32(p.ConsecutiveCrashCount),
		}
	}

	return pools, nil
}

type appInfo struct {
	id      string
	version string
	shortID string
}

// loadVersionApps maps an app version to the app it belongs to, which is how a
// sandbox that carries no app label is still attributed to one.
func (s *Server) loadVersionApps(ctx context.Context) (map[string]*appInfo, error) {
	res, err := s.EC.List(ctx, entity.Ref(entity.EntityKind, core_v1alpha.KindAppVersion))
	if err != nil {
		return nil, fmt.Errorf("listing app versions: %w", err)
	}

	apps := map[string]*appInfo{}
	for res.Next() {
		var v core_v1alpha.AppVersion
		if err := res.Read(&v); err != nil {
			continue
		}
		if entity.Empty(v.App) {
			continue
		}
		apps[v.ID.String()] = &appInfo{
			id:      v.App.String(),
			version: v.Version,
			shortID: res.Entity().ShortId(),
		}
	}

	return apps, nil
}

func (s *Server) loadNodes(ctx context.Context) (map[entity.Id]*nodeInfo, error) {
	res, err := s.EC.List(ctx, entity.Ref(entity.EntityKind, computev1.KindNode))
	if err != nil {
		return nil, fmt.Errorf("listing nodes: %w", err)
	}

	nodes := map[entity.Id]*nodeInfo{}
	for res.Next() {
		var n computev1.Node
		if err := res.Read(&n); err != nil {
			continue
		}

		role, _ := n.Constraints.Get("role")
		if role == "" {
			role = "runner"
		}

		nodes[n.ID] = &nodeInfo{
			id:         n.ID,
			name:       n.Name,
			runnerID:   n.RunnerId,
			role:       role,
			status:     ui.CleanStatus(string(n.Status)),
			scheduling: ui.CleanStatus(string(n.Scheduling)),
		}
	}

	return nodes, nil
}

// matchNode resolves whatever identifier a caller had to hand: the entity id,
// the runner id, or the display name. Operators type the name, scripts hold the
// id, and requiring one form would make the filter useless to the other.
func matchNode(nodes map[entity.Id]*nodeInfo, query string) *nodeInfo {
	for _, n := range nodes {
		if string(n.id) == query || n.runnerID == query || n.name == query || n.displayName() == query {
			return n
		}
	}
	return nil
}
