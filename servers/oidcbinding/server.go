package oidcbinding

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"strings"

	"miren.dev/runtime/api/core/core_v1alpha"
	aes "miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/api/oidcbinding/oidcbinding_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/idgen"
	"miren.dev/runtime/pkg/rpc"
)

const (
	GitHubActionsIssuer = "https://token.actions.githubusercontent.com"
)

type Server struct {
	Log *slog.Logger
	EC  *aes.Client
	EAC *entityserver_v1alpha.EntityAccessClient
}

var _ oidcbinding_v1alpha.OidcBindings = (*Server)(nil)

func NewServer(log *slog.Logger, ec *aes.Client, eac *entityserver_v1alpha.EntityAccessClient) *Server {
	return &Server{
		Log: log.With("module", "oidc-bindings"),
		EC:  ec,
		EAC: eac,
	}
}

func (s *Server) Add(ctx context.Context, state *oidcbinding_v1alpha.OidcBindingsAdd) error {
	args := state.Args()
	results := state.Results()

	if !args.HasApp() || args.App() == "" {
		results.SetError("app is required")
		return nil
	}
	if !args.HasIssuer() || args.Issuer() == "" {
		results.SetError("issuer is required")
		return nil
	}

	// Validate issuer URL — HTTPS required per OIDC spec (localhost exempt for dev)
	issuerURL, err := url.Parse(args.Issuer())
	if err != nil || issuerURL.Scheme == "" || issuerURL.Host == "" {
		results.SetError("issuer must be a valid HTTPS URL (e.g. https://token.actions.githubusercontent.com)")
		return nil
	}
	hostname := issuerURL.Hostname()
	isLoopback := hostname == "localhost" || (net.ParseIP(hostname) != nil && net.ParseIP(hostname).IsLoopback())
	if issuerURL.Scheme != "https" && !isLoopback {
		results.SetError("issuer must use HTTPS (except localhost for development)")
		return nil
	}

	if !rpc.AllowApp(ctx, args.App()) {
		return rpc.AppAccessError(ctx, args.App())
	}

	// Verify the app exists
	var appRec core_v1alpha.App
	if err := s.EC.Get(ctx, args.App(), &appRec); err != nil {
		results.SetError(fmt.Sprintf("app %q not found", args.App()))
		return nil
	}

	provider := args.Provider()
	subjectPattern := args.SubjectPattern()
	description := args.Description()

	// Build claim conditions
	var claimConditions []core_v1alpha.ClaimConditions
	if args.HasClaimConditions() {
		for _, cc := range args.ClaimConditions() {
			claimConditions = append(claimConditions, core_v1alpha.ClaimConditions{
				Key:     cc.Key(),
				Pattern: cc.Pattern(),
			})
		}
	}

	// Add default claim conditions for GitHub provider
	if provider == "github" && len(claimConditions) == 0 {
		claimConditions = append(claimConditions, core_v1alpha.ClaimConditions{
			Key:     "event_name",
			Pattern: "push,workflow_dispatch",
		})
	}

	if !identifiesCaller(subjectPattern, claimConditions) {
		results.SetError("binding must identify the caller: set a subject pattern or a claim condition (e.g. repository) that is not a bare wildcard")
		return nil
	}

	binding := &core_v1alpha.OidcBinding{
		App:             appRec.EntityId(),
		Provider:        provider,
		Issuer:          args.Issuer(),
		SubjectPattern:  subjectPattern,
		ClaimConditions: claimConditions,
		Description:     description,
	}

	name := "oidcb-" + idgen.Gen("ob")
	id, err := s.EC.Create(ctx, name, binding)
	if err != nil {
		s.Log.Error("failed to create OIDC binding", "error", err)
		results.SetError("failed to create OIDC binding")
		return nil
	}

	s.Log.Info("created OIDC binding",
		"id", string(id),
		"app", args.App(),
		"provider", provider,
		"issuer", args.Issuer(),
	)

	info := toBindingInfo(string(id), args.App(), binding)
	results.SetBinding(info)

	return nil
}

