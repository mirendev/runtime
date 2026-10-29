package routes

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/net/http/httpguts"
	core "miren.dev/runtime/api/core"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/api/ingress"
	"miren.dev/runtime/api/ingress/ingress_v1alpha"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/types"
	"miren.dev/runtime/pkg/rpc"
)

func clusterOnly(ctx context.Context) error {
	if rpc.BoundApp(ctx) != "" {
		return fmt.Errorf("%w: default routes require cluster-level access", rpc.ErrUnauthorized)
	}
	return nil
}

func (s *Server) app(ctx context.Context, name string) (*core_v1alpha.App, error) {
	if !rpc.AllowApp(ctx, name) {
		return nil, rpc.AppAccessError(ctx, name)
	}
	if name == "" || strings.Contains(name, "/") {
		return nil, fmt.Errorf("an app name is required")
	}
	var app core_v1alpha.App
	record, err := s.IC.GetEntityStore().GetWithEntity(ctx, name, &app)
	if err != nil {
		return nil, err
	}
	if !app.Is(record.Entity()) {
		return nil, fmt.Errorf("%q is not an app", name)
	}
	var owner core_v1alpha.Metadata
	owner.Decode(record.Entity())
	if !rpc.AllowApp(ctx, owner.Name) {
		return nil, rpc.AppAccessError(ctx, owner.Name)
	}
	return &app, nil
}

func (s *Server) owner(ctx context.Context, r *ingress_v1alpha.HttpRoute) (string, error) {
	var app core_v1alpha.App
	record, err := s.IC.GetEntityStore().GetByIdWithEntity(ctx, r.App, &app)
	if err != nil {
		if errors.Is(err, cond.ErrNotFound{}) {
			return "", nil
		}
		return "", err
	}
	if app.ID != r.App || !app.Is(record.Entity()) {
		return "", nil
	}
	var meta core_v1alpha.Metadata
	meta.Decode(record.Entity())
	return meta.Name, nil
}

func (s *Server) authorize(ctx context.Context, r *ingress_v1alpha.HttpRoute) error {
	if r.Default {
		if err := clusterOnly(ctx); err != nil {
			return err
		}
	}
	name, err := s.owner(ctx, r)
	if err != nil {
		return err
	}
	if !rpc.AllowApp(ctx, name) {
		return rpc.AppAccessError(ctx, name)
	}
	return nil
}

// Load and authorize the same snapshot whose revision guards the mutation.
// Re-reading inside a domain-client mutation would separate ownership from OCC.
func (s *Server) load(ctx context.Context, host string, def bool) (*ingress_v1alpha.HttpRoute, *entityserver_v1alpha.Entity, error) {
	if def {
		if err := clusterOnly(ctx); err != nil {
			return nil, nil, err
		}
		if host != "" {
			return nil, nil, fmt.Errorf("default_route cannot be combined with a hostname")
		}
	}
	route, _, err := s.resolve(ctx, host, def)
	if err != nil {
		return nil, nil, err
	}
	var current ingress_v1alpha.HttpRoute
	record, err := s.IC.GetEntityStore().GetByIdWithEntity(ctx, route.ID, &current)
	if err != nil {
		return nil, nil, err
	}
	if current.ID != route.ID || !current.Is(record.Entity()) {
		return nil, nil, cond.NotFound("route", route.ID)
	}
	if err := s.authorize(ctx, &current); err != nil {
		return nil, nil, err
	}
	return &current, record, nil
}

func (s *Server) replace(ctx context.Context, r *ingress_v1alpha.HttpRoute, record *entityserver_v1alpha.Entity) error {
	// Preserve metadata and extension attributes as well as untouched settings.
	e := record.Entity()
	var previous ingress_v1alpha.HttpRoute
	previous.Decode(e)
	for _, attr := range previous.Encode() {
		e.Remove(attr.ID)
	}
	if err := e.Update(r.Encode()); err != nil {
		return err
	}
	_, err := s.IC.GetEntityStore().EAC().Replace(ctx, e.Attrs(), record.Revision())
	return err
}

