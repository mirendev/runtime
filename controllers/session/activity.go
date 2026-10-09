package session

import (
	"context"
	"errors"
	"time"

	sessionapi "miren.dev/runtime/api/session/session_v1alpha"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/entity"
)

func (c *Controller) parkIdleSessions(ctx context.Context, now time.Time) error {
	resp, err := c.EAC.List(ctx, entity.Ref(entity.EntityKind, sessionapi.KindSession))
	if err != nil {
		return err
	}
	for _, value := range resp.Values() {
		var s sessionapi.Session
		s.Decode(value.Entity())
		if s.DesiredState != sessionapi.RUNNING || s.IdleTimeoutSeconds <= 0 ||
			s.SelfReportedActivity(now) != sessionapi.IDLE || s.IdleSince.IsZero() ||
			now.Sub(s.IdleSince) < time.Duration(s.IdleTimeoutSeconds)*time.Second {
			continue
		}
		// Compete with active reports on the same revision. Once this wins,
		// metadata refuses new work; suspension performs cleanup before reuse.
		_, err := c.EAC.Patch(ctx, entity.New(entity.DBId, s.ID,
			(&sessionapi.Session{DesiredState: sessionapi.SUSPENDED, Phase: sessionapi.SUSPENDING}).Encode).Attrs(), value.Revision())
		if err != nil && !errors.Is(err, cond.ErrConflict{}) {
			return err
		}
		if err == nil {
			c.Log.Info("parking idle session", "session", s.ID, "idle_since", s.IdleSince)
		}
	}
	return nil
}
