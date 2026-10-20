package commands

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"miren.dev/runtime/api/app"
	"miren.dev/runtime/api/ingress"
	"miren.dev/runtime/api/ingress/ingress_v1alpha"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/ui"
)

// routeRPCClient retains entity-based route management for clusters predating
// the management RPCs. Each call must fail specifically at service/method
// discovery before EntityAccess is even resolved.
func routeRPCClient(ctx *Context) (*routeCompatClient, error) {
	client, err := ctx.RPCClient("dev.miren.runtime/routes")
	if err != nil && !isMissingRPC(err) {
		return nil, err
	}
	return &routeCompatClient{NetworkClient: client, ctx: ctx, resolveErr: err}, nil
}

// Settings commands historically needed only the route entity, not its provider
// or WAF profile. Keep those unrelated reads out of their legacy Show calls.
func routeSettingsRPCClient(ctx *Context) (*routeCompatClient, error) {
	client, err := routeRPCClient(ctx)
	if err != nil {
		return nil, err
	}
	client.settingsOnly = true
	return client, nil
}

type routeCompatClient struct {
	*rpc.NetworkClient
	ctx          *Context
	resolveErr   error
	settingsOnly bool
}

func (c *routeCompatClient) Call(ctx context.Context, method string, args, result any) error {
	err := c.resolveErr
	if err == nil {
		err = requireRPCMethod(ctx, c.NetworkClient, method)
		if err == nil {
			err = c.NetworkClient.Call(ctx, method, args, result)
		}
	}
	if !isMissingRPC(err) {
		return err
	}
	switch method {
	case "list", "show", "set", "remove", "setDefault", "unsetDefault", "protect", "unprotect", "waf", "timeout", "tlsCheck":
	default:
		return err
	}
	client, err := c.ctx.RPCClient("entities")
	if err != nil {
		return err
	}
	defer client.Close()
	legacy := &legacyRoutes{ic: ingress.NewClient(c.ctx.Log, client), ctx: c.ctx, client: client, settingsOnly: c.settingsOnly}
	return rpc.LocalClient(ingress_v1alpha.AdaptRoutes(legacy)).Call(ctx, method, args, result)
}

// Only the management methods are used here; maintenance keeps its original
// RPC path and must never be routed through this compatibility implementation.
type legacyRoutes struct {
	ingress_v1alpha.Routes
	ic           *ingress.Client
	ctx          *Context
	client       rpc.Client
	settingsOnly bool
}

func (s *legacyRoutes) lookup(ctx context.Context, host string, def bool) (*ingress_v1alpha.HttpRoute, error) {
	var r *ingress_v1alpha.HttpRoute
	var err error
	if def {
		r, err = s.ic.LookupDefault(ctx)
	} else {
		r, err = s.ic.Lookup(ctx, host)
	}
	if err != nil {
		return nil, err
	}
	if r == nil {
		if def {
			return nil, fmt.Errorf("no default route configured")
		}
		return nil, fmt.Errorf("route not found: %s", host)
	}
	return r, nil
}

func legacyRouteInfo(r *ingress_v1alpha.HttpRoute) *ingress_v1alpha.RouteInfo {
	out := &ingress_v1alpha.RouteInfo{}
	out.SetHost(r.Host)
	out.SetApp(ui.CleanEntityID(string(r.App)))
	service := r.Service
	if service == "" {
		service = "web"
	}
	out.SetService(service)
	out.SetDefaultRoute(r.Default)
	out.SetRequestTimeout(r.RequestTimeout)
	out.SetTlsCheck(r.TlsCheck)
	out.SetMaintenance(!r.Maintenance.Empty())
	out.SetMaintenanceReason(r.Maintenance.Reason)
	out.SetMaintenanceBackAt(r.Maintenance.BackAt)
	out.SetMaintenanceStartedAt(r.Maintenance.StartedAt)
	out.SetMaintenanceStartedBy(r.Maintenance.StartedBy)
	out.SetProtected(!entity.Empty(r.AuthProvider))
	out.SetWafEnabled(!entity.Empty(r.WafProfile))
	return out
}

