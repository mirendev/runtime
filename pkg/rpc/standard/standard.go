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
	return time.Unix(ts.Seconds(), int64(ts.Nanoseconds()))
}
