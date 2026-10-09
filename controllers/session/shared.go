package session

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	computeapi "miren.dev/runtime/api/compute"
	compute "miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	shared "miren.dev/runtime/api/session"
	sessionapi "miren.dev/runtime/api/session/session_v1alpha"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/types"
)

// Shared Sessions reserve slots in controller-managed hosts. A slot is created
// before the host and binding so a restart can recover an interrupted attach.
func (c *Controller) runShared(ctx context.Context, s *sessionapi.Session) error {
	if s.Disk != "" {
		return c.fail(ctx, s.ID, fmt.Errorf("shared session %s cannot mount a per-Session disk", s.ID))
	}
	if s.Phase == sessionapi.FAILED && s.Failure == serviceMissingFailure(s.App, s.Service) {
		return nil
	}
	if s.Version != "" {
		appResp, err := c.EAC.Get(ctx, s.App.String())
		if err != nil {
			return err
		}
		var app core_v1alpha.App
		app.Decode(appResp.Entity().Entity())
		if app.ActiveVersion != s.Version {
			return nil // The app watcher will update this Session before it can boot a host.
		}
	}
	group := sharedGroup(s)
	id := shared.BindingID(s.ID)
	resp, err := c.EAC.Get(ctx, id.String())
	var binding sessionapi.Binding
	if err == nil {
		binding.Decode(resp.Entity().Entity())
		if !binding.DetachedAt.IsZero() {
			return nil // Resume waits for cleanup and reservation release.
		}
		if binding.Session != s.ID.String() || !binding.DeletedAt.IsZero() {
			return c.fail(ctx, s.ID, fmt.Errorf("session %s cannot reuse a shared sandbox binding", s.ID))
		}
	} else if errors.Is(err, cond.ErrNotFound{}) {
		if s.Sandbox != "" {
			return c.fail(ctx, s.ID, fmt.Errorf("session %s cannot change sandbox ownership mode while attached", s.ID))
		}
		// Recover a reservation made just before a coordinator restart.
		slots, err := c.EAC.List(ctx, entity.String(sessionapi.SlotSessionId, s.ID.String()))
		if err != nil {
			return err
		}
		var hostID entity.Id
		if len(slots.Values()) != 0 {
			var slot sessionapi.Slot
			slot.Decode(slots.Values()[0].Entity())
			host, err := c.getSandbox(ctx, entity.Id(slot.Sandbox))
			if err == nil && (terminal(host.Status) || !host.SessionInfo.ClosingAt.IsZero()) {
				if terminal(host.Status) {
					ack, err := c.teardownDone(ctx, host.ID)
					if err != nil || !ack {
						return err
					}
				}
				if _, err := c.EAC.Delete(ctx, slot.ID.String()); err != nil {
					return err
				}
			} else if err != nil && !errors.Is(err, cond.ErrNotFound{}) {
				return err
			} else {
				hostID = entity.Id(slot.Sandbox)
			}
		}
		if hostID == "" {
			hostID, err = c.reserveSharedSlot(ctx, s, group)
			if err != nil {
				return err
			}
		}
		binding = sessionapi.Binding{Session: s.ID.String(), Sandbox: hostID.String()}
		if err := c.ensureSharedHost(ctx, s, group, hostID); err != nil {
			return err
		}
		_, err = c.EAC.Create(ctx, entity.New(entity.DBId, id, binding.Encode).Attrs())
		if err != nil {
			return err
		}
		resp, err = c.EAC.Get(ctx, id.String())
		if err != nil {
			return err
		}
	} else {
		return err
	}
	host, err := c.getSandbox(ctx, entity.Id(binding.Sandbox))
	missing := errors.Is(err, cond.ErrNotFound{})
	if err != nil && !missing {
		return err
	}
	if missing {
		host = &compute.Sandbox{ID: entity.Id(binding.Sandbox), Status: compute.DEAD}
	} else {
		// Already-bound hosts may still use the old app+opaque-group key. Keep
		// their assignments until replacement, but never admit new Sessions there.
		if (host.SessionInfo.Group != group && host.Spec.LogEntity != s.App.String()) ||
			host.SessionInfo.Capacity != s.MaxSessionsPerSandbox || host.SessionInfo.Owner != "" {
			return c.fail(ctx, s.ID, fmt.Errorf("session %s cannot change its shared sandbox group", s.ID))
		}
		if retiring, err := c.retireOutdatedHost(ctx, s, host); retiring || err != nil {
			return err
		}
	}
	if terminal(host.Status) {
		ack, err := c.teardownDone(ctx, host.ID)
		if err != nil {
			return err
		}
		if !ack {
			return nil // Wait for runner teardown before assigning a replacement.
		}
		// If the coordinator crashed after claiming a replacement but before
		// updating the binding, reuse that reservation instead of taking a third.
		slots, err := c.EAC.List(ctx, entity.String(sessionapi.SlotSessionId, s.ID.String()))
		if err != nil {
			return err
		}
		var next entity.Id
		for _, e := range slots.Values() {
			var slot sessionapi.Slot
			slot.Decode(e.Entity())
			if slot.Sandbox != binding.Sandbox {
				candidateID := entity.Id(slot.Sandbox)
				candidate, err := c.getSandbox(ctx, candidateID)
				if err == nil && candidate.SessionInfo.Group == group && !terminal(candidate.Status) &&
					(s.Version == "" || candidate.Spec.Version == s.Version) {
					next = candidate.ID
					break
				}
				if errors.Is(err, cond.ErrNotFound{}) {
					done, err := c.teardownDone(ctx, candidateID)
					if err != nil {
						return err
					}
					if done {
						if _, err := c.EAC.Delete(ctx, slot.ID.String()); err != nil {
							return err
						}
						continue
					}
					if strings.HasPrefix(candidateID.String(), strings.TrimSuffix(sharedHostID(group, 0).String(), "0")) {
						next = candidateID // A reserved host may not have been created before a crash.
						break
					}
					return nil // An old-group host is missing without teardown proof.
				}
				if err != nil {
					return err
				}
			}
		}
		if next == "" {
			next, err = c.reserveSharedSlot(ctx, s, group)
			if err != nil {
				return err
			}
		}
		if err := c.ensureSharedHost(ctx, s, group, next); err != nil {
			return err
		}
		_, err = c.EAC.Patch(ctx, entity.New(entity.DBId, binding.ID,
			(&sessionapi.Binding{Sandbox: next.String()}).Encode).Attrs(), resp.Entity().Revision())
		if err != nil {
			return err
		}
		binding.Sandbox = next.String()
		host, err = c.getSandbox(ctx, next)
		if err != nil {
			return err
		}
	}
	phase := sessionapi.ACTIVATING
	if host.Status == compute.RUNNING {
		phase = sessionapi.READY
	}
	if terminal(host.Status) {
		phase = sessionapi.FAILED
	}
	if s.Sandbox == host.ID && s.Phase == phase {
		return nil
	}
	return c.patch(ctx, s.ID, &sessionapi.Session{
		Sandbox: host.ID, Phase: phase, LastTransition: time.Now(),
	})
}