func (s *legacyRoutes) info(ctx context.Context, r *ingress_v1alpha.HttpRoute, providers bool) (*ingress_v1alpha.RouteInfo, error) {
	out := legacyRouteInfo(r)
	if !entity.Empty(r.WafProfile) {
		p, err := s.ic.GetWAFProfileByID(ctx, r.WafProfile)
		if err != nil && providers && !errors.Is(err, cond.ErrNotFound{}) {
			return nil, err
		}
		if p != nil {
			out.SetWafLevel(p.ParanoiaLevel)
		}
	}
	if providers && !entity.Empty(r.AuthProvider) {
		record, err := s.ic.GetEntityStore().GetByIdWithEntity(ctx, r.AuthProvider, &ingress_v1alpha.OidcProvider{})
		if err != nil && !errors.Is(err, cond.ErrNotFound{}) {
			return nil, err
		}
		if err == nil {
			raw := record.Entity()
			switch {
			case entity.Is(raw, ingress_v1alpha.KindOidcProvider):
				p := &ingress_v1alpha.OidcProvider{}
				p.Decode(raw)
				out.SetProvider(p.Name)
				out.SetProviderUrl(p.ProviderUrl)
				out.SetProviderType("oidc")
				if p.ConnectorType != "" && p.ConnectorType != "oidc" {
					out.SetProviderType("connector")
					out.SetConnectorType(p.ConnectorType)
				}
			case entity.Is(raw, ingress_v1alpha.KindPasswordProvider):
				p := &ingress_v1alpha.PasswordProvider{}
				p.Decode(raw)
				out.SetProvider(p.Name)
				out.SetProviderType("password")
			}
		}
	}
	var mappings []*ingress_v1alpha.RouteClaimMapping
	for _, m := range r.ClaimMappings {
		v := &ingress_v1alpha.RouteClaimMapping{}
		v.SetClaim(m.Claim)
		v.SetHeader(m.Header)
		mappings = append(mappings, v)
	}
	out.SetClaimMappings(mappings)
	return out, nil
}

func (s *legacyRoutes) List(ctx context.Context, state *ingress_v1alpha.RoutesList) error {
	routes, err := s.ic.List(ctx)
	if err != nil {
		return err
	}
	var out []*ingress_v1alpha.RouteInfo
	for _, r := range routes {
		v, err := s.info(ctx, r.Route, false)
		if err != nil {
			return err
		}
		v.SetCreatedAt(r.CreatedAt)
		v.SetUpdatedAt(r.UpdatedAt)
		out = append(out, v)
	}
	state.Results().SetRoutes(out)
	return nil
}

func (s *legacyRoutes) Show(ctx context.Context, state *ingress_v1alpha.RoutesShow) error {
	r, err := s.lookup(ctx, state.Args().Host(), state.Args().DefaultRoute())
	if err != nil {
		return err
	}
	if s.settingsOnly {
		state.Results().SetRoute(legacyRouteInfo(r))
		return nil
	}
	out, err := s.info(ctx, r, true)
	if err == nil {
		state.Results().SetRoute(out)
	}
	return err
}

func (s *legacyRoutes) Set(ctx context.Context, state *ingress_v1alpha.RoutesSet) error {
	a := state.Args()
	owner, err := app.NewClient(s.ctx.Log, s.client).GetByName(ctx, a.App())
	if err != nil {
		return fmt.Errorf("failed to find app %q: %w", a.App(), err)
	}
	_, err = s.ic.SetRoute(ctx, a.Host(), owner.ID, a.Service())
	return err
}

func (s *legacyRoutes) Remove(ctx context.Context, state *ingress_v1alpha.RoutesRemove) error {
	return s.ic.DeleteByHost(ctx, state.Args().Host())
}

func (s *legacyRoutes) SetDefault(ctx context.Context, state *ingress_v1alpha.RoutesSetDefault) error {
	owner, err := app.NewClient(s.ctx.Log, s.client).GetByName(ctx, state.Args().App())
	if err != nil {
		return fmt.Errorf("failed to find app %q: %w", state.Args().App(), err)
	}
	_, err = s.ic.SetDefault(ctx, owner.ID)
	return err
}

func (s *legacyRoutes) UnsetDefault(ctx context.Context, state *ingress_v1alpha.RoutesUnsetDefault) error {
	r, err := s.ic.UnsetDefault(ctx)
	if err == nil && r != nil {
		state.Results().SetChanged(true)
		state.Results().SetApp(ui.CleanEntityID(string(r.App)))
	}
	return err
}

