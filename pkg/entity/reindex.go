package entity

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/mr-tron/base58"
	"go.etcd.io/etcd/api/v3/mvccpb"
	"miren.dev/runtime/pkg/cond"
)

const (
	// reindexBatchSize is how many entities we process between rate-limit
	// pauses, mirroring cleanupDeleteBatchSize on the cleanup path.
	reindexBatchSize = 100
	// reindexProgressInterval is how often a running pass logs progress. A
	// full reindex on a large store walks tens of thousands of entities, so
	// this is coarse enough to stay readable in the coordinator log.
	reindexProgressInterval = 1000
)

// errReindexBudgetExhausted stops the scan once a bounded pass has processed
// MaxEntities. It is a control signal, never surfaced to callers.
var errReindexBudgetExhausted = errors.New("reindex: pass budget exhausted")

// ReindexStats holds statistics about a reindex operation.
type ReindexStats struct {
	EntitiesProcessed        int64
	IndexesRebuilt           int64
	CollectionEntriesScanned int64
	StaleEntriesFound        int64
	StaleEntriesRemoved      int64

	// Complete reports whether the pass reached the end of the entity
	// keyspace. Only a complete pass has observed every entity, so only a
	// complete pass may be used to conclude the store matches the current
	// index schema and stamp the new hash.
	//
	// Complete is about coverage, not success: check EntitiesFailed too. A
	// pass can reach the end of the keyspace while individual entities failed
	// to index, and treating that as consistent would strand them.
	Complete bool

	// EntitiesFailed counts entities the pass could not index, either because
	// the entity could not be read or because a collection write was rejected.
	// Failures are logged and skipped rather than aborting the scan, so this
	// is the only signal that a pass which reached the end of the keyspace did
	// not actually finish the job.
	EntitiesFailed int64

	// NextCursor is the key to resume at, set whenever the pass stopped early
	// (budget exhausted, deadline, or shutdown). It is populated even when
	// Reindex returns an error, so a caller that is interrupted can still
	// persist the progress it made.
	NextCursor string
}

// ReindexOptions controls the behavior of a reindex operation.
type ReindexOptions struct {
	DryRun       bool
	CleanupStale bool

	// StartKey resumes the entity scan at a cursor from an earlier pass's
	// ReindexStats.NextCursor. Zero scans from the beginning.
	StartKey string

	// MaxEntities caps how many entities a single pass processes before it
	// stops and reports a resume cursor, so a large store is reindexed across
	// several bounded passes rather than one unbounded run. Zero means
	// unbounded (the manual `miren debug reindex` big hammer).
	MaxEntities int

	// BatchPause is slept after every reindexBatchSize entities to rate-limit
	// write pressure. Zero disables pacing.
	BatchPause time.Duration
}

