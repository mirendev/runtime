package session

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	computeapi "miren.dev/runtime/api/compute"
	compute "miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	shared "miren.dev/runtime/api/session"
	sessionapi "miren.dev/runtime/api/session/session_v1alpha"
	storage "miren.dev/runtime/api/storage/storage_v1alpha"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/types"
)

// Controller is the sole writer of Session observed state.
type Controller struct {
	Log             *slog.Logger
	EAC             *entityserver_v1alpha.EntityAccessClient
	sharedAdmission sync.Mutex
}

const shutdownGrace = time.Minute

func NewController(log *slog.Logger, eac *entityserver_v1alpha.EntityAccessClient) *Controller {
	return &Controller{Log: log.With("module", "session"), EAC: eac}
}

func (c *Controller) Init(context.Context) error { return nil }

func (c *Controller) Reconcile(ctx context.Context, s *sessionapi.Session, _ *entity.Meta) error {
	if s.MaxSessionsPerSandbox > 1 {
		// A second reconcile for the same Session must not reserve another
		// slot before the first has published its binding.
		c.sharedAdmission.Lock()
		defer c.sharedAdmission.Unlock()
		if s.Generation != 0 {
			return c.fail(ctx, s.ID, fmt.Errorf("session %s has dedicated incarnations and cannot enter shared mode", s.ID))
		}
		children, err := c.children(ctx, s.ID)
		if err != nil {
			return err
		}
		if len(children) != 0 {
			return c.fail(ctx, s.ID, fmt.Errorf("session %s has dedicated sandboxes and cannot enter shared mode", s.ID))
		}
		if s.DesiredState == sessionapi.SUSPENDED {
			return c.suspendShared(ctx, s)
		}
		return c.runShared(ctx, s)
	}
	// A mode switch must never turn an externally managed host into a child
	// that this Session could suspend or delete.
	if _, err := c.EAC.Get(ctx, shared.BindingID(s.ID).String()); err == nil {
		return fmt.Errorf("session %s is bound to a shared sandbox and cannot enter dedicated mode", s.ID)
	} else if !errors.Is(err, cond.ErrNotFound{}) {
		return err
	}
	if s.DesiredState == sessionapi.SUSPENDED {
		return c.suspend(ctx, s)
	}
	if s.Phase == sessionapi.FAILED && s.Failure == serviceMissingFailure(s.App, s.Service) {
		return nil // Wait for a deploy that restores the service instead of clearing this failure.
	}
	return c.run(ctx, s)
}

