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

	Size int
	URL  string
}

func (l *Lease) Version() *core_v1alpha.AppVersion {
	return l.ver
}

func (l *Lease) Sandbox() *compute_v1alpha.Sandbox {
	return l.sandbox
}

type AppActivator interface {
	AcquireLease(ctx context.Context, ver *core_v1alpha.AppVersion) (*Lease, error)
	ReleaseLease(ctx context.Context, lease *Lease) error
	RenewLease(ctx context.Context, lease *Lease) (*Lease, error)
}

type sandbox struct {
	sandbox     *compute_v1alpha.Sandbox
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

type localActivator struct {
	mu       sync.Mutex
	versions map[string]*verSandboxes

	log *slog.Logger
	eac *entityserver_v1alpha.EntityAccessClient
}

var _ AppActivator = (*localActivator)(nil)

func NewLocalActivator(ctx context.Context, log *slog.Logger, eac *entityserver_v1alpha.EntityAccessClient) AppActivator {
	la := &localActivator{
		log:      log,
		eac:      eac,
		versions: make(map[string]*verSandboxes),
	}

	go la.InBackground(ctx)

	return la
}

func (a *localActivator) AcquireLease(ctx context.Context, ver *core_v1alpha.AppVersion) (*Lease, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	vs, ok := a.versions[ver.ID.String()]

	if !ok {
		a.log.Info("creating new sandbox for app", "app", ver.App, "version", ver.Version)
		return a.activateApp(ctx, ver)
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
					Size:    vs.leaseSlots,
					URL:     s.url,
				}, nil
			}
		}

		// NOTE: We could attempt to fulfill a lease of 1 slot, but if we're getting to the bottom
		// of what the sandboxes can fulfill, it's best to just boot a new sandbox anyway.

		a.log.Info("no space in existing sandboxes, creating new sandbox for app", "app", ver.App, "version", ver.Version)
	}

	return a.activateApp(ctx, ver)
}

func (a *localActivator) activateApp(ctx context.Context, ver *core_v1alpha.AppVersion) (*Lease, error) {
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

	sb.Container = append(sb.Container, compute_v1alpha.Container{
		Name:  "app",
		Image: ver.ImageUrl,
		Env: []string{
			"MIREN_APP=" + appMD.Name,
			"MIREN_VERSION=" + ver.Version,
		},
		Port: []compute_v1alpha.Port{
			{
				Port: 80,
				Name: "http",
				Type: "http",
			},
		},
	})

	name := idgen.GenNS("sb")

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

	var runningSB compute_v1alpha.Sandbox

	a.log.Debug("watching sandbox", "app", ver.App, "sandbox", pr.Id())

	a.eac.WatchEntity(ctx, pr.Id(), stream.Callback(func(op *entityserver_v1alpha.EntityOp) error {
		var sb compute_v1alpha.Sandbox

		if op.HasEntity() {
			en := op.Entity().Entity()
			sb.Decode(en)

			if sb.Status == compute_v1alpha.RUNNING {
				runningSB = sb
				// TODO figure out a better way to signal that we're done with the watch.
				return io.EOF
			}
		}

		return nil
	}))

	if runningSB.Status != compute_v1alpha.RUNNING {
		return nil, err
	}

	addr := "http://" + runningSB.Network[0].Address

	var leaseSlots int

	if ver.Concurrency <= 0 {
		leaseSlots = 1
	} else {
		leaseSlots = int(float32(ver.Concurrency) * 0.20)

		if leaseSlots < 1 {
			leaseSlots = 1
		}
	}

	lsb := &sandbox{
		sandbox:     &runningSB,
		lastRenewal: time.Now(),
		url:         addr,
		maxSlots:    int(ver.Concurrency),
		inuseSlots:  leaseSlots,
	}

	lease := &Lease{
		ver:     ver,
		sandbox: lsb.sandbox,
		Size:    leaseSlots,
		URL:     lsb.url,
	}

	vs, ok := a.versions[ver.ID.String()]
	if !ok {
		vs = &verSandboxes{
			ver:        ver,
			sandboxes:  []*sandbox{},
			leaseSlots: leaseSlots,
		}
		a.versions[ver.ID.String()] = vs
	}

	vs.sandboxes = append(vs.sandboxes, lsb)

	return lease, nil
}

func (a *localActivator) ReleaseLease(ctx context.Context, lease *Lease) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	vs, ok := a.versions[lease.ver.ID.String()]
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

	vs, ok := a.versions[lease.ver.ID.String()]
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
