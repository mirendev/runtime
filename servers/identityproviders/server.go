// Package identityproviders manages cluster-wide ingress identity providers.
package identityproviders

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"golang.org/x/crypto/bcrypt"
	"miren.dev/runtime/api/ingress"
	"miren.dev/runtime/api/ingress/ingress_v1alpha"
	"miren.dev/runtime/pkg/connectors"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/rpc"
)

type Server struct {
	ic *ingress.Client
}

var _ ingress_v1alpha.IdentityProviders = (*Server)(nil)

func NewServer(ic *ingress.Client) *Server { return &Server{ic: ic} }

// Providers control authentication on shared ingress, not an app-owned resource.
// The transport additionally applies cloud RBAC to operator JWTs. Workload
// cluster-admin is deliberately not equivalent to a cluster operator here.
func allowOperator(ctx context.Context) error {
	id := rpc.IdentityFromContext(ctx)
	if id == nil || (id.Method != rpc.AuthMethodCert && id.Method != rpc.AuthMethodJWT) || rpc.BoundApp(ctx) != "" {
		return fmt.Errorf("%w: identity provider management requires a cluster operator", rpc.ErrUnauthorized)
	}
	if id.Method == rpc.AuthMethodCert && (id.Subject == "miren-runner" || strings.HasPrefix(id.Subject, "runner-")) {
		return fmt.Errorf("%w: identity provider management requires a cluster operator", rpc.ErrUnauthorized)
	}
	return nil
}

func (s *Server) checkName(ctx context.Context, name string, password, update bool) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("provider name is required")
	}
	oidc, err := s.ic.GetOIDCProvider(ctx, name)
	if err != nil {
		return err
	}
	pw, err := s.ic.GetPasswordProvider(ctx, name)
	if err != nil {
		return err
	}
	if (password && oidc != nil) || (!password && pw != nil) {
		return fmt.Errorf("provider %q already exists with a different type; names must be unique across types", name)
	}
	if !update && (oidc != nil || pw != nil) {
		return fmt.Errorf("provider %q already exists; update is required to overwrite credentials (pass --update to rotate credentials)", name)
	}
	return nil
}

func (s *Server) AddOIDC(ctx context.Context, state *ingress_v1alpha.IdentityProvidersAddOIDC) error {
	if err := allowOperator(ctx); err != nil {
		return err
	}
	a := state.Args()
	if a.ProviderUrl() == "" || a.ClientId() == "" || a.ClientSecret() == "" {
		return fmt.Errorf("provider URL, client ID and client secret are required")
	}
	s.ic.ProviderMu.Lock()
	defer s.ic.ProviderMu.Unlock()
	if err := s.checkName(ctx, a.Name(), false, a.Update()); err != nil {
		return err
	}
	scopes := a.Scopes()
	if !slices.Contains(scopes, "openid") {
		scopes = append([]string{"openid"}, scopes...)
	}
	_, err := s.ic.CreateOrUpdateOIDCProvider(ctx, &ingress_v1alpha.OidcProvider{
		Name: a.Name(), ConnectorType: "oidc", ProviderUrl: a.ProviderUrl(),
		ClientId: a.ClientId(), ClientSecret: a.ClientSecret(), Scopes: strings.Join(scopes, " "),
	})
	return err
}

func (s *Server) AddGitHub(ctx context.Context, state *ingress_v1alpha.IdentityProvidersAddGitHub) error {
	if err := allowOperator(ctx); err != nil {
		return err
	}
	a := state.Args()
	if a.ClientId() == "" || a.ClientSecret() == "" {
		return fmt.Errorf("client ID and client secret are required")
	}
	// Marshal only supported configuration, never caller-provided JSON that
	// could smuggle connector credentials into subsequent read responses.
	var cfg struct {
		Orgs []connectors.GitHubOrg `json:"orgs,omitempty"`
	}
	for _, org := range a.Orgs() {
		if org == nil {
			return fmt.Errorf("GitHub organization is required")
		}
		name := strings.TrimSpace(org.Name())
		if name == "" {
			return fmt.Errorf("GitHub organization name is required")
		}
		teams := org.Teams()
		for i, team := range teams {
			teams[i] = strings.TrimSpace(team)
			if teams[i] == "" {
				return fmt.Errorf("GitHub team name cannot be empty")
			}
		}
		cfg.Orgs = append(cfg.Orgs, connectors.GitHubOrg{Name: name, Teams: teams})
	}
	config, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	s.ic.ProviderMu.Lock()
	defer s.ic.ProviderMu.Unlock()
	if err := s.checkName(ctx, a.Name(), false, a.Update()); err != nil {
		return err
	}
	_, err = s.ic.CreateOrUpdateOIDCProvider(ctx, &ingress_v1alpha.OidcProvider{
		Name: a.Name(), ConnectorType: "github", ClientId: a.ClientId(),
		ClientSecret: a.ClientSecret(), ConfigJson: string(config),
	})
	return err
}