func (s *legacyRoutes) Protect(ctx context.Context, state *ingress_v1alpha.RoutesProtect) error {
	a := state.Args()
	r, err := s.lookup(ctx, a.Host(), a.DefaultRoute())
	if err != nil {
		return err
	}
	p, err := s.ic.GetOIDCProvider(ctx, a.Provider())
	if err != nil {
		return fmt.Errorf("failed to lookup provider: %w", err)
	}
	var id entity.Id
	var mappings []ingress_v1alpha.ClaimMappings
	out := &ingress_v1alpha.RouteInfo{}
	if p != nil {
		id = p.ID
		out.SetProviderType("oidc")
		if p.ConnectorType != "" && p.ConnectorType != "oidc" {
			out.SetProviderType("connector")
			out.SetConnectorType(p.ConnectorType)
		}
		out.SetClaimMappings(a.ClaimMappings())
		for _, m := range a.ClaimMappings() {
			if strings.TrimSpace(m.Claim()) == "" || strings.TrimSpace(m.Header()) == "" {
				return fmt.Errorf("invalid claim-header mapping format (expected non-empty 'claim:header')")
			}
			mappings = append(mappings, ingress_v1alpha.ClaimMappings{Claim: m.Claim(), Header: m.Header()})
		}
	} else {
		p, err := s.ic.GetPasswordProvider(ctx, a.Provider())
		if err != nil {
			return fmt.Errorf("failed to lookup provider: %w", err)
		}
		if p == nil {
			return fmt.Errorf("provider not found: %s", a.Provider())
		}
		if len(a.ClaimMappings()) > 0 && !a.IgnorePasswordClaimMappings() {
			return fmt.Errorf("--claim-header is not supported for password providers")
		}
		id = p.ID
		out.SetProviderType("password")
	}
	_, err = s.ic.AttachAuthProviderToRoute(ctx, r, id, mappings)
	if err != nil {
		return err
	}
	state.Results().SetRoute(out)
	return nil
}

func (s *legacyRoutes) Unprotect(ctx context.Context, state *ingress_v1alpha.RoutesUnprotect) error {
	r, err := s.lookup(ctx, state.Args().Host(), state.Args().DefaultRoute())
	if err != nil {
		return err
	}
	_, err = s.ic.DetachAuthProviderFromRoute(ctx, r)
	return err
}

func (s *legacyRoutes) Waf(ctx context.Context, state *ingress_v1alpha.RoutesWaf) error {
	a := state.Args()
	r, err := s.lookup(ctx, a.Host(), a.DefaultRoute())
	if err != nil {
		return err
	}
	if a.Level() == 0 {
		_, err = s.ic.DetachWAFProfileFromRoute(ctx, r)
	} else {
		_, err = s.ic.SetRouteWAFLevelOnRoute(ctx, r, int(a.Level()))
	}
	return err
}

func (s *legacyRoutes) Timeout(ctx context.Context, state *ingress_v1alpha.RoutesTimeout) error {
	a := state.Args()
	r, err := s.lookup(ctx, a.Host(), a.DefaultRoute())
	if err != nil {
		return err
	}
	if a.Timeout() == "" {
		r, err = s.ic.ClearRouteRequestTimeout(ctx, r)
	} else {
		r, err = s.ic.SetRouteRequestTimeout(ctx, r, a.Timeout())
	}
	if err != nil {
		return err
	}
	out := &ingress_v1alpha.RouteInfo{}
	out.SetRequestTimeout(r.RequestTimeout)
	state.Results().SetRoute(out)
	return nil
}

func (s *legacyRoutes) TlsCheck(ctx context.Context, state *ingress_v1alpha.RoutesTlsCheck) error {
	a := state.Args()
	r, err := s.lookup(ctx, a.Host(), false)
	if err != nil {
		return err
	}
	if a.Path() == "" {
		r, err = s.ic.ClearRouteTLSCheck(ctx, r)
	} else {
		r, err = s.ic.SetRouteTLSCheck(ctx, r, a.Path())
	}
	if err != nil {
		return err
	}
	out := &ingress_v1alpha.RouteInfo{}
	out.SetTlsCheck(r.TlsCheck)
	state.Results().SetRoute(out)
	return nil
}
