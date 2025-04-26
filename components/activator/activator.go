package activator

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
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
		log:      log,
		eac:      eac,
		versions: make(map[verKey]*verSandboxes),
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

				a.log.Debug("reusing sandbox", "app", ver.App, "version", ver.Version, "sandbox", s.sandbox.ID, "in-use", s.inuseSlots)
				return &Lease{
					ver:     ver,
					sandbox: s.sandbox,
					ent:     s.ent,
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
				Port: 3000,
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
			Labels: types.LabelSet("app", appMD.Name),
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

	addr := "http://" + runningSB.Network[0].Address + ":3000"

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