func (c *Controller) run(ctx context.Context, s *sessionapi.Session) error {
	children, err := c.children(ctx, s.ID)
	if err != nil {
		return err
	}
	// The child is the durable creation record even if the coordinator died
	// before publishing it on the Session. Never start another while any
	// unacknowledged earlier child could still execute.
	candidate := sandboxID(s.ID, s.Generation+1)
	for _, child := range children {
		if child.ID == s.Sandbox || child.ID == candidate {
			continue
		}
		ack, err := c.teardownDone(ctx, child.ID)
		if err != nil {
			return err
		}
		if !ack {
			if !terminal(child.Status) {
				if err := c.stopSandbox(ctx, child.ID); err != nil {
					return err
				}
			}
			return nil
		}
	}
	if s.Sandbox != "" {
		current, err := c.getSandbox(ctx, s.Sandbox)
		if err == nil && !terminal(current.Status) {
			for _, child := range children {
				if child.ID == candidate && !terminal(child.Status) {
					return c.stopSandbox(ctx, child.ID)
				}
			}
		} else if err != nil && !errors.Is(err, cond.ErrNotFound{}) {
			return err
		}
	}
	if s.Sandbox != "" {
		sb, err := c.getSandbox(ctx, s.Sandbox)
		if err == nil && !terminal(sb.Status) {
			if s.Version != "" && sb.Spec.Version != s.Version {
				return c.drainOrStop(ctx, sb.ID)
			}
			if !sb.ShutdownAt.IsZero() {
				return c.drainOrStop(ctx, sb.ID)
			}
			phase := sessionapi.ACTIVATING
			if sb.Status == compute.RUNNING {
				phase = sessionapi.READY
			}
			if s.Phase == phase {
				return nil
			}
			update := &sessionapi.Session{Phase: phase}
			if s.Phase != phase {
				update.LastTransition = time.Now()
			}
			return c.patch(ctx, s.ID, update)
		}
		if err != nil && !errors.Is(err, cond.ErrNotFound{}) {
			return fmt.Errorf("reading current sandbox: %w", err)
		}
		ack, err := c.teardownDone(ctx, s.Sandbox)
		if err != nil || !ack {
			return err // Wait for runner teardown, including if the child was deleted.
		}
	}

	generation := s.Generation + 1
	id := sandboxID(s.ID, generation)
	ack, err := c.teardownDone(ctx, id)
	if err != nil {
		return err
	}
	if ack {
		// An unpublished incarnation finished (and may already have been
		// garbage-collected). Consume its ID permanently, then retry with N+1.
		return c.patch(ctx, s.ID, &sessionapi.Session{
			Generation:  generation,
			Incarnation: []sessionapi.Incarnation{{Generation: generation, Sandbox: id.String(), EndedAt: time.Now(), Reason: "unpublished"}},
		})
	}
	spec, err := computeSpec(s.Spec)
	if err != nil {
		return c.fail(ctx, s.ID, err)
	}
	if s.Disk != "" {
		resp, err := c.EAC.Get(ctx, s.Disk.String())
		if err != nil {
			return fmt.Errorf("reading session disk: %w", err)
		}
		var disk storage.Disk
		disk.Decode(resp.Entity().Entity())
		if disk.CreatedBy != s.App || disk.Name == "" {
			return c.fail(ctx, s.ID, fmt.Errorf("session disk %s does not belong to app %s", s.Disk, s.App))
		}
		attached := false
		for i := range spec.Volume {
			if spec.Volume[i].Provider == "miren" {
				if attached {
					return c.fail(ctx, s.ID, fmt.Errorf("session disk %s requires exactly one miren volume", s.Disk))
				}
				spec.Volume[i].DiskName = disk.Name
				attached = true
			}
		}
		if !attached {
			return c.fail(ctx, s.ID, fmt.Errorf("session disk %s requires a miren volume in the spec", s.Disk))
		}
	}
	// Keep app-scoped log authorization; the indexed session attribute selects
	// every incarnation while miren.sandbox retains the concrete source.
	spec.LogEntity = s.App.String()
	spec.LogAttribute = append(spec.LogAttribute, types.LabelSet(
		"miren.session", s.ID.String(),
		"miren.sandbox", id.String(),
		"miren.session_generation", fmt.Sprint(generation),
	)...)

	if existing, err := c.getSandbox(ctx, id); errors.Is(err, cond.ErrNotFound{}) {
		name := strings.TrimPrefix(id.String(), "sandbox/")
		sb := &compute.Sandbox{
			Status:      compute.PENDING,
			Spec:        spec,
			SessionInfo: compute.SessionInfo{Owner: s.ID},
		}
		_, err = c.EAC.Create(ctx, entity.New(
			(&core_v1alpha.Metadata{Name: name, Labels: types.LabelSet("session", s.ID.String(), "service", s.Service)}).Encode,
			entity.DBId, id,
			sb.Encode,
		).Attrs())
		if err != nil {
			return c.fail(ctx, s.ID, fmt.Errorf("creating session sandbox: %w", err))
		}
	} else if err != nil {
		return err
	} else if existing.SessionInfo.Owner != s.ID {
		return c.fail(ctx, s.ID, fmt.Errorf("incarnation %s already exists but cannot be reused", id))
	} else if terminal(existing.Status) {
		return nil // The runner acknowledgment will wake this Session.
	} else if !existing.ShutdownAt.IsZero() {
		return c.drainOrStop(ctx, id)
	}

	now := time.Now()
	update := &sessionapi.Session{
		Sandbox:        id,
		Generation:     generation,
		Phase:          sessionapi.ACTIVATING,
		Activity:       sessionapi.UNKNOWN,
		LastTransition: now,
	}
	if s.Sandbox != "" {
		update.Incarnation = []sessionapi.Incarnation{{
			Generation: s.Generation, Sandbox: s.Sandbox.String(), StartedAt: s.LastTransition, EndedAt: now, Reason: "replaced",
		}}
	}
	return c.patch(ctx, s.ID, update)
}

