package secret

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/secret"
)

// Secrets an app owns are narrowed to callers that may act on that app. The
// rules, in the order they are applied:
//
//   - An unowned secret is open to every caller that can reach the method,
//     exactly as every secret was before ownership existed.
//   - A caller bound to an app (an OIDC binding or a workload identity) reaches
//     only secrets its own app owns.
//   - Under Cloud authorization, the caller needs read (or configure, for a
//     write) on the owning app.
//
// Anything else, which in practice means runners, internal components and
// operators without Cloud policy, is not narrowed at all. Runners in particular
// resolve every app's secrets to start sandboxes.
//
// No OIDC or workload role grants the secrets interface today, and Cloud policy
// sends secret methods through cluster/admin, so the bound-caller and Cloud
// rules mostly hold the line for whenever those grants arrive. What keeps an
// app from another app's secret right now is the runner, which hands an owned
// secret only to its own app's sandboxes (secret.MaterializeEnv), backed by
// the reference rule in secret.Pin.

const (
	actionRead  = "read"
	actionWrite = "configure"
)

var errOtherAppsSecret = fmt.Errorf("%w: this secret belongs to another app", rpc.ErrUnauthorized)

// narrowed reports whether ownership can change the answer for this caller.
// When it cannot, the checks below skip the store lookups entirely, which
// keeps a runner's resolve on every sandbox start as cheap as it was.
func narrowed(ctx context.Context) bool {
	return rpc.BoundApp(ctx) != "" || rpc.UsesLogicalAuthorization(ctx)
}

// authorizeRef checks the caller against the owner of the secret a reference
// names.
func (s *Server) authorizeRef(ctx context.Context, backend, ref, action string) error {
	if !narrowed(ctx) {
		return nil
	}
	path, _, err := secret.ParseRef(ref)
	if err != nil {
		return err
	}
	return s.authorizePath(ctx, backend, path, action)
}

// authorizePath checks the caller against the owner of the secret at path. A
// secret that does not exist yet has no owner, so a create is allowed through
// and the write itself records the owner.
func (s *Server) authorizePath(ctx context.Context, backend, path, action string) error {
	if !narrowed(ctx) {
		return nil
	}
	owner, err := s.ownerOf(ctx, backend, path)
	if err != nil {
		return err
	}
	return s.authorizeOwner(ctx, owner, action)
}

// ownerOf returns the id of the app that owns a secret, or empty for an unowned
// secret, a missing one, or a backend that does not track ownership. A missing
// secret is left for the operation itself to report.
func (s *Server) ownerOf(ctx context.Context, backend, path string) (string, error) {
	b, ok := s.registry.Get(backend)
	if !ok {
		return "", nil
	}
	owned, ok := b.(secret.OwnedBackend)
	if !ok {
		return "", nil
	}
	owner, err := owned.Owner(ctx, path)
	if err != nil {
		if errors.Is(err, secret.ErrNotFound) {
			return "", nil
		}
		return "", err
	}
	return owner, nil
}

// authorizeOwner checks the caller against an owning app.
func (s *Server) authorizeOwner(ctx context.Context, owner, action string) error {
	if owner == "" || !narrowed(ctx) {
		return nil
	}

	app, err := s.loadOwner(ctx, owner)
	if err != nil {
		return err
	}

	if !rpc.AllowApp(ctx, app.name) {
		return errOtherAppsSecret
	}
	if !rpc.UsesLogicalAuthorization(ctx) {
		return nil
	}
	return rpc.AuthorizeResource(ctx, rpc.ResourceRequest{
		Resource: "app", Action: action, Instance: app.name, Owner: app.ownedBy,
	})
}

type ownerApp struct {
	name    string
	ownedBy string
}

// loadOwner reads the owning app's name and Cloud owner. An owner that no
// longer exists keeps the name derived from its id, so a bound caller still
// fails the comparison rather than slipping through on an empty name.
func (s *Server) loadOwner(ctx context.Context, owner string) (ownerApp, error) {
	res, err := s.eac.Get(ctx, owner)
	if err != nil {
		if errors.Is(err, cond.ErrNotFound{}) {
			return ownerApp{name: strings.TrimPrefix(owner, "app/")}, nil
		}
		return ownerApp{}, fmt.Errorf("loading owner of secret: %w", err)
	}

	ent := res.Entity().Entity()
	var app core_v1alpha.App
	app.Decode(ent)
	var md core_v1alpha.Metadata
	md.Decode(ent)
	return ownerApp{name: md.Name, ownedBy: app.OwnedBy}, nil
}

