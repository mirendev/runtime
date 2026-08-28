package ingress

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/api/ingress/ingress_v1alpha"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/rpc"
)

// Client provides a domain-specific client for HttpRoute entities
type Client struct {
	log *slog.Logger
	ec  *entityserver.Client
	eac *entityserver_v1alpha.EntityAccessClient
}

// NewClient creates a new Ingress client from an RPC client
func NewClient(log *slog.Logger, client rpc.Client) *Client {
	eac := entityserver_v1alpha.NewEntityAccessClient(client)
	entityClient := entityserver.NewClient(log, eac)

	return &Client{
		log: log.With("module", "ingress-client"),
		ec:  entityClient,
		eac: eac,
	}
}

// GetEntityStore returns the underlying entity store
func (c *Client) GetEntityStore() *entityserver.Client {
	return c.ec
}

// Lookup finds an http_route by hostname, returns nil if not found
func (c *Client) Lookup(ctx context.Context, host string) (*ingress_v1alpha.HttpRoute, error) {
	ia := entity.String(ingress_v1alpha.HttpRouteHostId, strings.ToLower(host))

	var route ingress_v1alpha.HttpRoute
	err := c.ec.OneAtIndex(ctx, ia, &route)
	if err != nil {
		if errors.Is(err, cond.ErrNotFound{}) {
			return nil, nil
		} else {
			return nil, fmt.Errorf("failed to lookup route for host %s: %w", host, err)
		}
	}

	return &route, nil
}

// LookupWithWildcard finds an http_route by hostname with wildcard fallback.
// It tries in order: exact match, then wildcard subdomain (*.rest).
// A wildcard like *.example.com matches foo.example.com but not example.com itself.
func (c *Client) LookupWithWildcard(ctx context.Context, host string) (*ingress_v1alpha.HttpRoute, error) {
	host = strings.ToLower(host)

	// Step 1: exact match
	route, err := c.Lookup(ctx, host)
	if err != nil {
		return nil, err
	}
	if route != nil {
		return route, nil
	}

	// Step 2: replace first label with wildcard (e.g., foo.example.com → *.example.com)
	if idx := strings.Index(host, "."); idx > 0 {
		wildcard := "*" + host[idx:]
		route, err = c.Lookup(ctx, wildcard)
		if err != nil {
			return nil, err
		}
		if route != nil {
			return route, nil
		}
	}

	return nil, nil
}

// IsWildcardHost reports whether host is a wildcard route pattern (e.g. *.example.com).
func IsWildcardHost(host string) bool {
	return strings.HasPrefix(strings.ToLower(host), "*.")
}

// ValidateWildcardHost validates a wildcard host pattern.
// Valid patterns: *.example.com, *.sub.example.com
// Invalid: *.com, foo.*.com, **, *
func ValidateWildcardHost(host string) error {
	if !strings.HasPrefix(host, "*.") {
		return nil
	}
	remainder := host[2:]
	if remainder == "" || strings.Contains(remainder, "*") {
		return fmt.Errorf("invalid wildcard pattern: %s (must be *.domain.tld)", host)
	}
	if !strings.Contains(remainder, ".") {
		return fmt.Errorf("invalid wildcard pattern: %s (must have at least two domain labels after *)", host)
	}
	return nil
}

// ExtractSubdomainLabel extracts an ephemeral label from a request host
// by comparing it against the route's configured host pattern. For example,
// if requestHost is "feat-x.app.example.com" and the route host is
// "*.app.example.com", it returns "feat-x". Returns an empty string if
// the route is not a wildcard or if there's no subdomain prefix.
func ExtractSubdomainLabel(requestHost, routeHost string) string {
	requestHost = strings.ToLower(requestHost)
	routeHost = strings.ToLower(routeHost)

	if !strings.HasPrefix(routeHost, "*.") {
		return ""
	}

	// routeHost is "*.base.example.com", base is "base.example.com"
	base := routeHost[2:]

	if !strings.HasSuffix(requestHost, "."+base) {
		return ""
	}

	// Extract the prefix: "feat-x.app.example.com" minus ".app.example.com"
	label := requestHost[:len(requestHost)-len(base)-1]

	// Only return single-label prefixes (no dots)
	if strings.Contains(label, ".") {
		return ""
	}

	return label
}