func (c *Controller) retireOutdatedHost(ctx context.Context, s *sessionapi.Session, host *compute.Sandbox) (bool, error) {
	if terminal(host.Status) || s.Version == "" || host.Spec.Version == s.Version {
		return false, nil
	}
	if host.SessionInfo.ClosingAt.IsZero() {
		current, err := c.EAC.Get(ctx, host.ID.String())
		if err != nil {
			return true, err
		}
		var latest compute.Sandbox
		latest.Decode(current.Entity().Entity())
		latest.SessionInfo.ClosingAt = time.Now()
		_, err = c.EAC.Patch(ctx, entity.New(entity.DBId, host.ID,
			entity.Component(compute.SandboxSessionInfoId, latest.SessionInfo.Encode())).Attrs(), current.Entity().Revision())
		return true, err // Close admission before issuing a shutdown notice.
	}
	return true, c.drainOrStop(ctx, host.ID)
}

func sharedGroup(s *sessionapi.Session) string {
	// Separate components so opaque keys cannot collide with app or service names.
	sum := sha256.Sum256([]byte(fmt.Sprintf("%q:%q:%q", s.App, s.Service, s.Group)))
	return fmt.Sprintf("%x", sum[:16])
}

func sharedHostID(group string, number int64) entity.Id {
	return entity.Id(fmt.Sprintf("sandbox/session-group-%s-%d", group, number))
}

