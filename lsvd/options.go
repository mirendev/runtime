package lsvd

import "github.com/oklog/ulid/v2"

type opts struct {
	sa         SegmentAccess
	volName    string
	autoCreate bool
	seqGen     func() ulid.ULID
	afterNS    func(SegmentId)
	lowers     []*Disk
	ro         bool
	useZstd    bool

	autoGC bool
}

type Option func(o *opts)

func WithSegmentAccess(sa SegmentAccess) Option {
	return func(o *opts) {
		o.sa = sa
	}
}

func WithVolumeName(name string) Option {
	return func(o *opts) {
		o.volName = name
	}
}

func AutoCreate(ok bool) Option {
	return func(o *opts) {
		o.autoCreate = ok
	}
}

func WithSeqGen(f func() ulid.ULID) Option {
	return func(o *opts) {
		o.seqGen = f
	}
}

func AfterNewSegment(f func(SegmentId)) Option {
	return func(o *opts) {
		o.afterNS = f
	}
}

func ReadOnly() Option {
	return func(o *opts) {
		o.ro = true
	}
}

func WithLowerLayer(d *Disk) Option {
	return func(o *opts) {
		o.lowers = append(o.lowers, d)
	}
}

func WithZstd() Option {
	return func(o *opts) {
		o.useZstd = true
	}
}

var EnableAutoGC = func(o *opts) {
	o.autoGC = true
}
