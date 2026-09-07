package entity

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"go.etcd.io/etcd/api/v3/mvccpb"
	clientv3 "go.etcd.io/etcd/client/v3"
)

// cleanupDeleteBatchSize is how many stale entries we delete between rate-limit
// pauses. Deletes are issued one CAS'd transaction at a time (see
// CleanupStaleCollectionEntries); this only governs how often we sleep for
// BatchPause, not how many keys share a transaction.
const cleanupDeleteBatchSize = 100

// cleanupResolveBatchSize is how many scanned collection entries we buffer
// before resolving their entities in one batched read. It bounds peak memory to
// one scan page plus one resolve batch, and stays modest because every resolved
// entity arrives with its full payload and some kinds carry sizeable blobs.
const cleanupResolveBatchSize = 200

// CleanupOptions bounds a single stale-cleanup pass so it can run continuously
// in the background without overwhelming the store.
type CleanupOptions struct {
	// DryRun scans and counts stale entries but deletes nothing.
	DryRun bool
	// MaxDeletes caps how many stale entries a single pass removes. A large
	// legacy backlog then drains over several passes rather than one thundering
	// sweep. Zero means unbounded (the manual reindex "big hammer").
	MaxDeletes int
	// BatchPause is slept after every cleanupDeleteBatchSize deletes to
	// rate-limit write pressure. Zero disables pacing.
	BatchPause time.Duration
}

// CleanupStats reports what a stale-cleanup pass observed and did. RemovedByCollection
// breaks removals down by index/collection so a persistent, post-drain leak shows
// up as a specific collection that keeps re-accumulating orphans rather than
// hiding in an aggregate count.
type CleanupStats struct {
	CollectionEntriesScanned int64
	StaleEntriesFound        int64
	StaleEntriesRemoved      int64
	// OrphanedEntriesFound counts stale entries whose entity is gone entirely;
	// MismatchedEntriesFound those whose entity exists but no longer carries the
	// value the entry indexes. The two have different sources, so the split says
	// which leak is live when one keeps re-accumulating.
	OrphanedEntriesFound   int64
	MismatchedEntriesFound int64
	// CASConflicts counts entries that changed between scan and delete (the slot
	// was legitimately recreated/re-leased), so the CAS guard skipped them.
	CASConflicts        int64
	RemovedByCollection map[string]int64
}

// CleanupStaleCollectionEntries scans collection (index) entries and removes
// every entry the backing entity does not justify. An entry is justified when
// the entity still exists and still carries an indexed value hashing to the
// entry's collection segment; anything else is drift. An orphan is the
// degenerate case, an entity with no justified values at all.
//
// Safety rests on three things. Creates are atomic, so an entry whose entity is
// absent is genuinely orphaned with no create-in-flight race. Entities are
// resolved with a batched, linearizable read taken after the entry was scanned,
// so the verdict sees state at least as new as the entry. And each delete is
// CAS'd on the entry's mod-revision from scan time, which is what extends the
// ABA guard to the mismatch verdict: any writer that re-justifies an entry has
// to rewrite the key, so the compare fails and the entry survives.
//
// Session-scoped entries and entities whose attributes cannot be resolved are
// left alone.
//
// The pass is idempotent and bounded, so it is safe to run repeatedly as a
// background sweep.
func (s *EtcdStore) CleanupStaleCollectionEntries(ctx context.Context, log *slog.Logger, opts CleanupOptions) (*CleanupStats, error) {
	pass := &cleanupPass{
		store:            s,
		log:              log,
		opts:             opts,
		stats:            &CleanupStats{RemovedByCollection: map[string]int64{}},
		collectionPrefix: s.prefix + "/collections/",
	}

	scanErr := scanPagedFunc(ctx, s.client, pass.collectionPrefix, func(kv *mvccpb.KeyValue) error {
		return pass.scan(ctx, kv)
	})

	switch {
	case scanErr != nil && !errors.Is(scanErr, errStopScan):
		// Persist whatever we already confirmed before surfacing the error;
		// partial progress is the whole point of streaming the sweep.
		if flushErr := pass.flush(ctx); flushErr != nil {
			log.Warn("cleanup: failed to flush after scan error", "flush_error", flushErr)
		}
		return pass.stats, fmt.Errorf("cleanup: scan failed: %w", scanErr)
	case scanErr == nil:
		// Scan ended on its own, so judge the trailing partial page.
		if err := pass.resolve(ctx); err != nil && !errors.Is(err, errStopScan) {
			if flushErr := pass.flush(ctx); flushErr != nil {
				log.Warn("cleanup: failed to flush after resolve error", "flush_error", flushErr)
			}
			return pass.stats, err
		}
	}

	if err := pass.flush(ctx); err != nil {
		return pass.stats, err
	}

	return pass.stats, nil
}

