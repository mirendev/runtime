package cloudauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sync"

	"miren.dev/runtime/pkg/rbac"
	"miren.dev/runtime/pkg/uplink"
)

const (
	AuthorizationCapability   = "authorization"
	TypeAuthorizationSnapshot = "authorization.snapshot"
)

// AuthorizationSnapshot is the cloud authorization contract. Cloud
// sends full replacements, with increasing revisions within each session.
// Revisions restart with each session, so they only order snapshots within one.
type AuthorizationSnapshot struct {
	// Version 2 selects logical resource permissions. Zero preserves saved and
	// wire version-1 snapshots, whose permissions name RPC interfaces/methods.
	Version        uint                `json:"version,omitempty"`
	SessionID      string              `json:"session_id"`
	OrganizationID string              `json:"organization_id"`
	Revision       uint64              `json:"revision"`
	Policy         rbac.Policy         `json:"policy"`
	Memberships    map[string][]string `json:"memberships"`
}

func (s *AuthorizationSnapshot) GetPolicy() *rbac.Policy { return &s.Policy }

// AuthorizationState serializes snapshot replacement with evaluation, including
// cache reads/writes. Clearing a cache alone would allow an in-flight old-policy
// evaluation to repopulate it after a revocation.
//
// Cloud defines access, so the last snapshot it sent stays authoritative until
// it sends another. Losing the connection, reconnecting, or restarting is not
// new information about permissions, and neither is an update we cannot read,
// so none of them withdraw access. The cost is that a demotion made while the
// cluster cannot hear cloud waits for the next snapshot to take effect.
type AuthorizationState struct {
	mu         sync.Mutex
	logger     *slog.Logger
	path       string
	clusterID  string
	snapshot   AuthorizationSnapshot
	evaluator  *rbac.Evaluator
	session    uplink.Session
	sessionCtx context.Context
	// revision is the highest revision seen in the current session, including
	// rejected snapshots, so a replay of an older one cannot be applied.
	revision uint64
	loaded   bool
}

// savedAuthorization is the on-disk form of a snapshot. ClusterID ties it to
// the registration that received it.
type savedAuthorization struct {
	ClusterID string                `json:"cluster_id"`
	Snapshot  AuthorizationSnapshot `json:"snapshot"`
}

// NewAuthorizationState restores the snapshot saved at path for clusterID, if
// any, and saves each applied snapshot there. An empty path keeps the snapshot
// in memory only.
func NewAuthorizationState(ctx context.Context, logger *slog.Logger, path, clusterID string) *AuthorizationState {
	s := &AuthorizationState{logger: logger.With("module", "authorization"), path: path, clusterID: clusterID}
	s.evaluator = rbac.NewEvaluator(ctx, &s.snapshot, logger)
	s.restore()
	return s
}

func (s *AuthorizationState) restore() {
	if s.path == "" {
		return
	}
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	var saved savedAuthorization
	if err == nil {
		err = json.Unmarshal(data, &saved)
	}
	snapshot := saved.Snapshot
	if err == nil && saved.ClusterID != s.clusterID {
		err = fmt.Errorf("snapshot belongs to cluster %q, not %q", saved.ClusterID, s.clusterID)
	}
	if err == nil && (snapshot.Memberships == nil || snapshot.Policy.Rules == nil) {
		err = fmt.Errorf("snapshot is missing rules or memberships")
	}
	if err == nil && snapshot.Version > 2 {
		err = fmt.Errorf("unsupported authorization snapshot version %d", snapshot.Version)
	}
	if err != nil {
		s.logger.Warn("ignoring saved cloud authorization snapshot", "path", s.path, "error", err)
		return
	}
	s.snapshot = snapshot
	s.loaded = true
	s.logger.Info("restored saved cloud authorization snapshot", "revision", snapshot.Revision,
		"rules", len(snapshot.Policy.Rules), "principals", len(snapshot.Memberships))
}

func (s *AuthorizationState) save(snapshot AuthorizationSnapshot) error {
	data, err := json.Marshal(savedAuthorization{ClusterID: s.clusterID, Snapshot: snapshot})
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), filepath.Base(s.path)+".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), s.path)
}

type AuthorizationLink interface {
	OfferCapability(uplink.CapabilityOffer)
	OnSession(func(context.Context, uplink.Session))
	Handle(string, uplink.MessageHandler)
}

func (s *AuthorizationState) Register(link AuthorizationLink) {
	// Hold new v2 negotiation until Cloud scope emission and the full runtime
	// enforcement boundary are ready for an explicitly approved rollout.
	link.OfferCapability(uplink.CapabilityOffer{Name: AuthorizationCapability, Versions: []uint{1}})
	link.OnSession(s.beginSession)
	link.Handle(TypeAuthorizationSnapshot, s.receiveSnapshot)
}