func (s *Server) info(ctx context.Context, r *ingress_v1alpha.HttpRoute, record *entityserver_v1alpha.Entity) (*ingress_v1alpha.RouteInfo, error) {
	name, err := s.owner(ctx, r)
	if err != nil {
		return nil, err
	}
	out := &ingress_v1alpha.RouteInfo{}
	out.SetHost(r.Host)
	out.SetApp(name)
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
	if record != nil {
		out.SetCreatedAt(record.CreatedAt())
		out.SetUpdatedAt(record.UpdatedAt())
	}
	out.SetProtected(r.AuthProvider != "")
	if r.AuthProvider != "" {
		var oidc ingress_v1alpha.OidcProvider
		var password ingress_v1alpha.PasswordProvider
		p, err := s.IC.GetEntityStore().EAC().Get(ctx, r.AuthProvider.String())
		if err != nil && !errors.Is(err, cond.ErrNotFound{}) {
			return nil, err
		}
		if err == nil {
			oidc.Decode(p.Entity().Entity())
			password.Decode(p.Entity().Entity())
		}
		if oidc.Name != "" {
			out.SetProvider(oidc.Name)
			out.SetProviderType("oidc")
			out.SetProviderUrl(oidc.ProviderUrl)
			if oidc.ConnectorType != "" && oidc.ConnectorType != "oidc" {
				out.SetProviderType("connector")
				out.SetConnectorType(oidc.ConnectorType)
			}
		} else if password.Name != "" {
			out.SetProvider(password.Name)
			out.SetProviderType("password")
		}
	}
	mappings := make([]*ingress_v1alpha.RouteClaimMapping, 0, len(r.ClaimMappings))
	for _, m := range r.ClaimMappings {
		v := &ingress_v1alpha.RouteClaimMapping{}
		v.SetClaim(m.Claim)
		v.SetHeader(m.Header)
		mappings = append(mappings, v)
	}
	out.SetClaimMappings(mappings)
	out.SetWafEnabled(r.WafProfile != "")
	if r.WafProfile != "" {
		profile, err := s.IC.GetWAFProfileByID(ctx, r.WafProfile)
		if err != nil {
			return nil, err
		}
		if profile != nil {
			out.SetWafLevel(profile.ParanoiaLevel)
		}
	}
	return out, nil
}

func (s *Server) List(ctx context.Context, state *ingress_v1alpha.RoutesList) error {
	filter := state.Args().App()
	if filter != "" && !rpc.AllowApp(ctx, filter) {
		return rpc.AppAccessError(ctx, filter)
	}
	if filter == "" {
		filter = rpc.BoundApp(ctx)
	}
	routes, err := s.IC.GetEntityStore().List(ctx, entity.Ref(entity.EntityKind, ingress_v1alpha.KindHttpRoute))
	if err != nil {
		return err
	}
	items := make([]*ingress_v1alpha.RouteInfo, 0, routes.Length())
	for routes.Next() {
		var route ingress_v1alpha.HttpRoute
		if err := routes.Read(&route); err != nil {
			return err
		}
		r := &route
		if r.Default && rpc.BoundApp(ctx) != "" {
			continue
		}
		name, err := s.owner(ctx, r)
		if err != nil {
			return err
		}
		if filter != "" && name != filter {
			continue
		}
		out, err := s.info(ctx, r, nil)
		if err != nil {
			return err
		}
		out.SetCreatedAt(routes.Entity().GetCreatedAt().UnixMilli())
		out.SetUpdatedAt(routes.Entity().GetUpdatedAt().UnixMilli())
		items = append(items, out)
	}
	state.Results().SetRoutes(items)
	return nil
}

func (s *Server) Show(ctx context.Context, state *ingress_v1alpha.RoutesShow) error {
	r, record, err := s.load(ctx, state.Args().Host(), state.Args().DefaultRoute())
	if err != nil {
		return err
	}
	out, err := s.info(ctx, r, record)
	if err == nil {
		state.Results().SetRoute(out)
	}
	return err
}

