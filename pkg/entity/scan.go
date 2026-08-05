package entity

import (
	"context"

	"go.etcd.io/etcd/api/v3/mvccpb"
	clientv3 "go.etcd.io/etcd/client/v3"
)

// defaultScanPageSize bounds how many keys are fetched per etcd request when
// scanning a keyspace.
const defaultScanPageSize = 500

type scanConfig struct {
	pageSize int64
	keysOnly bool
	startKey string
	revision *int64
}

type scanOption func(*scanConfig)

// withKeysOnly fetches keys without their values. Use it when the caller only
// needs the key set (e.g. listing IDs or checking for collisions), so etcd
// doesn't ship values it won't read.
func withKeysOnly() scanOption {
	return func(c *scanConfig) { c.keysOnly = true }
}

// withRevisionSink reports the store revision the scan opened at. Callers that
// hand a revision back to a watch need it, and the scan already pins to it
// internally, so there is no reason to pay for a second read to learn it.
func withRevisionSink(out *int64) scanOption {
	return func(c *scanConfig) { c.revision = out }
}

// withPageSize overrides the default per-request page size.
func withPageSize(n int64) scanOption {
	return func(c *scanConfig) { c.pageSize = n }
}

// withStartKey begins the scan at key rather than at the head of the prefix,
// letting a caller resume a scan it stopped partway through. Keys strictly
// before it are skipped; the range end is still the prefix, so the scan stays
// bounded to the same keyspace.
//
// Resuming this way starts a *fresh* scan at the current revision rather than
// continuing the original one, since the revision a previous pass pinned to may
// well be compacted away by now. That means the skipped prefix is only as
// accurate as the moment it was scanned, so this is for callers whose skipped
// range is known not to need revisiting. A key at or before the prefix is
// ignored, so a zero value scans from the head.
func withStartKey(key string) scanOption {
	return func(c *scanConfig) { c.startKey = key }
}

// scanPaged reads every key/value under prefix in ascending key order, fetching
// them in bounded pages rather than a single Get(WithPrefix()).
//
// A single unbounded prefix read loads the entire keyspace in one RPC. On a
// large or not-recently-compacted store that request can stall, which is a
// problem when it runs early in coordinator startup. Paging keeps each request
// bounded and predictable regardless of store size, so every full-keyspace read
// should route through here instead of open-coding Get(WithPrefix()).
func scanPaged(ctx context.Context, client *clientv3.Client, prefix string, opts ...scanOption) ([]*mvccpb.KeyValue, error) {
	var kvs []*mvccpb.KeyValue
	err := scanPagedFunc(ctx, client, prefix, func(kv *mvccpb.KeyValue) error {
		kvs = append(kvs, kv)
		return nil
	}, opts...)
	if err != nil {
		return nil, err
	}
	return kvs, nil
}

// scanPagedFunc reads every key/value under prefix in ascending key order,
// invoking fn once per key/value as pages arrive rather than materializing the
// whole keyspace. It has the same paging and revision-pinning semantics as
// scanPaged; prefer it when the caller can process (and discard) entries
// incrementally, so memory stays bounded by a single page regardless of store
// size. If fn returns an error the scan stops and returns it.
func scanPagedFunc(ctx context.Context, client *clientv3.Client, prefix string, fn func(kv *mvccpb.KeyValue) error, opts ...scanOption) error {
	cfg := scanConfig{pageSize: defaultScanPageSize}
	for _, opt := range opts {
		opt(&cfg)
	}

	getOpts := []clientv3.OpOption{
		clientv3.WithRange(clientv3.GetPrefixRangeEnd(prefix)),
		clientv3.WithLimit(cfg.pageSize),
		clientv3.WithSort(clientv3.SortByKey, clientv3.SortAscend),
	}
	if cfg.keysOnly {
		getOpts = append(getOpts, clientv3.WithKeysOnly())
	}

	// A start key at or before the prefix would widen the scan; clamp it so the
	// range stays bounded to the prefix and a zero value means "from the head".
	next := max(prefix, cfg.startKey)
	first := true
	for {
		resp, err := client.Get(ctx, next, getOpts...)
		if err != nil {
			return err
		}

		// Pin every page after the first to the revision the scan opened at, so
		// concurrent writes can't make us miss or duplicate keys partway
		// through. A scan that outruns the compaction window will then fail
		// loudly (ErrCompacted) rather than return a torn result.
		if first {
			getOpts = append(getOpts, clientv3.WithRev(resp.Header.Revision))
			if cfg.revision != nil {
				*cfg.revision = resp.Header.Revision
			}
			first = false
		}

		for _, kv := range resp.Kvs {
			if err := fn(kv); err != nil {
				return err
			}
		}

		// An empty page can't advance the cursor; stop rather than index
		// Kvs[-1]. Unreachable while WithLimit > 0, but keeps the loop safe if
		// that ever changes.
		if !resp.More || len(resp.Kvs) == 0 {
			break
		}

		// Resume strictly after the last key returned in this page.
		next = string(resp.Kvs[len(resp.Kvs)-1].Key) + "\x00"
	}

	return nil
}

// scanPaged reads every key/value under prefix from the store's etcd client in
// bounded pages. See the package-level scanPaged for why.
func (s *EtcdStore) scanPaged(ctx context.Context, prefix string, opts ...scanOption) ([]*mvccpb.KeyValue, error) {
	return scanPaged(ctx, s.client, prefix, opts...)
}
