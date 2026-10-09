package sandbox

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"time"

	compute "miren.dev/runtime/api/compute/compute_v1alpha"
	shared "miren.dev/runtime/api/session"
	sessionapi "miren.dev/runtime/api/session/session_v1alpha"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/entity"
)

const sessionsWaitTimeout = 20 * time.Second

type sessionsResponse struct {
	Sessions       []string                  `json:"sessions"`
	SessionDetails map[string]sessionDetails `json:"session_details"`
	Deleted        []string                  `json:"deleted"`
	Version        string                    `json:"version"`
}

type sessionDetails struct {
	App     entity.Id              `json:"app"`
	Version entity.Id              `json:"version"`
	Service string                 `json:"service"`
	Group   string                 `json:"group,omitempty"`
	Spec    sessionapi.SandboxSpec `json:"spec"`
}

// The sandbox sees only bindings addressed to its authenticated identity.
// Deletion notifications remain visible until explicitly acknowledged.
func (c *SandboxController) handleSessionsRequest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMetadataError(w, http.StatusMethodNotAllowed, "only GET is allowed")
		return
	}
	sandboxID, _, ok := c.authenticateWorkload(w, r)
	if !ok {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	waitFor := r.URL.Query().Get("wait")
	deadline := time.NewTimer(sessionsWaitTimeout)
	defer deadline.Stop()
	for {
		result, err := c.sessionsSnapshot(r.Context(), sandboxID)
		if err != nil {
			writeMetadataError(w, http.StatusInternalServerError, "failed to read sessions")
			return
		}
		if waitFor == "" || result.Version != waitFor {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(result)
			return
		}
		select {
		case <-r.Context().Done():
			return
		case <-deadline.C:
			w.WriteHeader(http.StatusNotModified)
			return
		case <-time.After(time.Second):
		}
	}
}

func (c *SandboxController) sessionsSnapshot(ctx context.Context, sandboxID string) (sessionsResponse, error) {
	result := sessionsResponse{Sessions: []string{}, SessionDetails: map[string]sessionDetails{}, Deleted: []string{}}
	sandboxResp, err := c.EAC.Get(ctx, sandboxID)
	if err == nil {
		var sb compute.Sandbox
		sb.Decode(sandboxResp.Entity().Entity())
		if sb.SessionInfo.Owner != "" {
			owner, err := c.EAC.Get(ctx, sb.SessionInfo.Owner.String())
			if err != nil && !errors.Is(err, cond.ErrNotFound{}) {
				return result, err
			}
			if err == nil {
				var s sessionapi.Session
				s.Decode(owner.Entity().Entity())
				if s.Sandbox == entity.Id(sandboxID) && s.DesiredState != sessionapi.SUSPENDED {
					result.Sessions = append(result.Sessions, s.ID.String())
					result.SessionDetails[s.ID.String()] = sessionDetails{
						App: s.App, Version: s.Version, Service: s.Service, Group: s.Group, Spec: s.Spec,
					}
				}
			}
		}
	} else if !errors.Is(err, cond.ErrNotFound{}) {
		return result, err
	}
	resp, err := c.EAC.List(ctx, entity.String(sessionapi.BindingSandboxId, sandboxID))
	if err != nil {
		return result, err
	}
	for _, e := range resp.Values() {
		var binding sessionapi.Binding
		binding.Decode(e.Entity())
		if !binding.DeletedAt.IsZero() {
			if binding.AcknowledgedAt.IsZero() {
				result.Deleted = append(result.Deleted, binding.Session)
			}
			continue
		}
		sessionResp, err := c.EAC.Get(ctx, binding.Session)
		if errors.Is(err, cond.ErrNotFound{}) {
			// The deletion event can lag or be lost across coordinator restarts.
			// Persist the notice here so the next poll still sees it.
			_, err = c.EAC.Patch(ctx, entity.New(entity.DBId, binding.ID,
				(&sessionapi.Binding{DeletedAt: time.Now()}).Encode).Attrs(), e.Revision())
			if err != nil && !errors.Is(err, cond.ErrConflict{}) {
				return result, err
			}
			result.Deleted = append(result.Deleted, binding.Session)
			continue
		}
		if err != nil {
			return result, err
		}
		var s sessionapi.Session
		s.Decode(sessionResp.Entity().Entity())
		if s.MaxSessionsPerSandbox > 1 && s.Sandbox == entity.Id(sandboxID) && s.DesiredState != sessionapi.SUSPENDED {
			result.Sessions = append(result.Sessions, binding.Session)
			result.SessionDetails[binding.Session] = sessionDetails{
				App: s.App, Version: s.Version, Service: s.Service, Group: s.Group,
				Spec: s.Spec,
			}
		}
	}
	slices.Sort(result.Sessions)
	slices.Sort(result.Deleted)
	encoded, err := json.Marshal(result)
	if err != nil {
		return result, fmt.Errorf("encoding session snapshot: %w", err)
	}
	digest := sha256.Sum256(encoded)
	result.Version = hex.EncodeToString(digest[:])
	return result, nil
}

func (c *SandboxController) handleSessionAcknowledgment(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMetadataError(w, http.StatusMethodNotAllowed, "only POST is allowed")
		return
	}
	sandboxID, _, ok := c.authenticateWorkload(w, r)
	if !ok {
		return
	}
	var request struct {
		Session string `json:"session"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&request); err != nil || request.Session == "" {
		writeMetadataError(w, http.StatusBadRequest, "session is required")
		return
	}
	resp, err := c.EAC.Get(r.Context(), shared.BindingID(entity.Id(request.Session)).String())
	if errors.Is(err, cond.ErrNotFound{}) {
		writeMetadataError(w, http.StatusNotFound, "session binding not found")
		return
	}
	if err != nil {
		writeMetadataError(w, http.StatusInternalServerError, "failed to read session binding")
		return
	}
	var binding sessionapi.Binding
	binding.Decode(resp.Entity().Entity())
	if binding.Session != request.Session || binding.Sandbox != sandboxID {
		writeMetadataError(w, http.StatusForbidden, "session belongs to another sandbox")
		return
	}
	if binding.DeletedAt.IsZero() {
		writeMetadataError(w, http.StatusConflict, "session has not been deleted")
		return
	}
	if binding.AcknowledgedAt.IsZero() {
		_, err = c.EAC.Patch(r.Context(), entity.New(entity.DBId, binding.ID,
			(&sessionapi.Binding{AcknowledgedAt: time.Now()}).Encode).Attrs(), resp.Entity().Revision())
		if err != nil {
			writeMetadataError(w, http.StatusInternalServerError, "failed to acknowledge deletion")
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}