func (c *Controller) nextSharedHost(ctx context.Context, group string) (entity.Id, error) {
	id := entity.Id("session_group_counter/" + group)
	for {
		resp, err := c.EAC.Get(ctx, id.String())
		if errors.Is(err, cond.ErrNotFound{}) {
			// Seed the counter from existing hosts when upgrading a cluster that
			// already has shared sandboxes but predates this allocator.
			next := int64(0)
			prefix := sharedHostID(group, 0).String()
			prefix = strings.TrimSuffix(prefix, "0")
			advance := func(sandbox string) {
				if number, err := strconv.ParseInt(strings.TrimPrefix(sandbox, prefix), 10, 64); err == nil && strings.HasPrefix(sandbox, prefix) && number >= next {
					next = number + 1
				}
			}
			hosts, listErr := c.EAC.List(ctx, entity.String(compute.SessionInfoGroupId, group))
			if listErr != nil {
				return "", listErr
			}
			for _, host := range hosts.Values() {
				advance(host.Id())
			}
			// Old allocators used contiguous host numbers. Probe only this group's
			// retired IDs rather than listing every teardown in the cluster.
			for {
				_, err := c.EAC.Get(ctx, computeapi.TeardownID(sharedHostID(group, next)).String())
				if errors.Is(err, cond.ErrNotFound{}) {
					break
				}
				if err != nil {
					return "", err
				}
				next++
			}
			_, err = c.EAC.Create(ctx, entity.New(entity.DBId, id,
				(&sessionapi.GroupCounter{NextNumber: next + 1}).Encode).Attrs())
			if err == nil {
				return sharedHostID(group, next), nil
			}
		} else if err == nil {
			var counter sessionapi.GroupCounter
			counter.Decode(resp.Entity().Entity())
			_, err = c.EAC.Patch(ctx, entity.New(entity.DBId, id,
				(&sessionapi.GroupCounter{NextNumber: counter.NextNumber + 1}).Encode).Attrs(), resp.Entity().Revision())
			if err == nil {
				return sharedHostID(group, counter.NextNumber), nil
			}
		}
		if !errors.Is(err, cond.ErrConflict{}) {
			return "", err
		}
	}
}

func (c *Controller) reserveSharedSlot(ctx context.Context, s *sessionapi.Session, group string) (entity.Id, error) {
admission:
	for {
		resp, err := c.EAC.List(ctx, entity.String(compute.SessionInfoGroupId, group))
		if err != nil {
			return "", err
		}
		for _, e := range resp.Values() {
			var sb compute.Sandbox
			sb.Decode(e.Entity())
			if terminal(sb.Status) || !sb.SessionInfo.ClosingAt.IsZero() || sb.SessionInfo.Group != group ||
				(s.Version != "" && sb.Spec.Version != s.Version) {
				continue
			}
			if sb.SessionInfo.Capacity != s.MaxSessionsPerSandbox {
				return "", c.fail(ctx, s.ID, fmt.Errorf("app %s shared host already has capacity %d (requested %d)", s.App, sb.SessionInfo.Capacity, s.MaxSessionsPerSandbox))
			}
			reserved, retry, err := c.trySharedSlot(ctx, s, group, sb.ID, true)
			if err != nil {
				return "", err
			}
			if reserved {
				return sb.ID, nil
			}
			if retry {
				continue admission // The host changed during admission; refresh the list.
			}
		}
		host, err := c.nextSharedHost(ctx, group)
		if err != nil {
			return "", err
		}
		reserved, _, err := c.trySharedSlot(ctx, s, group, host, false)
		if err != nil {
			return "", err
		}
		if reserved {
			return host, nil
		}
	}
}

