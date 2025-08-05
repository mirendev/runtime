package activator

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/netip"
	"sync"
	"time"

	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/types"
	"miren.dev/runtime/pkg/idgen"
	"miren.dev/runtime/pkg/rpc/stream"
)

type Lease struct {
	ver     *core_v1alpha.AppVersion
	sandbox *compute_v1alpha.Sandbox
	ent     *entity.Entity
	pool    string

	Size int
	URL  string
}

func (l *Lease) Version() *core_v1alpha.AppVersion {
	return l.ver
}

func (l *Lease) Sandbox() *compute_v1alpha.Sandbox {
	return l.sandbox
}

func (l *Lease) SandboxEntity() *entity.Entity {
	return l.ent
}

func (l *Lease) Pool() string {
	return l.pool
}

type AppActivator interface {
	AcquireLease(ctx context.Context, ver *core_v1alpha.AppVersion, pool, service string) (*Lease, error)
	ReleaseLease(ctx context.Context, lease *Lease) error
	RenewLease(ctx context.Context, lease *Lease) (*Lease, error)
}

type sandbox struct {
	sandbox     *compute_v1alpha.Sandbox
	ent         *entity.Entity
	lastRenewal time.Time
	url         string
	maxSlots    int
	inuseSlots  int
}

type verSandboxes struct {
	ver       *core_v1alpha.AppVersion
	sandboxes []*sandbox

	leaseSlots int
}

type verKey struct {
	ver, pool string
}

type localActivator struct {
	mu       sync.Mutex
	versions map[verKey]*verSandboxes

	log *slog.Logger
	eac *entityserver_v1alpha.EntityAccessClient
}

var _ AppActivator = (*localActivator)(nil)

func NewLocalActivator(ctx context.Context, log *slog.Logger, eac *entityserver_v1alpha.EntityAccessClient) AppActivator {
	la := &localActivator{
		log:      log.With("module", "activator"),
		eac:      eac,
		versions: make(map[verKey]*verSandboxes),
	}

	// Recover existing sandboxes on startup
	if err := la.recoverSandboxes(ctx); err != nil {
		la.log.Error("failed to recover sandboxes", "error", err)
	}

	go la.InBackground(ctx)

	return la
}

func (a *localActivator) AcquireLease(ctx context.Context, ver *core_v1alpha.AppVersion, pool, service string) (*Lease, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	key := verKey{ver.ID.String(), pool}
	vs, ok := a.versions[key]

	if !ok {
		a.log.Info("creating new sandbox for app", "app", ver.App, "version", ver.Version, "pool", pool, "key", key)
		return a.activateApp(ctx, ver, pool, service)
	}

	if len(vs.sandboxes) == 0 {
		a.log.Info("no sandboxes available, creating new sandbox for app", "app", ver.App, "version", ver.Version)
	} else {

		a.log.Debug("reusing existing sandboxes", "app", ver.App, "version", ver.Version, "sandboxes", len(vs.sandboxes))

		start := rand.Int() % len(vs.sandboxes)

		for i := 0; i < len(vs.sandboxes); i++ {
			s := vs.sandboxes[(start+i)%len(vs.sandboxes)]
			if s.inuseSlots+vs.leaseSlots < s.maxSlots {
				s.inuseSlots += vs.leaseSlots
				s.lastRenewal = time.Now()

				a.log.Debug("reusing sandbox", "app", ver.App, "version", ver.Version, "sandbox", s.sandbox.ID, "in-use", s.inuseSlots)
				return &Lease{
					ver:     ver,
					sandbox: s.sandbox,
					ent:     s.ent,
					pool:    pool,
					Size:    vs.leaseSlots,
					URL:     s.url,
				}, nil
			}
		}

		// NOTE: We could attempt to fulfill a lease of 1 slot, but if we're getting to the bottom
		// of what the sandboxes can fulfill, it's best to just boot a new sandbox anyway.

		a.log.Info("no space in existing sandboxes, creating new sandbox for app", "app", ver.App, "version", ver.Version)
	}

	return a.activateApp(ctx, ver, pool, service)
}