func (c *Controller) suspend(ctx context.Context, s *sessionapi.Session) error {
	children, err := c.children(ctx, s.ID)
	if err != nil {
		return err
	}
	waiting := false
	for _, sb := range children {
		ack, err := c.teardownDone(ctx, sb.ID)
		if err != nil {
			return err
		}
		if ack {
			continue
		}
		waiting = true
		if !terminal(sb.Status) {
			if err := c.drainOrStop(ctx, sb.ID); err != nil {
				return err
			}
		}
	}
	if s.Sandbox != "" {
		ack, err := c.teardownDone(ctx, s.Sandbox)
		if err != nil {
			return err
		}
		if !ack {
			waiting = true // A deleted child is not proof that it stopped.
		}
	}
	if waiting {
		return c.transition(ctx, s, sessionapi.SUSPENDING)
	}
	if s.Sandbox == "" && s.Phase == sessionapi.INACTIVE {
		// Historical acknowledged children need no new history entry. An
		// unpublished generation+1 child still must be consumed, however.
		candidate := sandboxID(s.ID, s.Generation+1)
		unpublished := false
		for _, child := range children {
			unpublished = unpublished || child.ID == candidate
		}
		if !unpublished {
			return nil
		}
	}
	if s.Sandbox == "" && len(children) == 0 {
		return c.transition(ctx, s, sessionapi.INACTIVE)
	}
	if err := c.settleSuspended(ctx, s, "suspended", children); err != nil {
		return err
	}
	return nil
}

// drainOrStop uses the Sandbox revision to arbitrate with concurrent activity
// reports. If an active report wins the race, the STOPPED patch conflicts and
// the next reconcile sees the activity instead of interrupting new work.
func (c *Controller) drainOrStop(ctx context.Context, id entity.Id) error {
	resp, err := c.EAC.Get(ctx, id.String())
	if errors.Is(err, cond.ErrNotFound{}) {
		return nil
	}
	if err != nil {
		return err
	}
	var sb compute.Sandbox
	sb.Decode(resp.Entity().Entity())
	if terminal(sb.Status) {
		return nil
	}
	if sb.Status == compute.RUNNING {
		now := time.Now()
		if sb.ShutdownAt.IsZero() {
			_, err = c.EAC.Patch(ctx, entity.New(entity.DBId, id,
				(&compute.Sandbox{ShutdownAt: now.Add(shutdownGrace)}).Encode).Attrs(), resp.Entity().Revision())
			return err
		}
		if now.Before(sb.ShutdownAt) ||
			(sb.SelfReportedActivity(now) == compute.ACTIVE && sb.Activity.ReportedAt.After(sb.ShutdownAt.Add(-shutdownGrace))) {
			return nil
		}
	}
	_, err = c.EAC.Patch(ctx, entity.New(entity.DBId, id,
		(&compute.Sandbox{Status: compute.STOPPED}).Encode).Attrs(), resp.Entity().Revision())
	return err
}

// A revision-checked Replace clears the singular current ref without touching
// the durable specification or external owner metadata.
func (c *Controller) settleSuspended(ctx context.Context, s *sessionapi.Session, reason string, children []*compute.Sandbox) error {
	now := time.Now()
	for _, child := range children {
		if child.ID == s.Sandbox || child.ID == sandboxID(s.ID, s.Generation+1) {
			generation := s.Generation
			if child.ID != s.Sandbox {
				generation++
			}
			s.Incarnation = append(s.Incarnation, sessionapi.Incarnation{
				Generation: generation, Sandbox: child.ID.String(), StartedAt: s.LastTransition, EndedAt: now, Reason: reason,
			})
			if generation > s.Generation {
				s.Generation = generation
			}
		}
	}
	if s.Sandbox != "" {
		found := false
		for _, child := range children {
			found = found || child.ID == s.Sandbox
		}
		if !found {
			s.Incarnation = append(s.Incarnation, sessionapi.Incarnation{
				Generation: s.Generation, Sandbox: s.Sandbox.String(), StartedAt: s.LastTransition, EndedAt: now, Reason: reason,
			})
		}
	}
	s.Sandbox = ""
	s.Phase = sessionapi.INACTIVE
	s.Activity = sessionapi.UNKNOWN
	s.LastTransition = now

	resp, err := c.EAC.Get(ctx, s.ID.String())
	if err != nil {
		return err
	}
	e := entity.New(resp.Entity().Attrs())
	e.Remove(sessionapi.SessionSandboxId)
	e.Remove(sessionapi.SessionPhaseId)
	e.Remove(sessionapi.SessionActivityId)
	e.Remove(sessionapi.SessionActivityAtId)
	e.Remove(sessionapi.SessionIdleSinceId)
	e.Remove(sessionapi.SessionGenerationId)
	e.Remove(sessionapi.SessionLastTransitionId)
	e.Remove(sessionapi.SessionIncarnationId)
	attrs := e.Attrs()
	for _, attr := range (&sessionapi.Session{Phase: s.Phase, Activity: s.Activity, Generation: s.Generation, LastTransition: now, Incarnation: s.Incarnation}).Encode() {
		if attr.ID == sessionapi.SessionPhaseId || attr.ID == sessionapi.SessionActivityId || attr.ID == sessionapi.SessionGenerationId || attr.ID == sessionapi.SessionLastTransitionId || attr.ID == sessionapi.SessionIncarnationId {
			attrs = append(attrs, attr)
		}
	}
	_, err = c.EAC.Replace(ctx, attrs, resp.Entity().Revision())
	return err
}

