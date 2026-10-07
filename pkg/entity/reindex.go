package entity

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/mr-tron/base58"
	"go.etcd.io/etcd/api/v3/mvccpb"
	clientv3 "go.etcd.io/etcd/client/v3"
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
//
// It rebuilds matches only, each under the entity key's own lease so a bound
// entity's matches still go with its session (MIR-1320). Presence markers are
// left to their sessions' writes: writing one here would attach a session's
// lease from a read that may be stale by the time it lands. See the index
// layout comment above indexWrite.
func (s *EtcdStore) reindexEntity(ctx context.Context, log *slog.Logger, id Id, opts ReindexOptions, stats *ReindexStats) {
	for range reindexEntityAttempts {
		if s.reindexEntityOnce(ctx, log, id, opts, stats) {
			return
		}
	}
	// Counted as a failure rather than skipped: a pass that reports none may
	// be taken as proof every entity is indexed (see ReindexStats).
	log.Warn("reindex: entity kept changing under the pass, leaving it for the next one", "id", id)
	stats.EntitiesFailed++
}

// reindexEntityAttempts bounds how often reindexEntity rereads an entity that
// changed between its read and its write.
const reindexEntityAttempts = 3

// reindexEntityRaceHook is a test seam. When non-nil, reindexEntity invokes it
// after reading the entity but before committing its guarded write, so a
// white-box test can land a concurrent write in that window. It is always nil
// in production.
var reindexEntityRaceHook func(Id)

// reindexEntityOnce makes one attempt at reindexEntity. It reports false when
// the entity changed between the read and the write and should be read again.
func (s *EtcdStore) reindexEntityOnce(ctx context.Context, log *slog.Logger, id Id, opts ReindexOptions, stats *ReindexStats) bool {
	key := s.buildKey(id)
	resp, err := s.client.Get(ctx, key)
	if err != nil {
		log.Warn("reindex: failed to get entity", "id", id, "error", err)
		stats.EntitiesFailed++
		return true
	}
	if len(resp.Kvs) == 0 {
		return true
	}
	kv := resp.Kvs[0]

	var ent Entity
	if err := decoder.Unmarshal(kv.Value, &ent); err != nil {
		log.Warn("reindex: failed to decode entity", "id", id, "error", err)
		stats.EntitiesFailed++
		return true
	}

	// The entity key holds no session attributes, so every indexed value in
	// it is one to index.
	indexed, _ := indexedValues(ctx, s, ent.attrs, true)
	var puts putOps
	for _, attrs := range indexed {
		for _, attr := range attrs {
			puts.add(s.addToCollectionOp(&ent, attr.CAS(), clientv3.LeaseID(kv.Lease)))
		}
	}

	if opts.DryRun || len(puts.ops) == 0 {
		return true
	}

	if reindexEntityRaceHook != nil {
		reindexEntityRaceHook(id)
	}

	// The matches carry the lease read above, so they only stand if the entity
	// key has not moved: a write that binds or unbinds it has indexed the
	// entity itself, and the older lease would undo that. They go in bounded
	// chunks, since a schema change can newly index more values than any one
	// write ever put, and every chunk carries the guard, so chunks that landed
	// before a change are ones that change's own write supersedes.
	for chunk := range slices.Chunk(puts.ops, reindexTxnOps) {
		txn, err := s.client.Txn(ctx).
			If(clientv3.Compare(clientv3.ModRevision(key), "=", kv.ModRevision)).
			Then(chunk...).
			Commit()
		if err != nil {
			log.Warn("reindex: failed to write index entries", "id", id, "error", err)
			stats.EntitiesFailed++
			return true
		}
		if !txn.Succeeded {
			return false
		}
		stats.IndexesRebuilt += int64(len(chunk))
	}
	return true
}

// reindexTxnOps bounds the puts in one reindex transaction, comfortably under
// etcd's default limit of 128 ops.
const reindexTxnOps = 64