// LookupDefault finds the default http_route
func (c *Client) LookupDefault(ctx context.Context) (*ingress_v1alpha.HttpRoute, error) {
	var route ingress_v1alpha.HttpRoute
	err := c.ec.OneAtIndex(ctx, entity.Bool(ingress_v1alpha.HttpRouteDefaultId, true), &route)
	if err != nil {
		if errors.Is(err, cond.ErrNotFound{}) {
			return nil, nil
		} else {
			return nil, fmt.Errorf("failed to lookup default route: %w", err)
		}
	}
	return &route, nil
}

// SetDefault sets the default route to the provided app
func (c *Client) SetDefault(ctx context.Context, appId entity.Id) (*ingress_v1alpha.HttpRoute, error) {
	// Since host is blank for default routes, and it's normally used for the ID field, we make a special ID format
	routeId := fmt.Sprintf("default-%s", appId)

	route := &ingress_v1alpha.HttpRoute{
		ID:      entity.Id(routeId),
		App:     appId,
		Default: true,
	}
	if _, err := c.ec.CreateOrUpdate(ctx, routeId, route); err != nil {
		return nil, fmt.Errorf("failed to create default route: %w", err)
	}

	return route, nil
}

// UnsetDefault unsets the default route, if any. It returns the route that it unset the default from.
func (c *Client) UnsetDefault(ctx context.Context) (*ingress_v1alpha.HttpRoute, error) {
	route, err := c.LookupDefault(ctx)
	if err != nil {
		return nil, err
	}

	if route == nil {
		return nil, nil
	}

	if err := c.ec.Delete(ctx, route.ID); err != nil {
		return nil, fmt.Errorf("failed to delete default route: %w", err)
	}

	return route, nil
}

// EnsureSingleDefault removes any default routes but the one specified
func (c *Client) EnsureSingleDefault(ctx context.Context, routeToKeep *ingress_v1alpha.HttpRoute) error {
	resp, err := c.ec.List(ctx, entity.Bool(ingress_v1alpha.HttpRouteDefaultId, true))
	if err != nil {
		return fmt.Errorf("failed to list default routes: %w", err)
	}

	for resp.Next() {
		var route ingress_v1alpha.HttpRoute
		if err := resp.Read(&route); err != nil {
			c.log.Error("Failed to read route", "error", err)
			continue
		}

		// Skip the route we want to keep as default
		if route.ID == routeToKeep.ID {
			continue
		}

		c.log.Info("Deleting old default route", "route", route.ID)
		if err := c.ec.Delete(ctx, route.ID); err != nil {
			return fmt.Errorf("failed to delete old default route %s: %w", route.ID, err)
		}
	}

	return nil
}

// RouteWithMeta includes an http_route with its metadata
type RouteWithMeta struct {
	Route     *ingress_v1alpha.HttpRoute
	CreatedAt int64
	UpdatedAt int64
}

// List returns all http_routes with metadata
func (c *Client) List(ctx context.Context) ([]*RouteWithMeta, error) {
	kindRes, err := c.eac.LookupKind(ctx, "http_route")
	if err != nil {
		return nil, fmt.Errorf("failed to lookup http_route kind: %w", err)
	}

	res, err := c.eac.List(ctx, kindRes.Attr())
	if err != nil {
		return nil, fmt.Errorf("failed to list routes: %w", err)
	}

	var routes []*RouteWithMeta
	for _, e := range res.Values() {
		var route ingress_v1alpha.HttpRoute
		route.Decode(e.Entity())
		routes = append(routes, &RouteWithMeta{
			Route:     &route,
			CreatedAt: e.CreatedAt(),
			UpdatedAt: e.UpdatedAt(),
		})
	}

	return routes, nil
}