func (a *localActivator) activateApp(ctx context.Context, ver *core_v1alpha.AppVersion, pool, service string) (*Lease, error) {
	gr, err := a.eac.Get(ctx, ver.App.String())
	if err != nil {
		return nil, err
	}

	var app core_v1alpha.App
	app.Decode(gr.Entity().Entity())

	var appMD core_v1alpha.Metadata
	appMD.Decode(gr.Entity().Entity())

	var sb compute_v1alpha.Sandbox
	sb.Version = app.ActiveVersion

	sb.LogEntity = app.EntityId().String()
	sb.LogAttribute = types.LabelSet("stage", "app-run", "pool", pool, "service", service)

	// Determine port from config or default to 3000
	port := int64(3000)
	if ver.Config.Port > 0 {
		port = ver.Config.Port
	}

	appCont := compute_v1alpha.Container{
		Name:  "app",
		Image: ver.ImageUrl,
		Env: []string{
			"MIREN_APP=" + appMD.Name,
			"MIREN_VERSION=" + ver.Version,
		},
		Directory: "/app",
		Port: []compute_v1alpha.Port{
			{
				Port: port,
				Name: "http",
				Type: "http",
			},
		},
	}

	for _, x := range ver.Config.Variable {
		appCont.Env = append(appCont.Env, x.Key+"="+x.Value)
	}

	for _, s := range ver.Config.Commands {
		if s.Service == service && s.Command != "" {
			if ver.Config.Entrypoint != "" {
				appCont.Command = ver.Config.Entrypoint + " " + s.Command
			} else {
				appCont.Command = s.Command
			}
			break
		}
	}

	sb.Container = append(sb.Container, appCont)

	name := idgen.GenNS("sb")

	a.log.Debug("creating sandbox", "app", ver.App, "sandbox", name, "command", appCont.Command)

	var rpcE entityserver_v1alpha.Entity
	rpcE.SetAttrs(entity.Attrs(
		(&core_v1alpha.Metadata{
			Name:   name,
			Labels: types.LabelSet("app", appMD.Name, "pool", pool),
		}).Encode,
		entity.Ident, "sandbox/"+name,
		sb.Encode,
	))

	pr, err := a.eac.Put(ctx, &rpcE)
	if err != nil {
		return nil, err
	}

	a.log.Debug("created sandbox", "app", ver.App, "sandbox", pr.Id())

	var (
		runningSB compute_v1alpha.Sandbox
		sbEnt     *entity.Entity
	)

	a.log.Debug("watching sandbox", "app", ver.App, "sandbox", pr.Id())

	a.eac.WatchEntity(ctx, pr.Id(), stream.Callback(func(op *entityserver_v1alpha.EntityOp) error {
		var sb compute_v1alpha.Sandbox

		if op.HasEntity() {
			en := op.Entity().Entity()
			sb.Decode(en)

			if sb.Status == compute_v1alpha.RUNNING {
				runningSB = sb
				sbEnt = en
				// TODO figure out a better way to signal that we're done with the watch.
				return io.EOF
			}
		}

		return nil
	}))

	if runningSB.Status != compute_v1alpha.RUNNING {
		return nil, err
	}

	// Parse the address to extract just the IP from potential CIDR notation
	ip := runningSB.Network[0].Address
	if prefix, err := netip.ParsePrefix(ip); err == nil {
		// New format: extract IP from CIDR
		ip = prefix.Addr().String()
	} else if _, err := netip.ParseAddr(ip); err != nil {
		// Not a valid IP either, return error
		return nil, fmt.Errorf("invalid address format: %s", ip)
	}
	// If it's already a plain IP (old format), use as-is
	addr := fmt.Sprintf("http://%s:%d", ip, port)

	var leaseSlots int

	if ver.Config.Concurrency.Fixed <= 0 {
		leaseSlots = 1
	} else {
		leaseSlots = int(float32(ver.Config.Concurrency.Fixed) * 0.20)

		if leaseSlots < 1 {
			leaseSlots = 1
		}
	}

	lsb := &sandbox{
		sandbox:     &runningSB,
		ent:         sbEnt,
		lastRenewal: time.Now(),
		url:         addr,
		maxSlots:    int(ver.Config.Concurrency.Fixed),
		inuseSlots:  leaseSlots,
	}

	lease := &Lease{
		ver:     ver,
		sandbox: lsb.sandbox,
		ent:     lsb.ent,
		pool:    pool,
		Size:    leaseSlots,
		URL:     lsb.url,
	}

	key := verKey{ver.ID.String(), pool}

	vs, ok := a.versions[key]
	if !ok {
		vs = &verSandboxes{
			ver:        ver,
			sandboxes:  []*sandbox{},
			leaseSlots: leaseSlots,
		}
		a.versions[key] = vs
	}

	vs.sandboxes = append(vs.sandboxes, lsb)

	return lease, nil
}

func (a *localActivator) ReleaseLease(ctx context.Context, lease *Lease) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	vs, ok := a.versions[verKey{lease.ver.ID.String(), lease.pool}]
	if !ok {
		return nil
	}

	for _, s := range vs.sandboxes {
		if s.sandbox == lease.sandbox {
			s.inuseSlots -= lease.Size
			break
		}
	}

	return nil
}

func (a *localActivator) RenewLease(ctx context.Context, lease *Lease) (*Lease, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	vs, ok := a.versions[verKey{lease.ver.ID.String(), lease.pool}]
	if !ok {
		return nil, fmt.Errorf("lease not found")
	}

	for _, s := range vs.sandboxes {
		if s.sandbox == lease.sandbox {
			s.lastRenewal = time.Now()
			break
		}
	}

	return lease, nil
}

func (a *localActivator) InBackground(ctx context.Context) {
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			a.retireUnusedSandboxes()
		case <-ctx.Done():
			return
		}
	}
}