func (c *Controller) trySharedSlot(ctx context.Context, s *sessionapi.Session, group string, host entity.Id, existing bool) (reserved, retry bool, err error) {
	for index := int64(0); index < s.MaxSessionsPerSandbox; index++ {
		slotID := shared.SlotID(host, index)
		_, err := c.EAC.Create(ctx, entity.New(entity.DBId, slotID,
			(&sessionapi.Slot{Session: s.ID.String(), Sandbox: host.String()}).Encode).Attrs())
		if err == nil {
			if existing {
				current, err := c.EAC.Get(ctx, host.String())
				if err != nil {
					return false, false, err
				}
				var latest compute.Sandbox
				latest.Decode(current.Entity().Entity())
				if !latest.SessionInfo.ClosingAt.IsZero() || terminal(latest.Status) || latest.SessionInfo.Group != group {
					if _, err := c.EAC.Delete(ctx, slotID.String()); err != nil {
						return false, false, err
					}
					return false, true, nil
				}
				latest.SessionInfo.Epoch++
				_, err = c.EAC.Patch(ctx, entity.New(entity.DBId, host,
					entity.Component(compute.SandboxSessionInfoId, latest.SessionInfo.Encode())).Attrs(), current.Entity().Revision())
				if errors.Is(err, cond.ErrConflict{}) {
					if _, err := c.EAC.Delete(ctx, slotID.String()); err != nil {
						return false, false, err
					}
					return false, true, nil
				}
				if err != nil {
					return false, false, err
				}
			}
			return true, false, nil
		}
		if !errors.Is(err, cond.ErrConflict{}) {
			return false, false, err
		}
	}
	return false, false, nil
}

func (c *Controller) ensureSharedHost(ctx context.Context, s *sessionapi.Session, group string, id entity.Id) error {
	if host, err := c.getSandbox(ctx, id); err == nil {
		if host.SessionInfo.Group != group || host.SessionInfo.Capacity != s.MaxSessionsPerSandbox || !host.SessionInfo.ClosingAt.IsZero() || terminal(host.Status) {
			return fmt.Errorf("sandbox %s is not available for session group %s", id, group)
		}
		return nil
	} else if !errors.Is(err, cond.ErrNotFound{}) {
		return err
	}
	spec, err := computeSpec(s.Spec)
	if err != nil {
		return err
	}
	spec.LogEntity = s.App.String()
	spec.LogAttribute = append(spec.LogAttribute, types.LabelSet("miren.session_group", group, "miren.sandbox", id.String())...)
	_, err = c.EAC.Create(ctx, entity.New(
		(&core_v1alpha.Metadata{Name: strings.TrimPrefix(id.String(), "sandbox/"), Labels: types.LabelSet("service", s.Service)}).Encode,
		entity.DBId, id,
		(&compute.Sandbox{Status: compute.PENDING, Spec: spec,
			SessionInfo: compute.SessionInfo{Group: group, Capacity: s.MaxSessionsPerSandbox}}).Encode,
	).Attrs())
	if errors.Is(err, cond.ErrConflict{}) {
		return nil // Another Session created the same host.
	}
	return err
}