// HTTPService validates that service exists and exposes an HTTP port. Legacy
// scalar ports default to HTTP, matching deployment-time resolution.
func HTTPService(spec *core_v1alpha.ConfigSpec, service string) error {
	for _, svc := range spec.Services {
		if svc.Name != service {
			continue
		}
		if len(svc.Ports) == 0 {
			// The deployment launcher gives a web service with no port declaration
			// its historical HTTP default of port 3000.
			if service == "web" && svc.Port == 0 {
				return nil
			}
			if svc.Port > 0 && (svc.PortType == "" || svc.PortType == "http") {
				return nil
			}
		} else {
			for _, port := range svc.Ports {
				if port.Type == "http" {
					return nil
				}
			}
		}
		return fmt.Errorf("app service %q has no HTTP port", service)
	}
	return fmt.Errorf("app service %q does not exist in the active configuration", service)
}

// SetRoute creates or updates an http_route for the given host, app, and service.
// An empty service is retained as empty so routes written before service selection
// continue to decode and route to web.
func (c *Client) SetRoute(ctx context.Context, host string, appId entity.Id, services ...string) (*ingress_v1alpha.HttpRoute, error) {
	service := ""
	if len(services) > 0 {
		service = services[0]
	}
	route := &ingress_v1alpha.HttpRoute{
		Host:    strings.ToLower(host),
		App:     appId,
		Service: service,
	}

	// Use the host as the route name/ID
	_, err := c.ec.CreateOrUpdate(ctx, host, route)
	if err != nil {
		return nil, fmt.Errorf("failed to create/update route: %w", err)
	}

	return route, nil
}

// DeleteByHost deletes an http_route by hostname
func (c *Client) DeleteByHost(ctx context.Context, host string) error {
	route, err := c.Lookup(ctx, host)
	if err != nil {
		return err
	}

	if route == nil {
		return fmt.Errorf("route not found: %s", host)
	}

	if err := c.ec.Delete(ctx, route.ID); err != nil {
		return fmt.Errorf("failed to delete route: %w", err)
	}

	return nil
}

// CreateOrUpdateOIDCProvider creates or updates an OIDC provider
func (c *Client) CreateOrUpdateOIDCProvider(ctx context.Context, provider *ingress_v1alpha.OidcProvider) (*ingress_v1alpha.OidcProvider, error) {
	if provider.Name == "" {
		return nil, fmt.Errorf("provider name is required")
	}

	_, err := c.ec.CreateOrUpdate(ctx, provider.Name, provider)
	if err != nil {
		return nil, fmt.Errorf("failed to create/update OIDC provider: %w", err)
	}

	return provider, nil
}

// GetOIDCProvider looks up an OIDC provider by name
func (c *Client) GetOIDCProvider(ctx context.Context, name string) (*ingress_v1alpha.OidcProvider, error) {
	ia := entity.String(ingress_v1alpha.OidcProviderNameId, name)

	var provider ingress_v1alpha.OidcProvider
	err := c.ec.OneAtIndex(ctx, ia, &provider)
	if err != nil {
		if errors.Is(err, cond.ErrNotFound{}) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to lookup OIDC provider %s: %w", name, err)
	}

	return &provider, nil
}

// ListOIDCProviders returns all OIDC providers
func (c *Client) ListOIDCProviders(ctx context.Context) ([]*ingress_v1alpha.OidcProvider, error) {
	kindRes, err := c.eac.LookupKind(ctx, "oidc_provider")
	if err != nil {
		return nil, fmt.Errorf("failed to lookup oidc_provider kind: %w", err)
	}

	res, err := c.eac.List(ctx, kindRes.Attr())
	if err != nil {
		return nil, fmt.Errorf("failed to list OIDC providers: %w", err)
	}

	var providers []*ingress_v1alpha.OidcProvider
	for _, e := range res.Values() {
		var provider ingress_v1alpha.OidcProvider
		provider.Decode(e.Entity())
		providers = append(providers, &provider)
	}

	return providers, nil
}

