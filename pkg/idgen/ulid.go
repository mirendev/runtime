package idgen

import (
	"crypto/rand"
	"sync"

	"github.com/oklog/ulid/v2"
)

// ulidEntropy makes same-millisecond ULIDs strictly increasing. Plain random
// entropy only orders them to the millisecond. ulidMu covers both the
// timestamp read and the mint: if two callers could read the clock, then
// mint in the other order, the entropy would rewind to the earlier
// millisecond and the next ID minted in the later one would start from
// fresh random bytes, unordered against the one already issued there.
// ulidFloor is the millisecond of the last ID, so a wall clock that steps
// backwards cannot cause the same rewind.
var (
	ulidMu      sync.Mutex
	ulidEntropy = ulid.Monotonic(rand.Reader, 0)
	ulidFloor   uint64
)

// ULID mints a 26-character Crockford base32 ULID. Unlike Gen's base58
// output, ULIDs sort by creation time as plain strings, so they suit names
// that a directory listing or a key range has to return in order. Within a
// process the order is strict; across restarts it is millisecond-granular.
func ULID() string {
	ulidMu.Lock()
	defer ulidMu.Unlock()
	return ulidAt(ulid.Timestamp(timeNow()))
}

// ulidAt is ULID at a caller-chosen millisecond; the caller holds ulidMu.
func ulidAt(ms uint64) string {
	if ms < ulidFloor {
		ms = ulidFloor
	}
	for {
		id, err := ulid.New(ms, ulidEntropy)
		if err == nil {
			ulidFloor = ms
			return id.String()
		}
		// Only ErrMonotonicOverflow, which takes ~2^48 IDs in one
		// millisecond. Minting in the next millisecond keeps the order,
		// and the floor keeps later callers there until the clock catches
		// up.
		ms++
	}
}

// IsULID reports whether s is a ULID as ULID would mint one.
func IsULID(s string) bool {
	_, err := ulid.ParseStrict(s)
	return err == nil
}
