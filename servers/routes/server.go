// Package routes serves operator controls over existing HTTP routes.
//
// Routes are otherwise managed straight through the entity store, so this
// package is deliberately narrow: it exists for the operations where the server
// has to establish something the caller cannot be trusted to assert about
// itself. Maintenance windows record who opened them and when, and both of
// those are only worth reading if the server stamped them.
package routes

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"miren.dev/runtime/api/ingress"
	"miren.dev/runtime/api/ingress/ingress_v1alpha"
	"miren.dev/runtime/pkg/rpc"
)

type Server struct {
	Log *slog.Logger
	IC  *ingress.Client
}

var _ ingress_v1alpha.Routes = (*Server)(nil)

func NewServer(log *slog.Logger, ic *ingress.Client) *Server {
	return &Server{
		Log: log.With("module", "routes"),
		IC:  ic,
	}
}

func (s *Server) SetMaintenance(ctx context.Context, state *ingress_v1alpha.RoutesSetMaintenance) error {
	args := state.Args()

	// Taking a hostname out of service is an operator action on shared
	// infrastructure, not something an app does to itself.
	if app := rpc.BoundApp(ctx); app != "" {
		return rpc.AppAccessError(ctx, app)
	}

	backAt, err := validateBackAt(args.BackAt(), time.Now())
	if err != nil {
		return err
	}

	route, label, err := s.resolve(ctx, args.Host(), args.DefaultRoute())
	if err != nil {
		return err
	}

	maint := ingress_v1alpha.Maintenance{
		Reason:    args.Reason(),
		BackAt:    backAt,
		StartedAt: route.Maintenance.StartedAt,
		StartedBy: route.Maintenance.StartedBy,
	}

	// A repeat call is how an operator revises the reason or pushes out the
	// estimate mid-window. Restamping the start would then misreport how long
	// traffic has actually been held, which is the one number worth having
	// afterwards, so only a route that was serving gets a fresh stamp.
	if route.Maintenance.Empty() {
		maint.StartedAt = time.Now().UTC().Format(time.RFC3339)
		maint.StartedBy = operatorIdentity(ctx)
	}

	if _, err := s.IC.SetRouteMaintenance(ctx, route, maint); err != nil {
		return fmt.Errorf("failed to put route into maintenance: %w", err)
	}

	s.Log.Info("route entered maintenance",
		"route", label, "reason", maint.Reason, "back_at", maint.BackAt, "by", maint.StartedBy)

	results := state.Results()
	results.SetRoute(label)
	results.SetReason(maint.Reason)
	results.SetBackAt(maint.BackAt)
	results.SetStartedAt(maint.StartedAt)
	results.SetStartedBy(maint.StartedBy)

	return nil
}

func (s *Server) ClearMaintenance(ctx context.Context, state *ingress_v1alpha.RoutesClearMaintenance) error {
	args := state.Args()

	if app := rpc.BoundApp(ctx); app != "" {
		return rpc.AppAccessError(ctx, app)
	}

	route, label, err := s.resolve(ctx, args.Host(), args.DefaultRoute())
	if err != nil {
		return err
	}

	changed := !route.Maintenance.Empty()

	if changed {
		if _, err := s.IC.ClearRouteMaintenance(ctx, route); err != nil {
			return fmt.Errorf("failed to bring route out of maintenance: %w", err)
		}

		s.Log.Info("route left maintenance", "route", label)
	}

	results := state.Results()
	results.SetRoute(label)
	results.SetChanged(changed)

	return nil
}

// resolve turns a hostname, or the default-route flag, into the route entity to
// act on plus a label to show the operator.
func (s *Server) resolve(ctx context.Context, host string, isDefault bool) (*ingress_v1alpha.HttpRoute, string, error) {
	if isDefault {
		route, err := s.IC.LookupDefault(ctx)
		if err != nil {
			return nil, "", fmt.Errorf("failed to lookup default route: %w", err)
		}
		if route == nil {
			return nil, "", fmt.Errorf("no default route configured")
		}
		return route, "default", nil
	}

	if host == "" {
		return nil, "", fmt.Errorf("either a hostname or the default route must be specified")
	}

	route, err := s.IC.Lookup(ctx, host)
	if err != nil {
		return nil, "", fmt.Errorf("failed to lookup route: %w", err)
	}
	if route == nil {
		return nil, "", fmt.Errorf("route not found for host: %s", host)
	}

	return route, host, nil
}

// validateBackAt checks the expected-return time the caller sent and normalizes
// it to UTC. It has to parse, and it has to be in the future: a time that has
// already passed puts a stale estimate on the holding page and produces no
// Retry-After at all, and there is no way to arrive at one on purpose.
func validateBackAt(backAt string, now time.Time) (string, error) {
	if backAt == "" {
		return "", nil
	}

	t, err := time.Parse(time.RFC3339, backAt)
	if err != nil {
		return "", fmt.Errorf("back_at must be an RFC 3339 timestamp, got %q", backAt)
	}

	if !t.After(now) {
		return "", fmt.Errorf("back_at must be in the future, got %s", t.UTC().Format(time.RFC3339))
	}

	return t.UTC().Format(time.RFC3339), nil
}

// operatorIdentity names the authenticated caller for the maintenance record.
// It reads the identity the server established rather than anything the client
// sent, which is the whole reason this call is an RPC instead of a direct
// entity write.
func operatorIdentity(ctx context.Context) string {
	identity := rpc.IdentityFromContext(ctx)
	if identity == nil {
		return ""
	}

	return identity.Subject
}