// DeleteOIDCProvider deletes an OIDC provider by name
func (c *Client) DeleteOIDCProvider(ctx context.Context, name string) error {
	provider, err := c.GetOIDCProvider(ctx, name)
	if err != nil {
		return err
	}

	if provider == nil {
		return fmt.Errorf("OIDC provider not found: %s", name)
	}

	if err := c.ec.Delete(ctx, provider.ID); err != nil {
		return fmt.Errorf("failed to delete OIDC provider: %w", err)
	}

	return nil
}

// AttachAuthProviderToRoute associates an auth provider with an already-resolved route.
// The providerID should be the entity ID of either an OIDC or password provider.
func (c *Client) AttachAuthProviderToRoute(ctx context.Context, route *ingress_v1alpha.HttpRoute, providerID entity.Id, claimMappings []ingress_v1alpha.ClaimMappings) (*ingress_v1alpha.HttpRoute, error) {
	return c.mutateAndReplaceRoute(ctx, route.EntityId(), func(r *ingress_v1alpha.HttpRoute) {
		r.AuthProvider = providerID
		r.ClaimMappings = claimMappings
	})
}

// DetachAuthProviderFromRoute removes auth provider association from a route
func (c *Client) DetachAuthProviderFromRoute(ctx context.Context, route *ingress_v1alpha.HttpRoute) (*ingress_v1alpha.HttpRoute, error) {
	return c.mutateAndReplaceRoute(ctx, route.EntityId(), func(r *ingress_v1alpha.HttpRoute) {
		r.AuthProvider = ""
		r.ClaimMappings = nil
	})
}

// SetRouteMaintenance puts a route into maintenance. The router serves a
// holding page for the route until the state is cleared.
//
// reason and backAt always take effect. startedAt and startedBy are a proposal:
// they are recorded only if the route is not already in maintenance, so
// revising the reason mid-window leaves the original opener and start time
// alone.
//
// That decision happens here, inside the read-modify-write, rather than in the
// caller. A caller deciding it from its own earlier read can be wrong by the
// time the write lands: two operators opening a window at once would both see
// "not in maintenance", and the second write would replace the first operator's
// stamp with its own.
func (c *Client) SetRouteMaintenance(ctx context.Context, route *ingress_v1alpha.HttpRoute, reason, backAt, startedAt, startedBy string) (*ingress_v1alpha.HttpRoute, error) {
	return c.mutateAndReplaceRoute(ctx, route.EntityId(), func(r *ingress_v1alpha.HttpRoute) {
		m := ingress_v1alpha.Maintenance{
			Reason:    reason,
			BackAt:    backAt,
			StartedAt: startedAt,
			StartedBy: startedBy,
		}

		if !r.Maintenance.Empty() {
			m.StartedAt = r.Maintenance.StartedAt
			m.StartedBy = r.Maintenance.StartedBy
		}

		r.Maintenance = m
	})
}

// ClearRouteMaintenance returns a route to normal serving. The whole component
// is dropped, so the reason and operator recorded on entry don't linger on a
// route that's serving again.
func (c *Client) ClearRouteMaintenance(ctx context.Context, route *ingress_v1alpha.HttpRoute) (*ingress_v1alpha.HttpRoute, error) {
	return c.mutateAndReplaceRoute(ctx, route.EntityId(), func(r *ingress_v1alpha.HttpRoute) {
		r.Maintenance = ingress_v1alpha.Maintenance{}
	})
}