func (s *Server) List(ctx context.Context, state *oidcbinding_v1alpha.OidcBindingsList) error {
	args := state.Args()
	results := state.Results()

	if !args.HasApp() || args.App() == "" {
		results.SetBindings(nil)
		return nil
	}

	if !rpc.AllowApp(ctx, args.App()) {
		return rpc.AppAccessError(ctx, args.App())
	}

	// Look up the app to get its entity ID
	var appRec core_v1alpha.App
	if err := s.EC.Get(ctx, args.App(), &appRec); err != nil {
		results.SetBindings(nil)
		return nil
	}

	// List bindings by app ref
	listResp, err := s.EAC.List(ctx, entity.Ref(core_v1alpha.OidcBindingAppId, appRec.EntityId()))
	if err != nil {
		s.Log.Error("failed to list OIDC bindings", "error", err)
		return fmt.Errorf("failed to list OIDC bindings: %w", err)
	}

	var bindings []*oidcbinding_v1alpha.BindingInfo
	for _, e := range listResp.Values() {
		var b core_v1alpha.OidcBinding
		b.Decode(e.Entity())
		info := toBindingInfo(e.Id(), args.App(), &b)
		bindings = append(bindings, info)
	}

	results.SetBindings(bindings)
	return nil
}

func (s *Server) Remove(ctx context.Context, state *oidcbinding_v1alpha.OidcBindingsRemove) error {
	args := state.Args()
	results := state.Results()

	if !args.HasId() || args.Id() == "" {
		results.SetError("id is required")
		return nil
	}

	id := args.Id()

	// Verify it exists and is an oidc_binding
	resp, err := s.EAC.Get(ctx, id)
	if err != nil {
		results.SetError(fmt.Sprintf("binding %q not found", id))
		return nil
	}

	var binding core_v1alpha.OidcBinding
	binding.Decode(resp.Entity().Entity())
	if !binding.Is(resp.Entity().Entity()) {
		results.SetError(fmt.Sprintf("%q is not an OIDC binding", id))
		return nil
	}

	appName := ResolveAppName(string(binding.App))
	if !rpc.AllowApp(ctx, appName) {
		return rpc.AppAccessError(ctx, appName)
	}

	if err := s.EC.Delete(ctx, entity.Id(id)); err != nil {
		s.Log.Error("failed to delete OIDC binding", "error", err)
		results.SetError("failed to remove OIDC binding")
		return nil
	}

	s.Log.Info("removed OIDC binding", "id", id)
	results.SetSuccess(true)
	return nil
}

// identifiesCaller reports whether a binding narrows the tokens it accepts down
// to a particular caller. Without this, a binding constrained only by event_name
// (or by nothing at all) would accept a token from any repository the issuer
// serves, which for GitHub Actions means every repository on github.com.
func identifiesCaller(subjectPattern string, conditions []core_v1alpha.ClaimConditions) bool {
	if !isWildcardPattern(subjectPattern) {
		return true
	}
	for _, cc := range conditions {
		// event_name says what triggered the workflow, never who ran it.
		if cc.Key == "event_name" {
			continue
		}
		if !isWildcardPattern(cc.Pattern) {
			return true
		}
	}
	return false
}

// isWildcardPattern reports whether a pattern constrains nothing: either empty
// or made up entirely of '*', both of which match any value.
func isWildcardPattern(pattern string) bool {
	return strings.Trim(pattern, "*") == ""
}

func toBindingInfo(id, appName string, b *core_v1alpha.OidcBinding) *oidcbinding_v1alpha.BindingInfo {
	info := &oidcbinding_v1alpha.BindingInfo{}
	info.SetId(id)
	info.SetApp(appName)
	info.SetProvider(b.Provider)
	info.SetIssuer(b.Issuer)
	info.SetSubjectPattern(b.SubjectPattern)
	info.SetDescription(b.Description)

	if len(b.ClaimConditions) > 0 {
		var conditions []*oidcbinding_v1alpha.ClaimCondition
		for _, cc := range b.ClaimConditions {
			c := &oidcbinding_v1alpha.ClaimCondition{}
			c.SetKey(cc.Key)
			c.SetPattern(cc.Pattern)
			conditions = append(conditions, c)
		}
		info.SetClaimConditions(conditions)
	}

	return info
}

// ResolveAppName extracts the app name from an entity reference like "app/my-app".
func ResolveAppName(appRef string) string {
	if idx := strings.LastIndex(appRef, "/"); idx >= 0 {
		return appRef[idx+1:]
	}
	return appRef
}
