package query

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"strings"
	"time"
)

// Effective UTC dates of positive leap seconds, expressed as NTP seconds.
// IERS leap-seconds.list: TAI-UTC starts at 10 seconds in 1972 and reaches
// 37 on 2017-01-01. Update this table when another leap second is announced.
var taiLeapTransitions = [...]int64{
	2287785600, 2303683200, 2335219200, 2366755200, 2398291200,
	2429913600, 2461449600, 2492985600, 2524521600, 2571782400,
	2603318400, 2634854400, 2698012800, 2776982400, 2840140800,
	2871676800, 2918937600, 2950473600, 2982009600, 3029443200,
	3076704000, 3124137600, 3345062400, 3439756800, 3550089600,
	3644697600, 3692217600,
}

// FormatTAI64N formats a UTC time as a resumable event timestamp.
func FormatTAI64N(t time.Time) string {
	offset := int64(10)
	for _, transition := range taiLeapTransitions {
		if t.Unix() < transition-2208988800 {
			break
		}
		offset++
	}
	var raw [12]byte
	binary.BigEndian.PutUint64(raw[:8], uint64(t.Unix()+offset)+(1<<62))
	binary.BigEndian.PutUint32(raw[8:], uint32(t.Nanosecond()))
	return "@" + hex.EncodeToString(raw[:])
}

// ValidateTAI64N validates a timestamp cursor.
func ValidateTAI64N(text string) error {
	if len(text) != 25 || text[0] != '@' || text != strings.ToLower(text) {
		return errors.New("TAI64N timestamp must be @ followed by 24 lowercase hex digits")
	}
	raw, err := hex.DecodeString(text[1:])
	if err != nil || binary.BigEndian.Uint64(raw[:8]) >= 1<<63 || binary.BigEndian.Uint32(raw[8:]) >= 1000000000 {
		return errors.New("invalid TAI64N timestamp")
	}
	return nil
}

// StampEvent assigns a strictly increasing cursor for a stream. A tie or
// backward adjustment advances the previous timestamp by one nanosecond.
func StampEvent(event *Event, last *time.Time) {
	now := time.Now().UTC()
	if !now.After(*last) {
		now = last.Add(time.Nanosecond)
	}
	*last = now
	event.TAI64N = FormatTAI64N(now)
}
