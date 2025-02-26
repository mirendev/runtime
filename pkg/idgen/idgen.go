package idgen

import (
	"crypto/rand"
	"fmt"
	"sync"
	"time"

	"github.com/mr-tron/base58"
)

var (
	timeMu   sync.Mutex
	lasttime uint64 // last time we returned
	clockSeq uint16 // clock sequence for this run

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