// cleanupPass holds the mutable state of one sweep. It exists so the scan,
// verdict, and delete steps can be separate methods over named fields rather
// than closures over a dozen captured variables.
type cleanupPass struct {
	store            *EtcdStore
	log              *slog.Logger
	opts             CleanupOptions
	stats            *CleanupStats
	collectionPrefix string

	// page buffers scanned entries until there are enough to resolve in one
	// read; pending buffers confirmed-stale entries until there are enough to
	// delete in one batch. Both stream rather than materialize, so a pass cut
	// short by a deadline or shutdown keeps everything it already deleted.
	page    []collectionEntry
	pending []staleCollectionEntry
}

// scan buffers one scanned entry, resolving the page once it fills.
func (p *cleanupPass) scan(ctx context.Context, kv *mvccpb.KeyValue) error {
	p.stats.CollectionEntriesScanned++

	key := string(kv.Key)
	p.page = append(p.page, collectionEntry{
		key:           key,
		modRev:        kv.ModRevision,
		entityID:      Id(kv.Value),
		sessionScoped: sessionScopedKey(key, p.collectionPrefix),
	})

	if len(p.page) >= cleanupResolveBatchSize {
		return p.resolve(ctx)
	}
	return nil
}

// resolve judges the buffered page, reading the entities it refers to once.
func (p *cleanupPass) resolve(ctx context.Context) error {
	batch := p.page
	p.page = nil
	if len(batch) == 0 {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	justified, unverifiable, err := p.store.resolveJustified(ctx, p.log, batch)
	if err != nil {
		return err
	}

	for _, e := range batch {
		if unverifiable[e.entityID] {
			continue
		}
		if err := p.judge(ctx, e, justified); err != nil {
			return err
		}
	}
	return nil
}

// judge classifies one entry against its entity's justified values and, when it
// is stale, queues the delete and enforces the pass's delete budget. It returns
// errStopScan once the budget is spent.
func (p *cleanupPass) judge(ctx context.Context, e collectionEntry, justified map[Id]map[string]bool) error {
	collection := collectionFromKey(e.key, p.collectionPrefix)
	values, present := justified[e.entityID]

	switch {
	case present && values[collection]:
		return nil
	case present && e.sessionScoped:
		// Leased, so etcd collects it with the session. This only covers a live
		// entity; a session-scoped entry whose entity is gone falls to the
		// orphan case and is deleted like any other.
		return nil
	case present:
		p.stats.MismatchedEntriesFound++
	default:
		p.stats.OrphanedEntriesFound++
	}

	p.stats.StaleEntriesFound++
	if p.opts.DryRun {
		return nil
	}

	p.pending = append(p.pending, staleCollectionEntry{
		key:        e.key,
		modRev:     e.modRev,
		collection: collection,
	})

	atBudget := p.opts.MaxDeletes > 0 && int(p.stats.StaleEntriesRemoved)+len(p.pending) >= p.opts.MaxDeletes
	if len(p.pending) >= cleanupDeleteBatchSize || atBudget {
		if err := p.flush(ctx); err != nil {
			return err
		}
	}
	if p.opts.MaxDeletes > 0 && p.stats.StaleEntriesRemoved >= int64(p.opts.MaxDeletes) {
		return errStopScan
	}
	return nil
}

// flush deletes the queued entries, then paces the pass.
func (p *cleanupPass) flush(ctx context.Context) error {
	if len(p.pending) == 0 {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	p.store.deleteStaleBatch(ctx, p.log, p.pending, p.stats)
	p.pending = p.pending[:0]

	if p.opts.BatchPause > 0 {
		select {
		case <-time.After(p.opts.BatchPause):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

// collectionEntry carries everything the verdict needs, so judging never
// re-parses the key.
type collectionEntry struct {
	key           string
	modRev        int64
	entityID      Id
	sessionScoped bool
}

// distinctEntityIDs returns the entities a page refers to, once each: an entity
// commonly owns several entries in a page, one per indexed value.
func distinctEntityIDs(entries []collectionEntry) []Id {
	seen := make(map[Id]bool, len(entries))
	ids := make([]Id, 0, len(entries))
	for _, e := range entries {
		if !seen[e.entityID] {
			seen[e.entityID] = true
			ids = append(ids, e.entityID)
		}
	}
	return ids
}

// resolveJustified reads the entities a page refers to and returns, per entity,
// the collection segments its current indexed attributes justify.
//
// An absent entity is absent from justified, so its entries read as orphans. An
// entity that is present but unreadable, either because its payload will not
// decode or because its attribute schema will not resolve, lands in
// unverifiable and is left alone entirely: it is not absent, and guessing there
// deletes live entries.
func (s *EtcdStore) resolveJustified(
	ctx context.Context,
	log *slog.Logger,
	batch []collectionEntry,
) (justified map[Id]map[string]bool, unverifiable map[Id]bool, err error) {
	ids := distinctEntityIDs(batch)
	entities, undecodable, err := s.getEntities(ctx, ids, false)
	if err != nil {
		return nil, nil, fmt.Errorf("cleanup: failed to resolve entities: %w", err)
	}

	justified = make(map[Id]map[string]bool, len(ids))
	unverifiable = undecodable
	for i, id := range ids {
		ent := entities[i]
		if ent == nil {
			continue
		}
		values, err := s.justifiedCollections(ctx, ent)
		if err != nil {
			log.Warn("cleanup: could not resolve indexed attributes, leaving entries alone",
				"entity_id", id, "error", err)
			unverifiable[id] = true
			continue
		}
		justified[id] = values
	}
	return justified, unverifiable, nil
}

// justifiedCollections returns the collection segments the entity's current
// indexed attributes entitle it to, hashed the way the write path hashes them.
//
// Strict on purpose: it fails if any attribute's schema cannot be read, and the
// caller then leaves that entity's entries alone. Reindex's tolerant variant
// would drop the unreadable attribute, which here reads as "not justified" and
// deletes a live entry.
func (s *EtcdStore) justifiedCollections(ctx context.Context, ent *Entity) (map[string]bool, error) {
	indexed, err := s.collectIndexedAttributes(ctx, ent.attrs)
	if err != nil {
		return nil, err
	}

	values := make(map[string]bool, len(indexed))
	for _, attrs := range indexed {
		for _, attr := range attrs {
			values[tr.Replace(attr.CAS())] = true
		}
	}
	return values, nil
}

// sessionScopedKey reports whether a collection entry is the session-scoped
// variant. Plain entries are "{prefix}/collections/{colKey}/{base58 id}";
// session entries carry a further "/{session}" segment and are leased.
func sessionScopedKey(key, collectionPrefix string) bool {
	return strings.Count(strings.TrimPrefix(key, collectionPrefix), "/") > 1
}

// errStopScan is a sentinel returned by the scan callback to halt scanning once
// the per-pass delete budget is reached, without treating it as a real error.
var errStopScan = errors.New("cleanup: delete budget reached")

// staleCollectionEntry is a collection entry confirmed to be unjustified by its
// entity, along with the mod-revision it carried at scan time (the CAS guard).
type staleCollectionEntry struct {
	key        string
	modRev     int64
	collection string
}

// deleteStaleBatch removes a batch of confirmed-stale entries, CAS-guarded on the
// mod-revision each carried at scan time. It first tries one transaction guarded
// on every entry's mod-revision; if that succeeds (the common case for a static
// legacy backlog) the whole batch is removed in a single round trip. If any
// entry changed since the scan the batched compare fails, so it falls back to a
// per-entry CAS to remove the still-unchanged ones and count the conflicts. It
// records removals, CAS conflicts, and the per-collection breakdown into stats;
// per-entry failures are logged, never returned.
func (s *EtcdStore) deleteStaleBatch(ctx context.Context, log *slog.Logger, batch []staleCollectionEntry, stats *CleanupStats) {
	if len(batch) == 0 {
		return
	}

	cmps := make([]clientv3.Cmp, len(batch))
	ops := make([]clientv3.Op, len(batch))
	for i, e := range batch {
		cmps[i] = clientv3.Compare(clientv3.ModRevision(e.key), "=", e.modRev)
		ops[i] = clientv3.OpDelete(e.key)
	}

	resp, err := s.client.Txn(ctx).If(cmps...).Then(ops...).Commit()
	if err == nil && resp.Succeeded {
		for _, e := range batch {
			stats.StaleEntriesRemoved++
			stats.RemovedByCollection[e.collection]++
		}
		return
	}
	if err != nil {
		log.Warn("cleanup: batched delete failed, retrying per entry", "count", len(batch), "error", err)
	}

	// At least one slot changed (or the batch errored): CAS each entry on its own
	// so an ABA recreate is skipped rather than dragging down the rest of the batch.
	for _, e := range batch {
		r, err := s.client.Txn(ctx).
			If(clientv3.Compare(clientv3.ModRevision(e.key), "=", e.modRev)).
			Then(clientv3.OpDelete(e.key)).
			Commit()
		if err != nil {
			log.Warn("cleanup: failed to delete stale entry", "key", e.key, "error", err)
			continue
		}
		if !r.Succeeded {
			stats.CASConflicts++
			continue
		}
		stats.StaleEntriesRemoved++
		stats.RemovedByCollection[e.collection]++
	}
}

// collectionFromKey extracts the collection segment from a collection entry key.
// Keys are "{prefix}/collections/{colKey}/{base58(id)}" and colKey has its own
// slashes replaced (see addToCollectionDirect), so the collection is everything
// up to the first slash after the prefix.
func collectionFromKey(key, collectionPrefix string) string {
	rest := strings.TrimPrefix(key, collectionPrefix)
	if collection, _, found := strings.Cut(rest, "/"); found {
		return collection
	}
	return rest
}
