package export

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/entity"
)

const defaultBackfillPageSize int64 = 200

type BackfillStats struct {
	Scanned       int64
	Marked        int64
	AlreadyMarked int64
}

type BackfillOption func(*backfillConfig)

type backfillConfig struct {
	excludedKinds map[entity.Id]struct{}
}

// ExcludingKinds leaves marker ownership for the named kinds to a more
// specialized migration. This is how deployment attempts stay unselectable
// until their canonical migration has validated each individual record.
func ExcludingKinds(kinds ...entity.Id) BackfillOption {
	return func(config *backfillConfig) {
		for _, kind := range kinds {
			config.excludedKinds[kind] = struct{}{}
		}
	}
}

// BackfillMarker makes existing entities selectable through the contract's one
// indexed marker. Generated encoders mark new writes; this pass is the additive
// upgrade path for records created by older runtimes.
func BackfillMarker(
	ctx context.Context,
	log *slog.Logger,
	store entity.Store,
	contract *Contract,
	pageSize int64,
	options ...BackfillOption,
) (BackfillStats, error) {
	if pageSize <= 0 {
		pageSize = defaultBackfillPageSize
	}
	config := backfillConfig{excludedKinds: make(map[entity.Id]struct{})}
	for _, option := range options {
		option(&config)
	}
	var stats BackfillStats
	for _, kind := range contract.Kinds {
		kindID := entity.Id(kind.ID)
		if _, excluded := config.excludedKinds[kindID]; excluded {
			continue
		}
		cursor := ""
		for {
			page, err := store.ListIndexPage(ctx, entity.Ref(entity.EntityKind, kindID), cursor, pageSize)
			if err != nil {
				return stats, fmt.Errorf("list %s for cloud export: %w", kind.ID, err)
			}
			entities, err := store.GetEntities(ctx, page.Ids)
			if err != nil {
				return stats, fmt.Errorf("read %s for cloud export: %w", kind.ID, err)
			}
			for _, source := range entities {
				if source == nil {
					continue
				}
				stats.Scanned++
				if attr, ok := source.Get(contract.MarkerID()); ok &&
					attr.Value.Kind() == entity.KindBool && attr.Value.Bool() {
					stats.AlreadyMarked++
					continue
				}
				marked, err := ensureMarker(ctx, store, source.Id(), contract.MarkerID())
				if err != nil {
					return stats, fmt.Errorf("mark %s for cloud export: %w", source.Id(), err)
				}
				if marked {
					stats.Marked++
				} else {
					stats.AlreadyMarked++
				}
			}
			if page.Cursor == "" {
				break
			}
			cursor = page.Cursor
		}
	}
	log.Info("cloud export marker backfill complete",
		"scanned", stats.Scanned,
		"marked", stats.Marked,
		"already_marked", stats.AlreadyMarked)
	return stats, nil
}

func ensureMarker(ctx context.Context, store entity.Store, id, marker entity.Id) (bool, error) {
	for {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		current, err := store.GetEntity(ctx, id)
		if err != nil {
			if errors.Is(err, cond.ErrNotFound{}) {
				return false, nil
			}
			return false, err
		}
		if attr, ok := current.Get(marker); ok && attr.Value.Kind() == entity.KindBool && attr.Value.Bool() {
			return false, nil
		}
		_, err = store.PatchEntity(ctx, entity.New(
			entity.Ref(entity.DBId, id),
			entity.Bool(marker, true),
		), entity.WithFromRevision(current.GetRevision()))
		if errors.Is(err, cond.ErrConflict{}) {
			continue
		}
		return err == nil, err
	}
}