// mutateAndReplaceRoute performs a read-modify-write on a route entity.
// It fetches the latest version from the store, applies the mutate function,
// and replaces the entity at the current revision. This avoids overwriting
// concurrent changes that a stale route instance would miss.
func (c *Client) mutateAndReplaceRoute(ctx context.Context, routeID entity.Id, mutate func(*ingress_v1alpha.HttpRoute)) (*ingress_v1alpha.HttpRoute, error) {
	gr, err := c.eac.Get(ctx, string(routeID))
	if err != nil {
		return nil, fmt.Errorf("failed to get route entity for replace: %w", err)
	}

	var route ingress_v1alpha.HttpRoute
	route.Decode(gr.Entity().Entity())

	mutate(&route)

	attrs := entity.New(
		route.Encode,
		entity.DBId, routeID,
	).Attrs()

	_, err = c.eac.Replace(ctx, attrs, gr.Entity().Revision())
	if err != nil {
		return nil, fmt.Errorf("failed to replace route entity: %w", err)
	}

	return &route, nil
}

// CreateOrUpdatePasswordProvider creates or updates a password provider
func (c *Client) CreateOrUpdatePasswordProvider(ctx context.Context, provider *ingress_v1alpha.PasswordProvider) (*ingress_v1alpha.PasswordProvider, error) {
	if provider.Name == "" {
		return nil, fmt.Errorf("provider name is required")
	}

	_, err := c.ec.CreateOrUpdate(ctx, provider.Name, provider)
	if err != nil {
		return nil, fmt.Errorf("failed to create/update password provider: %w", err)
	}

	return provider, nil
}

// GetPasswordProvider looks up a password provider by name
func (c *Client) GetPasswordProvider(ctx context.Context, name string) (*ingress_v1alpha.PasswordProvider, error) {
	ia := entity.String(ingress_v1alpha.PasswordProviderNameId, name)

	var provider ingress_v1alpha.PasswordProvider
	err := c.ec.OneAtIndex(ctx, ia, &provider)
	if err != nil {
		if errors.Is(err, cond.ErrNotFound{}) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to lookup password provider %s: %w", name, err)
	}

	return &provider, nil
}

// ListPasswordProviders returns all password providers
func (c *Client) ListPasswordProviders(ctx context.Context) ([]*ingress_v1alpha.PasswordProvider, error) {
	kindRes, err := c.eac.LookupKind(ctx, "password_provider")
	if err != nil {
		return nil, fmt.Errorf("failed to lookup password_provider kind: %w", err)
	}

	res, err := c.eac.List(ctx, kindRes.Attr())
	if err != nil {
		return nil, fmt.Errorf("failed to list password providers: %w", err)
	}

	var providers []*ingress_v1alpha.PasswordProvider
	for _, e := range res.Values() {
		var provider ingress_v1alpha.PasswordProvider
		provider.Decode(e.Entity())
		providers = append(providers, &provider)
	}

	return providers, nil
}

// DeletePasswordProvider deletes a password provider by name
func (c *Client) DeletePasswordProvider(ctx context.Context, name string) error {
	provider, err := c.GetPasswordProvider(ctx, name)
	if err != nil {
		return err
	}

	if provider == nil {
		return fmt.Errorf("password provider not found: %s", name)
	}

	if err := c.ec.Delete(ctx, provider.ID); err != nil {
		return fmt.Errorf("failed to delete password provider: %w", err)
	}

	return nil
}

func (c *Client) CreateWAFProfile(ctx context.Context, level int) (*ingress_v1alpha.WafProfile, error) {
	if level < 1 || level > 4 {
		return nil, fmt.Errorf("paranoia level must be between 1 and 4, got %d", level)
	}

	profile := &ingress_v1alpha.WafProfile{
		ParanoiaLevel: int64(level),
	}

	name := fmt.Sprintf("waf-l%d", level)
	eid, err := c.ec.CreateOrUpdate(ctx, name, profile)
	if err != nil {
		return nil, fmt.Errorf("failed to create WAF profile: %w", err)
	}

	profile.ID = eid
	return profile, nil
}