func (c *Controller) suspendShared(ctx context.Context, s *sessionapi.Session) error {
	current, err := c.EAC.Get(ctx, s.ID.String())
	if err != nil {
		return err
	}
	var latest sessionapi.Session
	latest.Decode(current.Entity().Entity())
	if latest.DesiredState != sessionapi.SUSPENDED {
		return nil // A resume won before withdrawal started.
	}
	resp, err := c.EAC.Get(ctx, shared.BindingID(s.ID).String())
	if err == nil {
		if latest.Phase != sessionapi.SUSPENDING {
			_, err = c.EAC.Patch(ctx, entity.New(entity.DBId, s.ID,
				(&sessionapi.Session{Phase: sessionapi.SUSPENDING}).Encode).Attrs(), current.Entity().Revision())
			if err != nil {
				return err
			}
		}
		var binding sessionapi.Binding
		binding.Decode(resp.Entity().Entity())
		host, err := c.getSandbox(ctx, entity.Id(binding.Sandbox))
		if err == nil {
			if retiring, err := c.retireOutdatedHost(ctx, s, host); retiring || err != nil {
				return err
			}
		} else if !errors.Is(err, cond.ErrNotFound{}) {
			return err
		}
		if binding.DetachedAt.IsZero() {
			_, err = c.EAC.Patch(ctx, entity.New(entity.DBId, binding.ID,
				(&sessionapi.Binding{DetachedAt: time.Now()}).Encode).Attrs(), resp.Entity().Revision())
			if err != nil {
				return err
			}
		}
		return c.transition(ctx, s, sessionapi.SUSPENDING)
	} else if !errors.Is(err, cond.ErrNotFound{}) {
		return err
	}
	return c.clearSharedAssignment(ctx, s.ID)
}

func (c *Controller) clearSharedAssignment(ctx context.Context, id entity.Id) error {
	resp, err := c.EAC.Get(ctx, id.String())
	if errors.Is(err, cond.ErrNotFound{}) {
		return nil
	}
	if err != nil {
		return err
	}
	var s sessionapi.Session
	s.Decode(resp.Entity().Entity())
	phase := sessionapi.INACTIVE
	if s.DesiredState == sessionapi.RUNNING {
		phase = sessionapi.PENDING
	}
	if s.Sandbox == "" && s.Phase == phase && s.Activity == sessionapi.UNKNOWN &&
		s.ActivityAt.IsZero() && s.IdleSince.IsZero() {
		return nil
	}
	e := entity.New(resp.Entity().Attrs())
	e.Remove(sessionapi.SessionSandboxId)
	e.Remove(sessionapi.SessionPhaseId)
	e.Remove(sessionapi.SessionActivityId)
	e.Remove(sessionapi.SessionActivityAtId)
	e.Remove(sessionapi.SessionIdleSinceId)
	e.Remove(sessionapi.SessionLastTransitionId)
	attrs := e.Attrs()
	for _, attr := range (&sessionapi.Session{Phase: phase, Activity: sessionapi.UNKNOWN,
		LastTransition: time.Now()}).Encode() {
		if attr.ID == sessionapi.SessionPhaseId || attr.ID == sessionapi.SessionActivityId || attr.ID == sessionapi.SessionLastTransitionId {
			attrs = append(attrs, attr)
		}
	}
	_, err = c.EAC.Replace(ctx, attrs, resp.Entity().Revision())
	return err
}

func (c *Controller) notifyDeleted(ctx context.Context, id entity.Id) error {
	resp, err := c.EAC.Get(ctx, shared.BindingID(id).String())
	if errors.Is(err, cond.ErrNotFound{}) {
		return nil
	}
	if err != nil {
		return err
	}
	var binding sessionapi.Binding
	binding.Decode(resp.Entity().Entity())
	if !binding.DeletedAt.IsZero() {
		return nil
	}
	_, err = c.EAC.Patch(ctx, entity.New(entity.DBId, binding.ID,
		(&sessionapi.Binding{DeletedAt: time.Now()}).Encode).Attrs(), resp.Entity().Revision())
	return err
}

