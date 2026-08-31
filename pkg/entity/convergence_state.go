package entity

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
)

const (
	convergenceHashSuffix  = "/meta/schema-convergence-hash"
	convergenceStateSuffix = "/meta/schema-convergence-state"
)

// ConvergenceState is the persisted progress of an in-flight schema
// convergence scan.
type ConvergenceState struct {
	TargetHash              string `json:"target_hash"`
	Cursor                  string `json:"cursor"`
	EntitiesProcessed       int64  `json:"entities_processed"`
	EntitiesRewritten       int64  `json:"entities_rewritten"`
	ValuesRewritten         int64  `json:"values_rewritten"`
	ConsecutiveFailedPasses int    `json:"consecutive_failed_passes,omitempty"`
}

func (s *EtcdStore) LoadConvergenceHash(ctx context.Context) (string, error) {
	resp, err := s.client.Get(ctx, s.prefix+convergenceHashSuffix)
	if err != nil {
		return "", fmt.Errorf("failed to read schema convergence hash: %w", err)
	}
	if len(resp.Kvs) == 0 {
		return "", nil
	}
	return string(resp.Kvs[0].Value), nil
}

func (s *EtcdStore) SaveConvergenceHash(ctx context.Context, hash string) error {
	if _, err := s.client.Put(ctx, s.prefix+convergenceHashSuffix, hash); err != nil {
		return fmt.Errorf("failed to store schema convergence hash: %w", err)
	}
	return nil
}

func (s *EtcdStore) LoadConvergenceState(ctx context.Context, log *slog.Logger) (*ConvergenceState, error) {
	resp, err := s.client.Get(ctx, s.prefix+convergenceStateSuffix)
	if err != nil {
		return nil, fmt.Errorf("failed to read schema convergence state: %w", err)
	}
	if len(resp.Kvs) == 0 {
		return nil, nil
	}

	var state ConvergenceState
	if err := json.Unmarshal(resp.Kvs[0].Value, &state); err != nil {
		log.Warn("schema convergence: discarding unreadable state and restarting", "error", err)
		return nil, nil
	}
	return &state, nil
}

func (s *EtcdStore) SaveConvergenceState(ctx context.Context, state *ConvergenceState) error {
	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("failed to marshal schema convergence state: %w", err)
	}
	if _, err := s.client.Put(ctx, s.prefix+convergenceStateSuffix, string(data)); err != nil {
		return fmt.Errorf("failed to store schema convergence state: %w", err)
	}
	return nil
}

func (s *EtcdStore) ClearConvergenceState(ctx context.Context) error {
	if _, err := s.client.Delete(ctx, s.prefix+convergenceStateSuffix); err != nil {
		return fmt.Errorf("failed to clear schema convergence state: %w", err)
	}
	return nil
}
