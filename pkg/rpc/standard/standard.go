package standard

import (
	"time"
)

//go:generate go run ../../../pkg/rpc/cmd/rpcgen -pkg standard -input standard.yml -output standard.gen.go

func ToTimestamp(t time.Time) *Timestamp {
	var ts Timestamp
	ts.SetSeconds(t.Unix())
	ts.SetNanoseconds(int32(t.Nanosecond()))

	return &ts
}

func FromTimestamp(ts *Timestamp) time.Time {
	if ts == nil {
		return time.Time{}
	}

	return time.Unix(ts.Seconds(), int64(ts.Nanoseconds()))
}

func ToDuration(d time.Duration) *Duration {
	var dur Duration
	dur.SetNanoseconds(uint64(d.Nanoseconds()))
	return &dur
}

func FromDuration(dur *Duration) time.Duration {
	if dur == nil {
		return 0
	}

	return time.Duration(dur.Nanoseconds()) * time.Nanosecond
}