func (s *Server) Set(ctx context.Context, state *ingress_v1alpha.RoutesSet) error {
	a := state.Args()
	app, err := s.app(ctx, a.App())
	if err != nil {
		return err
	}
	host := strings.ToLower(a.Host())
	if host == "" || strings.ContainsAny(host, "/?#: \t\r\n") || (strings.Contains(host, "*") && !strings.HasPrefix(host, "*.")) {
		return fmt.Errorf("invalid route hostname")
	}
	if err := ingress.ValidateWildcardHost(host); err != nil {
		return err
	}
	service := a.Service()
	if service == "" {
		service = "web"
	}
	if app.ActiveVersion == "" {
		return fmt.Errorf("app has no active configuration")
	}
	var version core_v1alpha.AppVersion
	if err := s.IC.GetEntityStore().GetById(ctx, app.ActiveVersion, &version); err != nil {
		return err
	}
	spec, err := core.ResolveRuntimeConfig(ctx, s.IC.GetEntityStore().EAC(), &version)
	if err != nil {
		return err
	}
	if err := ingress.HTTPService(spec, service); err != nil {
		return err
	}
	r, err := s.IC.Lookup(ctx, host)
	if err != nil {
		return err
	}
	if r == nil {
		if rpc.BoundApp(ctx) != "" {
			return fmt.Errorf("%w: claiming a new hostname requires cluster-level access", rpc.ErrUnauthorized)
		}
		r = &ingress_v1alpha.HttpRoute{Host: host, App: app.ID, Service: service}
		err = s.create(ctx, host, r)
	} else {
		var record *entityserver_v1alpha.Entity
		r, record, err = s.load(ctx, host, false)
		if err == nil {
			r.App, r.Service = app.ID, service
			err = s.replace(ctx, r, record)
		}
	}
	if err != nil {
		return err
	}
	out, err := s.info(ctx, r, nil)
	if err == nil {
		state.Results().SetRoute(out)
	}
	return err
}

func (s *Server) create(ctx context.Context, name string, r *ingress_v1alpha.HttpRoute) error {
	attrs := entity.New(r.Encode, (&core_v1alpha.Metadata{Name: name}).Encode,
		entity.Ident, types.Keyword("http_route/"+name)).Attrs()
	res, err := s.IC.GetEntityStore().EAC().Create(ctx, attrs)
	if err == nil {
		r.ID = entity.Id(res.Id())
	}
	return err
}

func (s *Server) Remove(ctx context.Context, state *ingress_v1alpha.RoutesRemove) error {
	r, record, err := s.load(ctx, state.Args().Host(), false)
	if err != nil {
		return err
	}
	_, err = s.IC.GetEntityStore().EAC().DeleteAtRevision(ctx, r.ID.String(), record.Revision())
	return err
}

func (s *Server) SetDefault(ctx context.Context, state *ingress_v1alpha.RoutesSetDefault) error {
	if err := clusterOnly(ctx); err != nil {
		return err
	}
	app, err := s.app(ctx, state.Args().App())
	if err != nil {
		return err
	}
	s.defaults.Lock()
	defer s.defaults.Unlock()
	r, err := s.IC.LookupDefault(ctx)
	if err != nil {
		return err
	}
	if r == nil {
		r = &ingress_v1alpha.HttpRoute{App: app.ID, Default: true}
		err = s.create(ctx, "default-"+app.ID.String(), r)
	} else {
		var record *entityserver_v1alpha.Entity
		r, record, err = s.load(ctx, "", true)
		if err == nil {
			r.App = app.ID
			err = s.replace(ctx, r, record)
		}
	}
	if err != nil {
		return err
	}
	out, err := s.info(ctx, r, nil)
	if err == nil {
		state.Results().SetRoute(out)
	}
	return err
}

func (s *Server) UnsetDefault(ctx context.Context, state *ingress_v1alpha.RoutesUnsetDefault) error {
	if err := clusterOnly(ctx); err != nil {
		return err
	}
	s.defaults.Lock()
	defer s.defaults.Unlock()
	r, err := s.IC.LookupDefault(ctx)
	if err != nil || r == nil {
		return err
	}
	r, record, err := s.load(ctx, "", true)
	if err != nil {
		return err
	}
	name, err := s.owner(ctx, r)
	if err != nil {
		return err
	}
	_, err = s.IC.GetEntityStore().EAC().DeleteAtRevision(ctx, r.ID.String(), record.Revision())
	if err == nil {
		state.Results().SetChanged(true)
		state.Results().SetApp(name)
	}
	return err
}