func (s *Server) AddPassword(ctx context.Context, state *ingress_v1alpha.IdentityProvidersAddPassword) error {
	if err := allowOperator(ctx); err != nil {
		return err
	}
	a := state.Args()
	if a.Password() == "" {
		return fmt.Errorf("password cannot be empty")
	}
	s.ic.ProviderMu.Lock()
	defer s.ic.ProviderMu.Unlock()
	if err := s.checkName(ctx, a.Name(), true, a.Update()); err != nil {
		return err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(a.Password()), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("failed to hash password: %w", err)
	}
	_, err = s.ic.CreateOrUpdatePasswordProvider(ctx, &ingress_v1alpha.PasswordProvider{
		Name: a.Name(), PasswordHash: string(hash),
	})
	return err
}

func oidcInfo(p *ingress_v1alpha.OidcProvider) *ingress_v1alpha.ProviderInfo {
	info := &ingress_v1alpha.ProviderInfo{}
	info.SetName(p.Name)
	kind := p.ConnectorType
	if kind == "" {
		kind = "oidc"
	}
	info.SetKind(kind)
	info.SetClientId(p.ClientId)
	switch kind {
	case "oidc":
		info.SetProviderUrl(p.ProviderUrl)
		info.SetScopes(strings.Fields(p.Scopes))
	case "github":
		var cfg struct {
			Orgs []connectors.GitHubOrg `json:"orgs"`
		}
		if json.Unmarshal([]byte(p.ConfigJson), &cfg) == nil {
			var orgs []*ingress_v1alpha.GitHubOrganization
			for _, org := range cfg.Orgs {
				o := &ingress_v1alpha.GitHubOrganization{}
				o.SetName(org.Name)
				o.SetTeams(org.Teams)
				orgs = append(orgs, o)
			}
			info.SetOrgs(orgs)
		}
	}
	return info
}

func passwordInfo(p *ingress_v1alpha.PasswordProvider) *ingress_v1alpha.ProviderInfo {
	info := &ingress_v1alpha.ProviderInfo{}
	info.SetName(p.Name)
	info.SetKind("password")
	return info
}

func (s *Server) List(ctx context.Context, state *ingress_v1alpha.IdentityProvidersList) error {
	if err := allowOperator(ctx); err != nil {
		return err
	}
	oidcs, err := s.ic.ListOIDCProviders(ctx)
	if err != nil {
		return err
	}
	pws, err := s.ic.ListPasswordProviders(ctx)
	if err != nil {
		return err
	}
	providers := make([]*ingress_v1alpha.ProviderInfo, 0, len(oidcs)+len(pws))
	for _, p := range oidcs {
		providers = append(providers, oidcInfo(p))
	}
	for _, p := range pws {
		providers = append(providers, passwordInfo(p))
	}
	slices.SortFunc(providers, func(a, b *ingress_v1alpha.ProviderInfo) int { return strings.Compare(a.Name(), b.Name()) })
	state.Results().SetProviders(providers)
	return nil
}

func (s *Server) Show(ctx context.Context, state *ingress_v1alpha.IdentityProvidersShow) error {
	if err := allowOperator(ctx); err != nil {
		return err
	}
	name := state.Args().Name()
	p, err := s.ic.GetOIDCProvider(ctx, name)
	if err != nil {
		return err
	}
	if p != nil {
		state.Results().SetProvider(oidcInfo(p))
		return nil
	}
	pw, err := s.ic.GetPasswordProvider(ctx, name)
	if err != nil {
		return err
	}
	if pw == nil {
		return fmt.Errorf("identity provider not found: %s", name)
	}
	state.Results().SetProvider(passwordInfo(pw))
	return nil
}

func (s *Server) Remove(ctx context.Context, state *ingress_v1alpha.IdentityProvidersRemove) error {
	if err := allowOperator(ctx); err != nil {
		return err
	}
	s.ic.ProviderMu.Lock()
	defer s.ic.ProviderMu.Unlock()
	a := state.Args()
	p, err := s.ic.GetOIDCProvider(ctx, a.Name())
	if err != nil {
		return err
	}
	var id entity.Id
	if p != nil {
		id = p.ID
	} else {
		pw, err := s.ic.GetPasswordProvider(ctx, a.Name())
		if err != nil {
			return err
		}
		if pw == nil {
			return fmt.Errorf("identity provider not found: %s", a.Name())
		}
		id = pw.ID
	}
	if !a.Force() {
		// Query references directly: route/app joins can omit orphaned routes,
		// which still reference this provider (including the default route).
		routes, err := s.ic.GetEntityStore().List(ctx, entity.Ref(ingress_v1alpha.HttpRouteAuthProviderId, id))
		if err != nil {
			return err
		}
		if routes.Length() > 0 {
			var hosts []string
			for routes.Next() {
				var route ingress_v1alpha.HttpRoute
				if err := routes.Read(&route); err != nil {
					return err
				}
				host := route.Host
				if route.Default {
					host = "(default)"
				}
				hosts = append(hosts, host)
			}
			return fmt.Errorf("provider %q is attached to %d route(s): %s. Detach with `miren route unprotect`, or pass --force to remove anyway", a.Name(), len(hosts), strings.Join(hosts, ", "))
		}
	}
	return s.ic.GetEntityStore().Delete(ctx, id)
}
