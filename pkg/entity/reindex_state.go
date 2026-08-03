package entity

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
)

// Reindex bookkeeping lives under the store's /meta/ prefix. index-hash records
// the index schema the store is known to be consistent with; reindex-state
// records the progress of a reindex working toward a new one. They are written
// in that order — state cleared only after the hash lands — so a crash between
// the two re-runs a final no-op pass rather than declaring a partial reindex
// finished.
const (
	indexHashSuffix    = "/meta/index-hash"
	reindexStateSuffix = "/meta/reindex-state"
)

// ReindexState is the persisted progress of an in-flight schema reindex, so a
// reindex too large to finish in one pass resumes where it stopped instead of
// restarting from the beginning on every coordinator boot.
type ReindexState struct {
	// TargetHash is the index schema hash this reindex is working toward. If it
	// no longer matches the running schema the work in flight is stale, and the
	// scan must restart from the beginning against the new target.
	TargetHash string `json:"target_hash"`

	// Cursor is the entity key to resume the scan at.
	Cursor string `json:"cursor"`

	// EntitiesProcessed counts entities covered across every pass so far. It is
	// operator visibility only; nothing keys off it.
	EntitiesProcessed int64 `json:"entities_processed"`
}

// LoadIndexHash returns the index schema hash the store is recorded as being
// consistent with. An empty string means no reindex has ever completed.
func (s *EtcdStore) LoadIndexHash(ctx context.Context) (string, error) {
	resp, err := s.client.Get(ctx, s.prefix+indexHashSuffix)
	if err != nil {
		return "", fmt.Errorf("failed to read index hash: %w", err)
	}
	if len(resp.Kvs) == 0 {
		return "", nil
	}
	return string(resp.Kvs[0].Value), nil
}

// SaveIndexHash records that the store is consistent with hash. Only call this
// after a reindex pass that ran to completion: it is the gate that stops the
// work from being retried, so writing it early strands entities un-indexed with
// nothing left to notice.
func (s *EtcdStore) SaveIndexHash(ctx context.Context, hash string) error {
	if _, err := s.client.Put(ctx, s.prefix+indexHashSuffix, hash); err != nil {
		return fmt.Errorf("failed to store index hash: %w", err)
	}
	return nil
}

// LoadReindexState returns the in-flight reindex progress, or nil if there is
// none. Unreadable or corrupt state is reported as absent rather than as an
// error: the safe fallback is a reindex from scratch, which is idempotent.
func (s *EtcdStore) LoadReindexState(ctx context.Context, log *slog.Logger) (*ReindexState, error) {
	resp, err := s.client.Get(ctx, s.prefix+reindexStateSuffix)
	if err != nil {
		return nil, fmt.Errorf("failed to read reindex state: %w", err)
	}
	if len(resp.Kvs) == 0 {
		return nil, nil
	}

	var state ReindexState
	if err := json.Unmarshal(resp.Kvs[0].Value, &state); err != nil {
		log.Warn("reindex: discarding unreadable reindex state, restarting scan", "error", err)
		return nil, nil
	}

	return &state, nil
}

// SaveReindexState persists reindex progress so the next pass resumes from it.
func (s *EtcdStore) SaveReindexState(ctx context.Context, state *ReindexState) error {
	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("failed to marshal reindex state: %w", err)
	}
	if _, err := s.client.Put(ctx, s.prefix+reindexStateSuffix, string(data)); err != nil {
		return fmt.Errorf("failed to store reindex state: %w", err)
	}
	return nil
}

// ClearReindexState removes in-flight reindex progress, marking the work done.
func (s *EtcdStore) ClearReindexState(ctx context.Context) error {
	if _, err := s.client.Delete(ctx, s.prefix+reindexStateSuffix); err != nil {
		return fmt.Errorf("failed to clear reindex state: %w", err)
	}
	return nil
}