func (s *Server) Protect(ctx context.Context, state *ingress_v1alpha.RoutesProtect) error {
	s.IC.ProviderMu.Lock()
	defer s.IC.ProviderMu.Unlock()

	a := state.Args()
	r, record, err := s.load(ctx, a.Host(), a.DefaultRoute())
	if err != nil {
		return err
	}
	if a.Provider() == "" {
		return fmt.Errorf("provider is required")
	}
	oidc, err := s.IC.GetOIDCProvider(ctx, a.Provider())
	if err != nil {
		return err
	}
	password, err := s.IC.GetPasswordProvider(ctx, a.Provider())
	if err != nil {
		return err
	}
	if oidc != nil && password != nil {
		return fmt.Errorf("ambiguous provider name")
	}
	r.ClaimMappings = nil
	if oidc != nil {
		r.AuthProvider = oidc.ID
		for _, m := range a.ClaimMappings() {
			if m == nil || strings.TrimSpace(m.Claim()) == "" || !httpguts.ValidHeaderFieldName(m.Header()) || !httpguts.ValidHeaderFieldValue(m.Claim()) {
				return fmt.Errorf("invalid claim mapping")
			}
			r.ClaimMappings = append(r.ClaimMappings, ingress_v1alpha.ClaimMappings{Claim: m.Claim(), Header: m.Header()})
		}
	} else if password != nil {
		if len(a.ClaimMappings()) != 0 && !a.IgnorePasswordClaimMappings() {
			return fmt.Errorf("--claim-header is not supported for password providers")
		}
		r.AuthProvider = password.ID
	} else {
		return fmt.Errorf("provider not found: %s", a.Provider())
	}
	if err := s.replace(ctx, r, record); err != nil {
		return err
	}
	out, err := s.info(ctx, r, nil)
	if err == nil {
		state.Results().SetRoute(out)
	}
	return err
}

func (s *Server) Unprotect(ctx context.Context, state *ingress_v1alpha.RoutesUnprotect) error {
	r, record, err := s.load(ctx, state.Args().Host(), state.Args().DefaultRoute())
	if err != nil {
		return err
	}
	r.AuthProvider, r.ClaimMappings = "", nil
	if err := s.replace(ctx, r, record); err != nil {
		return err
	}
	out, err := s.info(ctx, r, nil)
	if err == nil {
		state.Results().SetRoute(out)
	}
	return err
}

func (s *Server) Waf(ctx context.Context, state *ingress_v1alpha.RoutesWaf) error {
	a := state.Args()
	r, record, err := s.load(ctx, a.Host(), a.DefaultRoute())
	if err != nil {
		return err
	}
	if a.Level() < 0 || a.Level() > 4 {
		return fmt.Errorf("WAF level must be between 0 and 4")
	}
	r.WafProfile = ""
	if a.Level() != 0 {
		profile, err := s.IC.CreateWAFProfile(ctx, int(a.Level()))
		if err != nil {
			return err
		}
		r.WafProfile = profile.ID
	}
	if err := s.replace(ctx, r, record); err != nil {
		return err
	}
	out, err := s.info(ctx, r, nil)
	if err == nil {
		state.Results().SetRoute(out)
	}
	return err
}

func (s *Server) Timeout(ctx context.Context, state *ingress_v1alpha.RoutesTimeout) error {
	a := state.Args()
	r, record, err := s.load(ctx, a.Host(), a.DefaultRoute())
	if err != nil {
		return err
	}
	if a.Timeout() != "" {
		d, err := time.ParseDuration(a.Timeout())
		if err != nil || d <= 0 {
			return fmt.Errorf("request timeout must be a positive duration")
		}
	}
	r.RequestTimeout = a.Timeout()
	if err := s.replace(ctx, r, record); err != nil {
		return err
	}
	out, err := s.info(ctx, r, nil)
	if err == nil {
		state.Results().SetRoute(out)
	}
	return err
}

func (s *Server) TlsCheck(ctx context.Context, state *ingress_v1alpha.RoutesTlsCheck) error {
	a := state.Args()
	r, record, err := s.load(ctx, a.Host(), false)
	if err != nil {
		return err
	}
	if a.Path() != "" {
		if err := ingress.ValidateTLSCheckPath(a.Path()); err != nil {
			return err
		}
	}
	r.TlsCheck = a.Path()
	if err := s.replace(ctx, r, record); err != nil {
		return err
	}
	out, err := s.info(ctx, r, nil)
	if err == nil {
		state.Results().SetRoute(out)
	}
	return err
}