// Reindex rebuilds index (collection) entries for entities in the store,
// streaming the entity keyspace in bounded pages rather than materializing
// every ID up front.
//
// A pass may be bounded with opts.MaxEntities and resumed from a later pass via
// stats.NextCursor, so a store too large to reindex inside one deadline still
// converges across several passes. Resuming forward-only is safe because the
// process running the reindex already has the current index schema loaded:
// anything written while it runs is indexed correctly on the write path, so
// only pre-existing entities need backfill and their keys don't move. The
// coordinator is a singleton, so there is no concurrent writer running an older
// schema that could land un-indexed entities behind the cursor.
//
// If opts.CleanupStale is true, a pass that runs to completion also scans for
// and removes stale collection entries that point to non-existent entities.
func (s *EtcdStore) Reindex(ctx context.Context, log *slog.Logger, opts ReindexOptions) (*ReindexStats, error) {
	s.ClearSchemaCache()

	stats := &ReindexStats{}

	// Phase 1: stream the entity keyspace and rebuild indexes.
	entityPrefix := fmt.Sprintf("%s/entity/", s.prefix)

	var scanOpts []scanOption
	if opts.StartKey != "" {
		scanOpts = append(scanOpts, withStartKey(opts.StartKey))
		log.Info("reindex: resuming entity scan", "cursor", opts.StartKey)
	} else {
		log.Info("reindex: starting entity scan")
	}

	scanErr := scanPagedFunc(ctx, s.client, entityPrefix, func(kv *mvccpb.KeyValue) error {
		if err := ctx.Err(); err != nil {
			return err
		}

		key := string(kv.Key)
		id, ok := entityIDFromKey(log, entityPrefix, key)
		if !ok {
			return nil
		}

		s.reindexEntity(ctx, log, id, opts, stats)

		// Advance strictly past this key only after the entity is done. Index
		// writes are idempotent, so a cursor that lags by one entity costs at
		// most a repeat on resume, never a gap.
		stats.NextCursor = key + "\x00"
		stats.EntitiesProcessed++

		if stats.EntitiesProcessed%reindexProgressInterval == 0 {
			log.Info("reindex: progress",
				"processed", stats.EntitiesProcessed,
				"indexes_rebuilt", stats.IndexesRebuilt)
		}

		if opts.MaxEntities > 0 && stats.EntitiesProcessed >= int64(opts.MaxEntities) {
			return errReindexBudgetExhausted
		}

		if opts.BatchPause > 0 && stats.EntitiesProcessed%reindexBatchSize == 0 {
			select {
			case <-time.After(opts.BatchPause):
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		return nil
	}, scanOpts...)

	switch {
	case scanErr == nil:
		stats.Complete = true
		stats.NextCursor = ""
	case errors.Is(scanErr, errReindexBudgetExhausted):
		log.Info("reindex: pass budget reached, will resume",
			"processed", stats.EntitiesProcessed,
			"cursor", stats.NextCursor)
	default:
		// Return the stats alongside the error: the cursor recorded so far is
		// still valid progress, and an interrupted caller should persist it.
		return stats, fmt.Errorf("failed to scan entities: %w", scanErr)
	}

	// Phase 2: Clean up stale index entries (optional). Delegates to the shared,
	// CAS-guarded cleanup used by the background sweeper so both paths remove
	// orphans the same safe way. Manual reindex is the "big hammer": unbounded
	// deletes, no pacing. It is gated on a complete pass because a partial one
	// hasn't seen every entity and the cleanup's own scan is full-keyspace
	// regardless, so running it per-pass would be pure waste.
	if opts.CleanupStale && stats.Complete {
		log.Info("reindex: cleaning up stale index entries")
		cleanup, err := s.CleanupStaleCollectionEntries(ctx, log, CleanupOptions{DryRun: opts.DryRun})
		if err != nil {
			log.Warn("reindex: stale cleanup failed", "error", err)
		}
		if cleanup != nil {
			stats.CollectionEntriesScanned = cleanup.CollectionEntriesScanned
			stats.StaleEntriesFound = cleanup.StaleEntriesFound
			stats.StaleEntriesRemoved = cleanup.StaleEntriesRemoved
		}
	}

	log.Info("reindex: pass finished",
		"complete", stats.Complete,
		"entities_processed", stats.EntitiesProcessed,
		"entities_failed", stats.EntitiesFailed,
		"indexes_rebuilt", stats.IndexesRebuilt,
		"collection_entries_scanned", stats.CollectionEntriesScanned,
		"stale_entries_found", stats.StaleEntriesFound,
		"stale_entries_removed", stats.StaleEntriesRemoved)

	return stats, nil
}

// entityIDFromKey decodes an entity ID from its store key, reporting false for
// keys that aren't entities.
func entityIDFromKey(log *slog.Logger, entityPrefix, key string) (Id, bool) {
	suffix := strings.TrimPrefix(key, entityPrefix)
	if suffix == "" {
		return "", false
	}

	// Session attributes live under the owning entity's key
	// (`.../entity/<id>/session/<session>`) and carry nothing to index. Test the
	// suffix rather than the whole key: a store prefix that happened to contain
	// "/session/" would otherwise skip every entity in the store, and the pass
	// would report a clean reindex having indexed nothing.
	if strings.Contains(suffix, "/session/") {
		return "", false
	}

	decoded, err := base58.Decode(suffix)
	if err != nil {
		log.Warn("reindex: failed to decode entity ID", "key", suffix, "error", err)
		return "", false
	}

	return Id(decoded), true
}

// reindexEntity rewrites the collection entries for a single entity. Failures
// are logged rather than aborting the pass, since one unreadable entity must
// not stop a scan that is otherwise making progress, but they are counted in
// stats.EntitiesFailed so the caller can tell a genuinely complete pass from
// one that merely reached the end of the keyspace. An entity that has since
// been deleted is not a failure; there is simply nothing left to index.
func (s *EtcdStore) reindexEntity(ctx context.Context, log *slog.Logger, id Id, opts ReindexOptions, stats *ReindexStats) {
	ent, err := s.GetEntity(ctx, id)
	if err != nil {
		if errors.Is(err, cond.ErrNotFound{}) {
			return
		}
		log.Warn("reindex: failed to get entity", "id", id, "error", err)
		stats.EntitiesFailed++
		return
	}

	if opts.DryRun {
		return
	}

	indexedAttrs := collectIndexedAttributesTolerant(ctx, s, ent.Attrs())
	for _, attrs := range indexedAttrs {
		for _, attr := range attrs {
			if err := s.addToCollectionDirect(ctx, ent, attr.CAS()); err != nil {
				log.Warn("reindex: failed to add to collection", "id", id, "attr", attr.ID, "error", err)
				stats.EntitiesFailed++
				continue
			}
			stats.IndexesRebuilt++
		}
	}
}

// collectIndexedAttributesTolerant is like EtcdStore.collectIndexedAttributes but
// skips attributes whose schema cannot be looked up, rather than returning an error.
// This is appropriate for reindex where some attribute schemas may be missing.
func collectIndexedAttributesTolerant(ctx context.Context, store Store, attrs []Attr) map[Id][]Attr {
	indexedAttrs := make(map[Id][]Attr)
	allAttrs := enumerateAllAttrs(attrs)
	for _, attr := range allAttrs {
		schema, err := store.GetAttributeSchema(ctx, attr.ID)
		if err != nil {
			continue
		}
		if schema.Index {
			indexedAttrs[attr.ID] = append(indexedAttrs[attr.ID], attr)
		}
	}
	return indexedAttrs
}

var colReplacer = strings.NewReplacer("/", "_", ":", "_")

// addToCollectionDirect writes a single collection entry for the given entity and collection key.
func (s *EtcdStore) addToCollectionDirect(ctx context.Context, ent *Entity, collection string) error {
	key := base58.Encode([]byte(ent.Id()))
	colKey := colReplacer.Replace(collection)

	key = fmt.Sprintf("%s/collections/%s/%s", s.prefix, colKey, key)

	_, err := s.client.Put(ctx, key, ent.Id().String())
	return err
}
