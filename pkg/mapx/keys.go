package mapx

import (
	"cmp"
	"iter"
	"slices"
)

func Keys[K cmp.Ordered, V any](m map[K]V) []K {
	keys := make([]K, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}

	slices.Sort(keys)
	return keys
}

// Keys returns an iterator over keys in m.
// The iteration order is the result of calling slices.Sort on the keys.
func StableOrder[Map ~map[K]V, K cmp.Ordered, V any](m Map) iter.Seq2[K, V] {
	keys := Keys(m)

	return func(yield func(K, V) bool) {
		for _, k := range keys {
			if !yield(k, m[k]) {
				return
			}
		}
	}
}