// getLabel extracts a label value from metadata, returning defaultValue if not found
func getLabel(metadata *core_v1alpha.Metadata, key string, defaultValue string) string {
	for _, label := range metadata.Labels {
		if label.Key == key {
			return label.Value
		}
	}
	return defaultValue
}

func (a *localActivator) recoverSandboxes(ctx context.Context) error {
	// List all sandboxes
	resp, err := a.eac.List(ctx, entity.Ref(entity.EntityKind, compute_v1alpha.KindSandbox))
	if err != nil {
		return fmt.Errorf("failed to list sandboxes: %w", err)
	}

	a.log.Info("recovering sandboxes on startup", "count", len(resp.Values()))

	recoveryTime := time.Now()

	for _, ent := range resp.Values() {
		var sb compute_v1alpha.Sandbox
		sb.Decode(ent.Entity())

		// Only recover running sandboxes
		if sb.Status != compute_v1alpha.RUNNING {
			continue
		}

		// Skip sandboxes without a version reference
		if sb.Version.String() == "" {
			continue
		}

		// Get the app version details
		verResp, err := a.eac.Get(ctx, sb.Version.String())
		if err != nil {
			a.log.Error("failed to get app version", "version", sb.Version, "error", err)
			continue
		}

		var appVer core_v1alpha.AppVersion
		appVer.Decode(verResp.Entity().Entity())

		// Extract pool from sandbox labels - default to "default" if not found
		var metadata core_v1alpha.Metadata
		metadata.Decode(ent.Entity())
		pool := getLabel(&metadata, "pool", "default")

		// Calculate the URL
		port := int64(3000)
		if appVer.Config.Port > 0 {
			port = appVer.Config.Port
		}

		// Skip if no network address assigned yet
		if len(sb.Network) == 0 || sb.Network[0].Address == "" {
			a.log.Debug("skipping sandbox without network address", "sandbox", sb.ID)
			continue
		}

		// Parse the address to extract just the IP from potential CIDR notation
		ipAddr := sb.Network[0].Address
		if prefix, err := netip.ParsePrefix(ipAddr); err == nil {
			// New format: extract IP from CIDR
			ipAddr = prefix.Addr().String()
		} else if _, err := netip.ParseAddr(ipAddr); err != nil {
			// Not a valid IP either, skip this sandbox
			a.log.Error("invalid address format", "address", ipAddr, "sandbox", sb.ID)
			continue
		}
		// If it's already a plain IP (old format), use as-is
		addr := fmt.Sprintf("http://%s:%d", ipAddr, port)

		// Calculate lease slots
		var leaseSlots int
		if appVer.Config.Concurrency.Fixed <= 0 {
			leaseSlots = 1
		} else {
			leaseSlots = int(float32(appVer.Config.Concurrency.Fixed) * 0.20)
			if leaseSlots < 1 {
				leaseSlots = 1
			}
		}

		// Create sandbox tracking entry
		lsb := &sandbox{
			sandbox:     &sb,
			ent:         ent.Entity(),
			lastRenewal: recoveryTime, // Set to now to give grace period
			url:         addr,
			maxSlots:    int(appVer.Config.Concurrency.Fixed),
			inuseSlots:  0, // Start with no slots in use
		}

		// Add to versions map
		key := verKey{appVer.ID.String(), pool}
		vs, ok := a.versions[key]
		if !ok {
			vs = &verSandboxes{
				ver:        &appVer,
				sandboxes:  []*sandbox{},
				leaseSlots: leaseSlots,
			}
			a.versions[key] = vs
		}
		vs.sandboxes = append(vs.sandboxes, lsb)

		a.log.Info("recovered sandbox", "app", appVer.App, "version", appVer.Version, "sandbox", sb.ID, "pool", pool)
	}

	return nil
}

func (a *localActivator) retireUnusedSandboxes() {
	a.mu.Lock()
	defer a.mu.Unlock()

	for _, vs := range a.versions {
		var newSandboxes []*sandbox

		for _, sb := range vs.sandboxes {
			if time.Since(sb.lastRenewal) > 2*time.Minute {
				a.log.Debug("retiring unused sandbox", "app", vs.ver.App, "sandbox", sb.sandbox.ID)

				if sb.sandbox.Status != compute_v1alpha.RUNNING {
					continue
				}

				sb.sandbox.Status = compute_v1alpha.STOPPED

				var rpcE entityserver_v1alpha.Entity

				rpcE.SetId(sb.sandbox.ID.String())

				rpcE.SetAttrs(entity.Attrs(
					(&compute_v1alpha.Sandbox{
						Status: compute_v1alpha.STOPPED,
					}).Encode,
				))

				_, err := a.eac.Put(context.Background(), &rpcE)
				if err != nil {
					a.log.Error("failed to retire sandbox", "error", err)
				}
			} else {
				newSandboxes = append(newSandboxes, sb)
			}
		}

		vs.sandboxes = newSandboxes
	}
}
