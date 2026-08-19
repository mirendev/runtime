package idgen

import (
	"crypto/rand"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/mr-tron/base58"
)

var (
	timeMu  sync.Mutex
	timeNow = time.Now // for testing
)

// lastV7time is the last time we returned stored as:
//
//	52 bits of time in milliseconds since epoch
//	12 bits of (fractional nanoseconds) >> 8
var lastV7time int64

const nanoPerMilli = 1000000

// getV7Time returns the time in milliseconds and nanoseconds / 256.
// The returned (milli << 12 + seq) is guaranteed to be greater than
// (milli << 12 + seq) returned by any previous call to getV7Time.
func getV7Time() (milli, seq int64) {
	timeMu.Lock()
	defer timeMu.Unlock()

	nano := timeNow().UnixNano()
	milli = nano / nanoPerMilli
	// Sequence number is between 0 and 3906 (nanoPerMilli>>8)
	seq = (nano - milli*nanoPerMilli) >> 8
	now := milli<<12 + seq
	if now <= lastV7time {
		now = lastV7time + 1
		milli = now >> 12
		seq = now & 0xfff
	}
	lastV7time = now
	return milli, seq
}

func Gen(prefix string) string {
	var uuid [16]byte

	// Generate 10 random bytes
	if _, err := rand.Read(uuid[:]); err != nil {
		panic(fmt.Sprintf("failed to read random bytes: %v", err))
	}

	t, s := getV7Time()

	uuid[0] = byte(t >> 40)
	uuid[1] = byte(t >> 32)
	uuid[2] = byte(t >> 24)
	uuid[3] = byte(t >> 16)
	uuid[4] = byte(t >> 8)
	uuid[5] = byte(t)

	uuid[6] = 0x70 | (0x0F & byte(s>>8))

	uuid[7] = byte(s)
	uuid[8] = (uuid[8] & 0x3f) | 0x80 // Variant is 10

	return prefix + base58.Encode(uuid[:])
}

func GenNS(ns string) string {
	return Gen(ns + "-")
}

// TimeOf recovers the moment an id was generated, and reports whether it could.
//
// Gen lays a UUIDv7 down under the base58, so the first six bytes are the
// millisecond timestamp it was minted at. That makes creation time an immutable
// property of the id itself rather than a field alongside it, recoverable for
// records whose own timestamp field was never populated.
//
// This lives here rather than at the call site because this package owns the id
// format. A reader that hand-decoded base58 and reached into byte offsets would
// be a second, silent copy of Gen's layout, and the two would drift the first
// time the format changed.
//
// It reports false rather than a zero time for anything it cannot read, so a
// caller has to decide what an unreadable id means instead of receiving the
// epoch and mistaking it for an answer.
func TimeOf(id string) (time.Time, bool) {
	// Ids are prefixed by kind (see GenNS), and the prefix is not part of the
	// encoding. Everything through the last separator is dropped rather than
	// the first, since a kind may contain one.
	if idx := strings.LastIndex(id, "-"); idx >= 0 {
		id = id[idx+1:]
	}

	// Exactly the UUID width, not merely enough bytes to read a timestamp from.
	// base58 drops leading zero bytes, so a truncated or hand-made id can decode
	// to something shorter whose first six bytes are not the timestamp at all,
	// and reading them anyway would produce a confident wrong answer rather than
	// a refusal.
	raw, err := base58.Decode(id)
	if err != nil || len(raw) != 16 {
		return time.Time{}, false
	}

	milli := int64(raw[0])<<40 |
		int64(raw[1])<<32 |
		int64(raw[2])<<24 |
		int64(raw[3])<<16 |
		int64(raw[4])<<8 |
		int64(raw[5])

	if milli <= 0 {
		return time.Time{}, false
	}

	return time.UnixMilli(milli).UTC(), true
}

// GenAdminToken generates a cryptographically secure admin token.
// Uses 32 bytes of randomness encoded as base58 for URL-safe, compact representation.
func GenAdminToken() string {
	var token [32]byte
	if _, err := rand.Read(token[:]); err != nil {
		panic(fmt.Sprintf("failed to generate admin token: %v", err))
	}
	return base58.Encode(token[:])
}