func (c *Client) GetWAFProfileByID(ctx context.Context, id entity.Id) (*ingress_v1alpha.WafProfile, error) {
	var profile ingress_v1alpha.WafProfile
	err := c.ec.GetById(ctx, id, &profile)
	if err != nil {
		if errors.Is(err, cond.ErrNotFound{}) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to lookup WAF profile: %w", err)
	}

	return &profile, nil
}

func (c *Client) SetRouteWAFLevel(ctx context.Context, host string, level int) (*ingress_v1alpha.HttpRoute, error) {
	route, err := c.Lookup(ctx, host)
	if err != nil {
		return nil, err
	}
	if route == nil {
		return nil, fmt.Errorf("route not found: %s", host)
	}

	return c.SetRouteWAFLevelOnRoute(ctx, route, level)
}

func (c *Client) SetRouteWAFLevelOnRoute(ctx context.Context, route *ingress_v1alpha.HttpRoute, level int) (*ingress_v1alpha.HttpRoute, error) {
	if route == nil {
		return nil, fmt.Errorf("route is required")
	}
	if level < 1 || level > 4 {
		return nil, fmt.Errorf("WAF level must be between 1 and 4, got %d", level)
	}

	profile, err := c.CreateWAFProfile(ctx, level)
	if err != nil {
		return nil, err
	}

	route.WafProfile = profile.ID

	err = c.ec.Update(ctx, route)
	if err != nil {
		return nil, fmt.Errorf("failed to set WAF profile on route: %w", err)
	}

	return route, nil
}

func (c *Client) DetachWAFProfile(ctx context.Context, host string) (*ingress_v1alpha.HttpRoute, error) {
	route, err := c.Lookup(ctx, host)
	if err != nil {
		return nil, err
	}
	if route == nil {
		return nil, fmt.Errorf("route not found: %s", host)
	}

	return c.DetachWAFProfileFromRoute(ctx, route)
}

func (c *Client) DetachWAFProfileFromRoute(ctx context.Context, route *ingress_v1alpha.HttpRoute) (*ingress_v1alpha.HttpRoute, error) {
	if route == nil {
		return nil, fmt.Errorf("route is required")
	}
	err := c.ec.Patch(ctx, route.ID, 0,
		entity.Ref(ingress_v1alpha.HttpRouteWafProfileId, ""),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to detach WAF profile from route: %w", err)
	}

	route.WafProfile = ""
	return route, nil
}

// SetRouteRequestTimeout sets the per-route ingress request timeout override.
// The timeout is stored as a duration string (e.g. "10m") and must parse to a
// positive duration.
func (c *Client) SetRouteRequestTimeout(ctx context.Context, route *ingress_v1alpha.HttpRoute, timeout string) (*ingress_v1alpha.HttpRoute, error) {
	if route == nil {
		return nil, fmt.Errorf("route is required")
	}

	d, err := time.ParseDuration(timeout)
	if err != nil {
		return nil, fmt.Errorf("invalid request timeout %q: %w", timeout, err)
	}
	if d <= 0 {
		return nil, fmt.Errorf("request timeout must be positive, got %q", timeout)
	}

	err = c.ec.Patch(ctx, route.ID, 0,
		entity.String(ingress_v1alpha.HttpRouteRequestTimeoutId, timeout),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to set request timeout on route: %w", err)
	}

	route.RequestTimeout = timeout
	return route, nil
}

// ClearRouteRequestTimeout removes the per-route timeout override, so the route
// falls back to the server-wide http_request_timeout.
func (c *Client) ClearRouteRequestTimeout(ctx context.Context, route *ingress_v1alpha.HttpRoute) (*ingress_v1alpha.HttpRoute, error) {
	if route == nil {
		return nil, fmt.Errorf("route is required")
	}

	err := c.ec.Patch(ctx, route.ID, 0,
		entity.String(ingress_v1alpha.HttpRouteRequestTimeoutId, ""),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to clear request timeout on route: %w", err)
	}

	route.RequestTimeout = ""
	return route, nil
}
