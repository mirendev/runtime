package lsvd

type Cover int

const (
	CoverSuperRange Cover = iota
	CoverExact
	CoverPartly
	CoverNone
)

func (c Cover) String() string {
	switch c {
	case CoverSuperRange:
		return "cover-super-range"
	case CoverExact:
		return "cover-exact"
	case CoverPartly:
		return "cover-partly"
	case CoverNone:
		return "cover-none"
	default:
		return "bad-cover"
	}
}