// ownershipForSet decides which app a write is on behalf of.
//
// A caller that names an app gets a secret that belongs to it, and must be
// allowed to configure that app: a bound caller only its own, and under Cloud
// authorization only apps it holds configure on. Naming an app requires the
// secret to end up owned by it, so a write to an existing unowned secret fails
// rather than quietly leaving it shared.
//
// A bound caller that names none still writes on behalf of its own app, but
// without the requirement, so writing an existing unowned secret leaves it
// unowned, as it was before ownership existed.
func (s *Server) ownershipForSet(ctx context.Context, name string) (secret.Ownership, error) {
	if name == "" {
		id, err := s.boundAppID(ctx)
		return secret.Ownership{App: id}, err
	}

	if bound := rpc.BoundApp(ctx); bound != "" && bound != name {
		return secret.Ownership{}, fmt.Errorf("%w: a caller bound to app %q cannot create secrets for app %q", rpc.ErrUnauthorized, bound, name)
	}

	id, app, err := s.loadApp(ctx, name)
	if err != nil {
		return secret.Ownership{}, err
	}
	if rpc.UsesLogicalAuthorization(ctx) {
		if err := rpc.AuthorizeResource(ctx, rpc.ResourceRequest{
			Resource: "app", Action: actionWrite, Instance: app.name, Owner: app.ownedBy,
		}); err != nil {
			return secret.Ownership{}, err
		}
	}
	return secret.Ownership{App: id, Require: true}, nil
}

// loadApp loads an app by name, failing when it does not exist.
func (s *Server) loadApp(ctx context.Context, name string) (string, ownerApp, error) {
	res, err := s.eac.Get(ctx, "app/"+name)
	if err != nil {
		if errors.Is(err, cond.ErrNotFound{}) {
			return "", ownerApp{}, fmt.Errorf("app %q not found", name)
		}
		return "", ownerApp{}, fmt.Errorf("loading app %q: %w", name, err)
	}
	ent := res.Entity().Entity()
	var app core_v1alpha.App
	app.Decode(ent)
	return res.Entity().Id(), ownerApp{name: name, ownedBy: app.OwnedBy}, nil
}

// boundAppID returns the entity id of the app the caller is bound to, or empty
// for a caller bound to none.
func (s *Server) boundAppID(ctx context.Context) (string, error) {
	bound := rpc.BoundApp(ctx)
	if bound == "" {
		return "", nil
	}
	id, _, err := s.loadApp(ctx, bound)
	return id, err
}

// ownerView resolves owning apps for a listing, loading each at most once: its
// name, for display, and whether the caller may see its secrets at all.
type ownerView struct {
	s    *Server
	seen map[string]ownerVisibility
}

type ownerVisibility struct {
	name    string
	visible bool
}

func newOwnerView(s *Server) *ownerView {
	return &ownerView{s: s, seen: map[string]ownerVisibility{}}
}

// lookup returns the owning app's name and whether the caller may see secrets
// it owns. An unowned secret has no name and is visible to everyone.
func (v *ownerView) lookup(ctx context.Context, owner string) (string, bool, error) {
	if owner == "" {
		return "", true, nil
	}
	if got, ok := v.seen[owner]; ok {
		return got.name, got.visible, nil
	}

	app, err := v.s.loadOwner(ctx, owner)
	if err != nil {
		return "", false, err
	}

	visible := true
	if narrowed(ctx) {
		visible = rpc.AllowApp(ctx, app.name)
		if visible {
			visible, err = rpc.FilterResource(ctx, rpc.ResourceRequest{
				Resource: "app", Action: actionRead, Instance: app.name, Owner: app.ownedBy,
			})
			if err != nil {
				return "", false, err
			}
		}
	}

	v.seen[owner] = ownerVisibility{name: app.name, visible: visible}
	return app.name, visible, nil
}
