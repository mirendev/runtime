package entity

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestScanPaged(t *testing.T) {
	r := require.New(t)

	client, basePrefix := setupTestEtcd(t)
	ctx := context.Background()

	prefix := basePrefix + "/scan/"

	// Write more keys than a single page so we exercise the paging loop with a
	// tiny page size, plus a key just outside the prefix that must not show up.
	const total = 25
	for i := range total {
		_, err := client.Put(ctx, fmt.Sprintf("%skey-%03d", prefix, i), fmt.Sprintf("value-%03d", i))
		r.NoError(err)
	}
	_, err := client.Put(ctx, basePrefix+"/other/key", "nope")
	r.NoError(err)

	t.Run("pages through every key in order", func(t *testing.T) {
		r := require.New(t)

		kvs, err := scanPaged(ctx, client, prefix, withPageSize(10))
		r.NoError(err)
		r.Len(kvs, total)

		for i, kv := range kvs {
			r.Equal(fmt.Sprintf("%skey-%03d", prefix, i), string(kv.Key))
			r.Equal(fmt.Sprintf("value-%03d", i), string(kv.Value))
		}
	})

	t.Run("keys-only omits values", func(t *testing.T) {
		r := require.New(t)

		kvs, err := scanPaged(ctx, client, prefix, withPageSize(10), withKeysOnly())
		r.NoError(err)
		r.Len(kvs, total)

		for i, kv := range kvs {
			r.Equal(fmt.Sprintf("%skey-%03d", prefix, i), string(kv.Key))
			r.Empty(kv.Value)
		}
	})

	t.Run("start key resumes mid-prefix", func(t *testing.T) {
		r := require.New(t)

		start := fmt.Sprintf("%skey-%03d", prefix, 10)
		kvs, err := scanPaged(ctx, client, prefix, withPageSize(4), withStartKey(start))
		r.NoError(err)
		r.Len(kvs, total-10)
		r.Equal(start, string(kvs[0].Key))
		r.Equal(fmt.Sprintf("%skey-%03d", prefix, total-1), string(kvs[len(kvs)-1].Key))
	})

	t.Run("start key past the prefix returns nothing", func(t *testing.T) {
		r := require.New(t)

		kvs, err := scanPaged(ctx, client, prefix, withStartKey(prefix+"key-999"))
		r.NoError(err)
		r.Empty(kvs)
	})

	t.Run("start key before the prefix scans from the head", func(t *testing.T) {
		r := require.New(t)

		// Clamped rather than honored: an out-of-range cursor must not widen the
		// scan to sibling keyspaces.
		kvs, err := scanPaged(ctx, client, prefix, withPageSize(10), withStartKey(basePrefix))
		r.NoError(err)
		r.Len(kvs, total)
		r.Equal(fmt.Sprintf("%skey-%03d", prefix, 0), string(kvs[0].Key))
	})

	t.Run("empty prefix returns nothing", func(t *testing.T) {
		r := require.New(t)

		kvs, err := scanPaged(ctx, client, basePrefix+"/empty/")
		r.NoError(err)
		r.Empty(kvs)
	})
}
