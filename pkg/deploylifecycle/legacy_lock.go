package deploylifecycle

import (
	"context"
	"errors"
	"fmt"

	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/schema"
)

// The standalone lock is retained as a downgrade compatibility shadow for one
// supported window. New code treats app.deployment_lock as canonical, but an
// older binary only checks deploy-lock/<app>. Writing both prevents an upgrade
// or downgrade boundary from splitting mutual exclusion across two locations.
const (
	legacyLockKind           = entity.Id("dev.miren.core/deployment_lock")
	lockOwnerReservationKind = entity.Id("dev.miren.core/deployment_lock_owner_reservation")
	legacyLockAppName        = entity.Id("dev.miren.core/deployment_lock.app_name")
	legacyLockDeploymentID   = entity.Id("dev.miren.core/deployment_lock.deployment_id")
	legacyLockAcquiredAt     = entity.Id("dev.miren.core/deployment_lock.acquired_at")
	legacyLockExpiresAt      = entity.Id("dev.miren.core/deployment_lock.expires_at")
)

func init() {
	// Fresh clusters still need the old attribute definitions so the current
	// binary can write a shadow that a downgraded binary understands. Register
	// them outside the generated core model to keep the standalone entity out of
	// the canonical API.
	schema.Register("dev.miren.core.deployment-lock-compat", "v1alpha", func(sb *schema.SchemaBuilder) {
		sb.Singleton(string(legacyLockKind))
		sb.Singleton(string(lockOwnerReservationKind))
		sb.String("app_name", string(legacyLockAppName), schema.Indexed)
		sb.String("deployment_id", string(legacyLockDeploymentID))
		sb.Time("acquired_at", string(legacyLockAcquiredAt))
		sb.Time("expires_at", string(legacyLockExpiresAt), schema.Indexed)
	})
}

func legacyLockID(appName string) entity.Id {
	return entity.Id("deploy-lock/" + appName)
}

func (l *Locks) acquireLegacy(ctx context.Context, appName, deploymentID string) (*Holder, bool, error) {
	id := legacyLockID(appName)
	for attempt := range lockAcquireLimit {
		mine := l.holderFor(appName, deploymentID)
		res, err := l.eac.Ensure(ctx, encodeLegacyLock(id, mine))
		if err != nil {
			return nil, false, fmt.Errorf("failed to acquire compatibility deploy lock: %w", err)
		}
		if res.Created() {
			mine.Revision = res.Revision()
			return mine, true, nil
		}

		current, err := l.getLegacy(ctx, appName)
		if err != nil {
			if errors.Is(err, cond.ErrNotFound{}) {
				l.backoff(attempt + 1)
				continue
			}
			return nil, false, err
		}
		if current.DeploymentID == deploymentID {
			mine.AcquiredAt = current.AcquiredAt
			refreshed, err := l.replaceLegacy(ctx, id, current.Revision, mine)
			if err == nil {
				return refreshed, false, nil
			}
			if errors.Is(err, cond.ErrConflict{}) {
				l.backoff(attempt + 1)
				continue
			}
			return nil, false, err
		}

		stealable, reason := l.stealable(ctx, current)
		if !stealable {
			return current, false, &LockHeldError{Holder: current}
		}
		if current.DeploymentID != "" {
			l.log.Warn("stealing compatibility deploy lock",
				"app", appName,
				"from_deployment_id", current.DeploymentID,
				"to_deployment_id", deploymentID,
				"reason", reason)
		}

		taken, err := l.replaceLegacy(ctx, id, current.Revision, mine)
		if err != nil {
			if errors.Is(err, cond.ErrConflict{}) {
				l.backoff(attempt + 1)
				continue
			}
			return nil, false, err
		}
		return taken, true, nil
	}

	return nil, false, fmt.Errorf("gave up acquiring compatibility deploy lock for %s after %d attempts",
		appName, lockAcquireLimit)
}

func (l *Locks) releaseLegacy(ctx context.Context, appName, deploymentID string) error {
	id := legacyLockID(appName)
	for attempt := range lockAcquireLimit {
		current, err := l.getLegacy(ctx, appName)
		if err != nil {
			if errors.Is(err, cond.ErrNotFound{}) {
				return nil
			}
			return err
		}
		if current.DeploymentID == "" || current.DeploymentID != deploymentID {
			return nil
		}

		released := &Holder{
			AppName:    appName,
			AcquiredAt: current.AcquiredAt,
			ExpiresAt:  l.now.Now(),
		}
		if _, err := l.replaceLegacy(ctx, id, current.Revision, released); err != nil {
			if errors.Is(err, cond.ErrConflict{}) {
				l.backoff(attempt + 1)
				continue
			}
			return fmt.Errorf("failed to release compatibility deploy lock: %w", err)
		}
		return nil
	}

	return fmt.Errorf("gave up releasing compatibility deploy lock for %s after %d attempts",
		appName, lockAcquireLimit)
}

func (l *Locks) getLegacy(ctx context.Context, appName string) (*Holder, error) {
	res, err := l.eac.Get(ctx, string(legacyLockID(appName)))
	if err != nil {
		return nil, err
	}
	attrs := &entityAttrs{entity: res.Entity()}
	holder := &Holder{AppName: appName, Revision: res.Entity().Revision()}
	if attr, ok := attrs.Get(legacyLockAppName); ok && attr.Value.Kind() == entity.KindString {
		holder.AppName = attr.Value.String()
	}
	if attr, ok := attrs.Get(legacyLockDeploymentID); ok && attr.Value.Kind() == entity.KindString {
		holder.DeploymentID = attr.Value.String()
	}
	if attr, ok := attrs.Get(legacyLockAcquiredAt); ok && attr.Value.Kind() == entity.KindTime {
		holder.AcquiredAt = attr.Value.Time()
	}
	if attr, ok := attrs.Get(legacyLockExpiresAt); ok && attr.Value.Kind() == entity.KindTime {
		holder.ExpiresAt = attr.Value.Time()
	}
	return holder, nil
}

func (l *Locks) replaceLegacy(ctx context.Context, id entity.Id, revision int64, holder *Holder) (*Holder, error) {
	res, err := l.eac.Replace(ctx, encodeLegacyLock(id, holder), revision)
	if err != nil {
		return nil, err
	}
	replaced := *holder
	replaced.Revision = res.Revision()
	return &replaced, nil
}

func encodeLegacyLock(id entity.Id, holder *Holder) []entity.Attr {
	attrs := []entity.Attr{
		entity.Ref(entity.DBId, id),
		entity.Ref(entity.EntityKind, legacyLockKind),
		entity.String(legacyLockAppName, holder.AppName),
		entity.Time(legacyLockAcquiredAt, holder.AcquiredAt),
		entity.Time(legacyLockExpiresAt, holder.ExpiresAt),
	}
	if holder.DeploymentID != "" {
		attrs = append(attrs, entity.String(legacyLockDeploymentID, holder.DeploymentID))
	}
	return attrs
}
