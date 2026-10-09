package sandbox

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	sessionapi "miren.dev/runtime/api/session/session_v1alpha"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/entity"
)

func (c *SandboxController) handleSessionActivity(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMetadataError(w, http.StatusMethodNotAllowed, "only POST is allowed")
		return
	}
	sandboxID, _, ok := c.authenticateWorkload(w, r)
	if !ok {
		return
	}
	var report struct {
		Session string `json:"session"`
		State   string `json:"state"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&report); err != nil ||
		report.Session == "" || (report.State != "active" && report.State != "idle") {
		writeMetadataError(w, http.StatusBadRequest, "session and active or idle state are required")
		return
	}
	now := time.Now()
	for range 10 {
		resp, err := c.EAC.Get(r.Context(), report.Session)
		if errors.Is(err, cond.ErrNotFound{}) {
			writeMetadataError(w, http.StatusConflict, "Session is not assigned")
			return
		}
		if err != nil {
			writeMetadataError(w, http.StatusInternalServerError, "failed to read Session")
			return
		}
		var s sessionapi.Session
		s.Decode(resp.Entity().Entity())
		if s.Sandbox.String() != sandboxID {
			writeMetadataError(w, http.StatusForbidden, "Session belongs to another sandbox")
			return
		}
		if s.DesiredState != sessionapi.RUNNING {
			writeMetadataError(w, http.StatusConflict, "Session is not accepting work")
			return
		}
		if s.Phase != sessionapi.READY {
			writeMetadataError(w, http.StatusServiceUnavailable, "Session is not ready")
			return
		}
		state := sessionapi.ACTIVE
		if report.State == "idle" {
			state = sessionapi.IDLE
		}
		if s.ActivityAt.After(now) || (s.Activity == state && !s.ActivityAt.IsZero() && now.Sub(s.ActivityAt) < activityRenewAt) {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		e := entity.New(resp.Entity().Attrs())
		e.Remove(sessionapi.SessionIdleSinceId)
		idleSince := time.Time{}
		if state == sessionapi.IDLE {
			idleSince = s.IdleSince
			if idleSince.IsZero() || s.SelfReportedActivity(now) != sessionapi.IDLE {
				idleSince = now
			}
		}
		for _, attr := range (&sessionapi.Session{Activity: state, ActivityAt: now, IdleSince: idleSince}).Encode() {
			e.Set(attr)
		}
		_, err = c.EAC.Replace(r.Context(), e.Attrs(), resp.Entity().Revision())
		if errors.Is(err, cond.ErrConflict{}) {
			continue
		}
		if err != nil {
			writeMetadataError(w, http.StatusInternalServerError, "failed to record Session activity")
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeMetadataError(w, http.StatusServiceUnavailable, "Session activity changed concurrently; retry")
}