// Delete is restart-safe: the deterministic current sandbox is only ever moved
// toward STOPPED. Durable disk ownership remains untouched.
func (c *Controller) Delete(ctx context.Context, id entity.Id) error {
	if err := c.notifyDeleted(ctx, id); err != nil {
		return err
	}
	children, err := c.children(ctx, id)
	if err != nil {
		return err
	}
	for _, sb := range children {
		if !terminal(sb.Status) {
			if err := c.stopSandbox(ctx, sb.ID); err != nil {
				return err
			}
		}
	}
	return nil
}

// SweepOrphans repairs deletion cleanup when a coordinator misses a tombstone
// during restart or watch compaction. It never touches standalone sandboxes.
func (c *Controller) SweepOrphans(ctx context.Context) error {
	c.sharedAdmission.Lock()
	defer c.sharedAdmission.Unlock()
	if err := c.parkIdleSessions(ctx, time.Now()); err != nil {
		return err
	}
	if err := c.sweepDeletedBindings(ctx); err != nil {
		return err
	}
	if err := c.sweepSharedSlots(ctx); err != nil {
		return err
	}
	resp, err := c.EAC.List(ctx, entity.Ref(entity.EntityKind, compute.KindSandbox))
	if err != nil {
		return err
	}
	for _, e := range resp.Values() {
		var sb compute.Sandbox
		sb.Decode(e.Entity())
		if sb.SessionInfo.Group != "" && !terminal(sb.Status) {
			slots, err := c.EAC.List(ctx, entity.String(sessionapi.SlotSandboxId, sb.ID.String()))
			if err != nil {
				return err
			}
			if len(slots.Values()) == 0 {
				current, err := c.EAC.Get(ctx, sb.ID.String())
				if err != nil {
					return err
				}
				var host compute.Sandbox
				host.Decode(current.Entity().Entity())
				if terminal(host.Status) {
					continue
				}
				if host.SessionInfo.ClosingAt.IsZero() {
					host.SessionInfo.ClosingAt = time.Now()
					_, err = c.EAC.Patch(ctx, entity.New(entity.DBId, sb.ID,
						(&compute.Sandbox{SessionInfo: host.SessionInfo}).Encode).Attrs(), current.Entity().Revision())
					if errors.Is(err, cond.ErrConflict{}) {
						continue // An admission won the host revision.
					}
					if err != nil {
						return err
					}
					current, err = c.EAC.Get(ctx, sb.ID.String())
					if err != nil {
						return err
					}
				}
				slots, err = c.EAC.List(ctx, entity.String(sessionapi.SlotSandboxId, sb.ID.String()))
				if err != nil {
					return err
				}
				if len(slots.Values()) == 0 {
					_, err = c.EAC.Patch(ctx, entity.New(entity.DBId, sb.ID,
						(&compute.Sandbox{Status: compute.STOPPED}).Encode).Attrs(), current.Entity().Revision())
					if err != nil && !errors.Is(err, cond.ErrConflict{}) {
						return err
					}
				}
			}
			continue
		}
		if sb.SessionInfo.Owner == "" || terminal(sb.Status) {
			continue
		}
		_, err := c.EAC.Get(ctx, sb.SessionInfo.Owner.String())
		if errors.Is(err, cond.ErrNotFound{}) {
			if err := c.stopSandbox(ctx, sb.ID); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
	}
	return nil
}

func (c *Controller) stopSandbox(ctx context.Context, id entity.Id) error {
	_, err := c.EAC.Patch(ctx, entity.New(entity.Ref(entity.DBId, id), (&compute.Sandbox{Status: compute.STOPPED}).Encode).Attrs(), 0)
	if errors.Is(err, cond.ErrNotFound{}) {
		return nil
	}
	return err
}

func (c *Controller) transition(ctx context.Context, s *sessionapi.Session, phase sessionapi.SessionPhase) error {
	if s.Phase == phase {
		return nil
	}
	return c.patch(ctx, s.ID, &sessionapi.Session{Phase: phase, LastTransition: time.Now()})
}

func (c *Controller) fail(ctx context.Context, id entity.Id, cause error) error {
	if err := c.patch(ctx, id, &sessionapi.Session{Phase: sessionapi.FAILED, Failure: cause.Error(), LastTransition: time.Now()}); err != nil {
		return err
	}
	return cause
}

func (c *Controller) getSandbox(ctx context.Context, id entity.Id) (*compute.Sandbox, error) {
	resp, err := c.EAC.Get(ctx, id.String())
	if err != nil {
		return nil, err
	}
	var sb compute.Sandbox
	sb.Decode(resp.Entity().Entity())
	return &sb, nil
}

func (c *Controller) children(ctx context.Context, id entity.Id) ([]*compute.Sandbox, error) {
	resp, err := c.EAC.List(ctx, entity.Ref(compute.SessionInfoOwnerId, id))
	if err != nil {
		return nil, err
	}
	var children []*compute.Sandbox
	for _, e := range resp.Values() {
		var sb compute.Sandbox
		sb.Decode(e.Entity())
		children = append(children, &sb)
	}
	return children, nil
}

func (c *Controller) teardownDone(ctx context.Context, id entity.Id) (bool, error) {
	resp, err := c.EAC.Get(ctx, computeapi.TeardownID(id).String())
	if errors.Is(err, cond.ErrNotFound{}) {
		sandbox, getErr := c.EAC.Get(ctx, id.String())
		if errors.Is(getErr, cond.ErrNotFound{}) {
			return false, nil
		}
		if getErr != nil {
			return false, getErr
		}
		var sb compute.Sandbox
		sb.Decode(sandbox.Entity().Entity())
		var schedule compute.Schedule
		if !terminal(sb.Status) || !schedule.Is(sandbox.Entity().Entity()) {
			return false, nil
		}
		schedule.Decode(sandbox.Entity().Entity())
		if schedule.Key.Node == "" {
			return false, nil
		}
		_, nodeErr := c.EAC.Get(ctx, schedule.Key.Node.String())
		if errors.Is(nodeErr, cond.ErrNotFound{}) {
			return true, nil // The node was removed; runner teardown cannot arrive.
		}
		return false, nodeErr
	}
	if err != nil {
		return false, err
	}
	var ack compute.SandboxTeardown
	ack.Decode(resp.Entity().Entity())
	return ack.Sandbox == id.String(), nil
}

func (c *Controller) patch(ctx context.Context, id entity.Id, update *sessionapi.Session) error {
	if update.Sandbox != "" {
		resp, err := c.EAC.Get(ctx, id.String())
		if err != nil {
			return err
		}
		var current sessionapi.Session
		current.Decode(resp.Entity().Entity())
		e := entity.New(resp.Entity().Attrs())
		if current.Sandbox != update.Sandbox {
			e.Remove(sessionapi.SessionActivityAtId)
			e.Remove(sessionapi.SessionIdleSinceId)
			update.Activity = sessionapi.UNKNOWN
		}
		for _, attr := range update.Encode() {
			e.Set(attr)
		}
		_, err = c.EAC.Replace(ctx, e.Attrs(), resp.Entity().Revision())
		return err
	}
	_, err := c.EAC.Patch(ctx, entity.New(entity.Ref(entity.DBId, id), update.Encode).Attrs(), 0)
	return err
}

func terminal(status compute.SandboxStatus) bool {
	return status == compute.STOPPED || status == compute.DEAD
}

func sandboxID(session entity.Id, generation int64) entity.Id {
	name := sha256.Sum256([]byte(session))
	return entity.Id(fmt.Sprintf("sandbox/session-%x-%d", name[:16], generation))
}

// The schema generator cannot reference a component in another domain. Keep
// the wire-identical Session copy typed at its boundary and convert here.
func computeSpec(spec sessionapi.SandboxSpec) (compute.SandboxSpec, error) {
	b, err := json.Marshal(spec)
	if err != nil {
		return compute.SandboxSpec{}, err
	}
	var out compute.SandboxSpec
	err = json.Unmarshal(b, &out)
	return out, err
}
