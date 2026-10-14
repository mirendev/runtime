package sandbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	computeapi "miren.dev/runtime/api/compute"
	compute "miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/entity"
)

const activityRenewAt = 30 * time.Second

var errTerminalSandbox = errors.New("sandbox is terminal")
var errStartingSandbox = errors.New("sandbox is not running yet")

type activityOrder struct {
	mu     sync.Mutex
	issued uint64
	latest uint64
}

func (c *SandboxController) handleActivityRequest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodGet {
		writeMetadataError(w, http.StatusMethodNotAllowed, "only GET and POST are allowed")
		return
	}
	if r.Method == http.MethodGet {
		sandboxID, _, ok := c.authenticateWorkload(w, r)
		if !ok {
			return
		}
		resp, err := c.EAC.Get(r.Context(), sandboxID)
		if err != nil {
			writeMetadataError(w, http.StatusInternalServerError, "failed to read activity")
			return
		}
		var sb compute.Sandbox
		sb.Decode(resp.Entity().Entity())
		if sb.Status != compute.RUNNING {
			writeMetadataError(w, http.StatusConflict, errTerminalSandbox.Error())
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		var shutdownAt *time.Time
		if !sb.ShutdownAt.IsZero() {
			shutdownAt = &sb.ShutdownAt
		}
		_ = json.NewEncoder(w).Encode(struct {
			ShutdownAt *time.Time `json:"shutdown_at"`
		}{ShutdownAt: shutdownAt})
		return
	}
	var report struct {
		State string `json:"state"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&report); err != nil || (report.State != "active" && report.State != "idle") {
		writeMetadataError(w, http.StatusBadRequest, "state must be active or idle")
		return
	}
	sandboxID, _, ok := c.authenticateWorkload(w, r)
	if !ok {
		return
	}
	state := compute.ACTIVE
	if report.State == "idle" {
		state = compute.IDLE
	}
	c.activityMu.Lock()
	if c.activity == nil {
		c.activity = make(map[string]*activityOrder)
	}
	order := c.activity[sandboxID]
	if order == nil {
		order = &activityOrder{}
		c.activity[sandboxID] = order
	}
	order.issued++
	seq := order.issued
	c.activityMu.Unlock()
	order.mu.Lock()
	var err error
	if seq > order.latest {
		order.latest = seq
		err = c.recordSandboxActivity(r.Context(), sandboxID, state, time.Now())
	}
	order.mu.Unlock()
	if err != nil {
		if errors.Is(err, errTerminalSandbox) {
			writeMetadataError(w, http.StatusConflict, err.Error())
			return
		}
		if errors.Is(err, errStartingSandbox) {
			writeMetadataError(w, http.StatusServiceUnavailable, err.Error())
			return
		}
		c.Log.Warn("failed to record sandbox activity", "sandbox", sandboxID, "error", err)
		writeMetadataError(w, http.StatusInternalServerError, "failed to record activity")
		return
	}
	// Preserve the 204 contract while exposing the notice to heartbeat clients.
	resp, err := c.EAC.Get(r.Context(), sandboxID)
	if err != nil {
		writeMetadataError(w, http.StatusInternalServerError, "failed to read activity")
		return
	}
	var sb compute.Sandbox
	sb.Decode(resp.Entity().Entity())
	if !sb.ShutdownAt.IsZero() {
		w.Header().Set("Miren-Shutdown-At", sb.ShutdownAt.Format(time.RFC3339Nano))
	}
	w.WriteHeader(http.StatusNoContent)
}

// An unchanged heartbeat is persisted at most once every 30 seconds. Changes
// are always written immediately; CAS ensures a stale read cannot undo one.
func (c *SandboxController) recordSandboxActivity(ctx context.Context, sandboxID string, state compute.ActivityState, now time.Time) error {
	for range 10 {
		resp, err := c.EAC.Get(ctx, sandboxID)
		if err != nil {
			return err
		}
		var sb compute.Sandbox
		sb.Decode(resp.Entity().Entity())
		if sb.Status != compute.RUNNING {
			if computeapi.SandboxDead(sb.Status) {
				return errTerminalSandbox
			}
			return errStartingSandbox
		}
		if sb.Activity.ReportedAt.After(now) {
			return nil // A later request already won the CAS race.
		}
		if sb.Activity.State == state && sb.ShutdownAt.IsZero() && !sb.Activity.ReportedAt.IsZero() && now.Sub(sb.Activity.ReportedAt) < activityRenewAt {
			return nil
		}
		result, err := c.EAC.Patch(ctx, entity.New(
			entity.Ref(entity.DBId, entity.Id(sandboxID)),
			entity.Component(compute.SandboxActivityId, (&compute.Activity{State: state, ReportedAt: now}).Encode()),
		).Attrs(), resp.Entity().Revision())
		if err == nil {
			if c.writeTracker != nil && result.HasRevision() {
				c.writeTracker.RecordWrite(result.Revision())
			}
			return nil
		}
		if !errors.Is(err, cond.ErrConflict{}) {
			return err
		}
	}
	return fmt.Errorf("activity update conflicted repeatedly")
}