func (s *AuthorizationState) beginSession(ctx context.Context, session uplink.Session) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.session = session
	s.sessionCtx = ctx
	s.revision = 0
	context.AfterFunc(ctx, func() { s.endSession(ctx) })
	if !s.loaded {
		return
	}
	// A different organization means the cluster was registered again, and the
	// old snapshot describes someone else's users.
	if s.snapshot.OrganizationID != session.OrganizationID {
		s.logger.Warn("discarding cloud authorization snapshot from another organization",
			"snapshot_organization", s.snapshot.OrganizationID, "session_organization", session.OrganizationID)
		s.loaded = false
		s.snapshot = AuthorizationSnapshot{}
		s.evaluator.ClearCache()
		if s.path != "" {
			if err := os.Remove(s.path); err != nil && !errors.Is(err, os.ErrNotExist) {
				s.logger.Warn("failed to remove saved cloud authorization snapshot", "path", s.path, "error", err)
			}
		}
		return
	}
	s.logger.Info("cloud connected; authorizing from the previous snapshot until cloud sends a current one",
		"revision", s.snapshot.Revision)
}

func (s *AuthorizationState) endSession(ctx context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sessionCtx != ctx || !s.loaded {
		return
	}
	s.logger.Warn("cloud connection lost; authorizing from the last snapshot until cloud sends a new one",
		"revision", s.snapshot.Revision)
}

func (s *AuthorizationState) receiveSnapshot(ctx context.Context, data json.RawMessage) error {
	var snapshot AuthorizationSnapshot
	decodeErr := json.Unmarshal(data, &snapshot)
	s.mu.Lock()
	defer s.mu.Unlock()
	selection, selected := s.session.Capability(AuthorizationCapability)
	if !selected || (selection.Version != 1 && selection.Version != 2) || s.sessionCtx == nil || s.sessionCtx.Err() != nil || ctx.Err() != nil {
		return fmt.Errorf("authorization snapshot outside a negotiated session")
	}
	// An update we cannot read tells us nothing about permissions, so access
	// stays as the last good snapshot defined it. Cloud sending one is a bug.
	if decodeErr != nil {
		s.logger.Error("ignoring unreadable cloud authorization snapshot", "error", decodeErr)
		return fmt.Errorf("invalid authorization snapshot: %w", decodeErr)
	}
	if snapshot.SessionID != s.session.ID || snapshot.OrganizationID != s.session.OrganizationID {
		return fmt.Errorf("authorization snapshot scope does not match session")
	}
	if snapshot.Revision == 0 || snapshot.Revision <= s.revision {
		return fmt.Errorf("authorization snapshot revision is not increasing")
	}
	s.revision = snapshot.Revision
	if snapshot.Version > 2 || (selection.Version == 1 && snapshot.Version > 1) || (selection.Version == 2 && snapshot.Version != 2) {
		return fmt.Errorf("authorization snapshot version does not match negotiated capability")
	}
	if snapshot.Memberships == nil || snapshot.Policy.Rules == nil {
		s.logger.Error("ignoring incomplete cloud authorization snapshot", "revision", snapshot.Revision)
		return fmt.Errorf("authorization snapshot must contain rules and memberships")
	}
	s.snapshot = snapshot
	s.loaded = true
	s.evaluator.ClearCache()
	s.logger.Info("cloud authorization snapshot applied", "revision", snapshot.Revision,
		"rules", len(snapshot.Policy.Rules), "principals", len(snapshot.Memberships))
	if s.path != "" {
		if err := s.save(snapshot); err != nil {
			s.logger.Warn("failed to save cloud authorization snapshot; a restart while cloud is unreachable will deny cloud users",
				"path", s.path, "error", err)
		}
	}
	return nil
}

func (s *AuthorizationState) Evaluate(req *rbac.Request) (rbac.Decision, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.evaluateLocked(req)
}

// EvaluateMethod defers migrated methods only under logical policy. Legacy
// method authorization and the version decision share the snapshot lock.
func (s *AuthorizationState) EvaluateMethod(req *rbac.Request, migrated bool) (rbac.Decision, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.loaded && s.snapshot.Version == 2 && migrated {
		return rbac.DecisionAllow, ""
	}
	return s.evaluateLocked(req)
}

// AllowsLegacy authorizes before permitting a handler to skip logical checks.
// A version-only read could skip both gates during a v2-to-v1 transition.
func (s *AuthorizationState) AllowsLegacy(req *rbac.Request) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.snapshot.Version >= 2 {
		return false
	}
	decision, _ := s.evaluateLocked(req)
	return decision == rbac.DecisionAllow
}

// EvaluateResource selects legacy or logical vocabulary under the same lock as
// snapshot replacement. A revocation cannot race a separate version lookup.
func (s *AuthorizationState) EvaluateResource(req *rbac.Request, legacyResource, legacyAction string) (rbac.Decision, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.snapshot.Version < 2 {
		req.Resource, req.Action = legacyResource, legacyAction
	}
	return s.evaluateLocked(req)
}

func (s *AuthorizationState) evaluateLocked(req *rbac.Request) (rbac.Decision, string) {
	if !s.loaded {
		return rbac.DecisionDeny, "not_synced"
	}
	// The authenticated session scopes this authoritative principal map. Token
	// organization claims may be absent or numeric and are not organization XIDs.
	groups, present := s.snapshot.Memberships[req.Subject]
	if !present {
		return rbac.DecisionDeny, "unknown_principal"
	}
	// The evaluator sorts request groups for its cache key; never lend it the
	// authoritative slice or the caller's token groups.
	req.Groups = slices.Clone(groups)
	decision := s.evaluator.Evaluate(req)
	if decision == rbac.DecisionDeny {
		return decision, "no_matching_rule"
	}
	return decision, ""
}