func (c *Controller) sweepDeletedBindings(ctx context.Context) error {
	resp, err := c.EAC.List(ctx, entity.Ref(entity.EntityKind, sessionapi.KindBinding))
	if err != nil {
		return err
	}
	for _, e := range resp.Values() {
		var binding sessionapi.Binding
		binding.Decode(e.Entity())
		if !binding.DeletedAt.IsZero() || !binding.DetachedAt.IsZero() {
			if binding.AcknowledgedAt.IsZero() {
				done, err := c.teardownDone(ctx, entity.Id(binding.Sandbox))
				if err != nil {
					return err
				}
				if !done {
					continue
				}
				if _, err := c.EAC.Patch(ctx, entity.New(entity.DBId, binding.ID,
					(&sessionapi.Binding{AcknowledgedAt: time.Now()}).Encode).Attrs(), e.Revision()); err != nil {
					return err
				}
			}
			slots, err := c.EAC.List(ctx, entity.String(sessionapi.SlotSessionId, binding.Session))
			if err != nil {
				return err
			}
			if len(slots.Values()) != 0 {
				continue // The reservation still protects the workload's cleanup.
			}
			if !binding.DetachedAt.IsZero() {
				if err := c.clearSharedAssignment(ctx, entity.Id(binding.Session)); err != nil {
					return err
				}
			}
			if _, err := c.EAC.Delete(ctx, binding.ID.String()); err != nil && !errors.Is(err, cond.ErrNotFound{}) {
				return err
			}
			continue
		}
		_, err := c.EAC.Get(ctx, binding.Session)
		if errors.Is(err, cond.ErrNotFound{}) {
			if err := c.notifyDeleted(ctx, entity.Id(binding.Session)); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
	}
	return nil
}

// A reservation is held until the workload acknowledges deletion. This keeps
// its host alive long enough to run cleanup and makes capacity admission atomic.
func (c *Controller) sweepSharedSlots(ctx context.Context) error {
	resp, err := c.EAC.List(ctx, entity.Ref(entity.EntityKind, sessionapi.KindSlot))
	if err != nil {
		return err
	}
	for _, e := range resp.Values() {
		var slot sessionapi.Slot
		slot.Decode(e.Entity())
		bindingResp, err := c.EAC.Get(ctx, shared.BindingID(entity.Id(slot.Session)).String())
		if errors.Is(err, cond.ErrNotFound{}) {
			// An attach can be interrupted after reserving its slot.
			if _, err := c.EAC.Get(ctx, slot.Session); err == nil {
				continue
			} else if !errors.Is(err, cond.ErrNotFound{}) {
				return err
			}
			if _, err := c.EAC.Get(ctx, slot.Sandbox); err == nil {
				// The host exists: publish the binding before its deletion notice.
				// A concurrent attach can create the same binding idempotently.
				_, err = c.EAC.Create(ctx, entity.New(entity.DBId, shared.BindingID(entity.Id(slot.Session)),
					(&sessionapi.Binding{Session: slot.Session, Sandbox: slot.Sandbox}).Encode).Attrs())
				if err != nil && !errors.Is(err, cond.ErrConflict{}) {
					return err
				}
				if err := c.notifyDeleted(ctx, entity.Id(slot.Session)); err != nil {
					return err
				}
				continue
			} else if !errors.Is(err, cond.ErrNotFound{}) {
				return err
			}
			// A live reconcile may still be between reservation and host creation.
			if time.Since(time.UnixMilli(e.CreatedAt())) < 5*time.Minute {
				continue
			}
		} else if err != nil {
			return err
		} else {
			var binding sessionapi.Binding
			binding.Decode(bindingResp.Entity().Entity())
			if binding.Sandbox != slot.Sandbox {
				ack, err := c.teardownDone(ctx, entity.Id(slot.Sandbox))
				if err != nil {
					return err
				}
				if !ack {
					continue
				}
			} else if binding.AcknowledgedAt.IsZero() {
				done, err := c.teardownDone(ctx, entity.Id(slot.Sandbox))
				if err != nil {
					return err
				}
				if !done {
					continue
				}
			}
		}
		if _, err := c.EAC.Delete(ctx, slot.ID.String()); err != nil && !errors.Is(err, cond.ErrNotFound{}) {
			return err
		}
	}
	return nil
}
