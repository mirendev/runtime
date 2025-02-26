package lsvd

import "github.com/oklog/ulid/v2"

func ParseSegment(str string) (SegmentId, error) {
	u, err := ulid.Parse(str)
	if err != nil {
		return SegmentId{}, err
	}

	return SegmentId(u), nil
}

type SegmentId ulid.ULID

func (s SegmentId) String() string {
	return ulid.ULID(s).String()
}

func (s SegmentId) Valid() bool {
	return s != SegmentId{}
}

const SegmentIdSize = 16
