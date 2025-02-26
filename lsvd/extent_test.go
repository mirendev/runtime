package lsvd

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExtent(t *testing.T) {
	e := func(lba LBA, blocks uint32) Extent {
		return Extent{lba, blocks}
	}

	t.Run("extent-from", func(t *testing.T) {
		r := require.New(t)

		_, ok := ExtentFrom(10, 1)
		r.False(ok)

		x, ok := ExtentFrom(1, 10)
		r.True(ok)

		r.Equal(LBA(1), x.LBA)
		r.Equal(uint32(10), x.Blocks)
	})

	t.Run("contains", func(t *testing.T) {
		r := require.New(t)

		r.True(e(1, 10).Contains(1))
		r.True(e(1, 10).Contains(10))

		r.False(e(1, 10).Contains(11))
		r.False(e(1, 10).Contains(0))
	})

	t.Run("covers", func(t *testing.T) {
		r := require.New(t)

		r.Equal(CoverExact, e(1, 1).Cover(e(1, 1)))

		for _, x := range []Extent{e(0, 1), e(1, 2), e(9, 1)} {
			r.Equal(CoverSuperRange, e(0, 10).Cover(x))
		}

		for _, x := range []Extent{e(9, 2), e(15, 20), e(0, 100)} {
			r.Equal(CoverPartly, e(10, 10).Cover(x))
		}

		for _, x := range []Extent{e(0, 10), e(20, 1)} {
			r.Equal(CoverNone, e(10, 10).Cover(x), "%s covers but shouldn't", x)
		}

		x := Extent{7238172, 21}
		y := Extent{7238193, 15}

		r.Equal(CoverNone, x.Cover(y))
		r.Equal(CoverNone, y.Cover(x))

	})

	t.Run("clamp", func(t *testing.T) {
		r := require.New(t)

		chk := func(res, lhs, rhs Extent) {
			act, ok := lhs.Clamp(rhs)
			r.True(ok, "unable to clamp")
			r.Equal(res, act)
		}

		chk(e(2, 4), e(1, 10), e(2, 4))

		chk(e(28, 5), e(1, 32), e(28, 32))

		chk(e(121667583, 1), e(121667583, 2), e(121667583, 1))

		_, ok := Extent{0, 2}.Clamp(Extent{3, 1})
		r.False(ok)

		_, ok = Extent{3, 1}.Clamp(Extent{0, 2})
		r.False(ok)
	})

	t.Run("sub", func(t *testing.T) {
		r := require.New(t)

		chk := func(lhs, rhs Extent, rest ...Extent) {
			act, ok := lhs.Sub(rhs)
			r.True(ok, "unable to sub %s - %s", lhs, rhs)
			r.Equal(rest, act)
		}

		chk(e(1, 10), e(1, 1), e(2, 9))
		chk(e(1, 10), e(2, 1), e(1, 1), e(3, 8))
		chk(e(1, 10), e(9, 2), e(1, 8))
		chk(e(1, 10), e(9, 1), e(1, 8), e(10, 1))

		chk(e(10, 10), e(8, 3), e(11, 9))

		chk(e(1, 1), e(1, 1))
		chk(e(1, 4), e(2, 3), e(1, 1))

		_, ok := Extent{0, 2}.Sub(Extent{3, 1})
		r.False(ok)

		_, ok = Extent{3, 1}.Sub(Extent{0, 2})
		r.False(ok)

		_, ok = Extent{1, 4}.Sub(Extent{1, 7})
		r.False(ok)
	})

	t.Run("sub_many", func(t *testing.T) {
		r := require.New(t)

		res, ok := e(0, 10).SubMany([]Extent{e(1, 1), e(2, 1), e(8, 2)})
		r.True(ok)
		r.Equal([]Extent{e(0, 1), e(3, 5)}, res)

		res, ok = e(0, 10).SubMany([]Extent{e(8, 2), e(2, 1), e(1, 1)})
		r.True(ok)
		r.Equal([]Extent{e(0, 1), e(3, 5)}, res)

		res, ok = e(0, 4).SubMany([]Extent{e(1, 1)})
		r.True(ok)
		r.Equal([]Extent{e(0, 1), e(2, 2)}, res)

		res, ok = e(0, 10).SubMany([]Extent{e(1, 3), e(1, 1), e(8, 2)})
		r.True(ok)
		r.Equal([]Extent{e(0, 1), e(4, 4)}, res)

		_, ok = e(0, 2).SubMany([]Extent{e(3, 1)})
		r.False(ok)
	})

	t.Run("mask", func(t *testing.T) {
		r := require.New(t)

		m := Extent{0, 4}.StartMask()

		r.NoError(m.Cover(Extent{0, 1}))
		r.NoError(m.Cover(Extent{1, 19}))

		holes := m.Holes()
		r.Len(holes, 0)
	})
}
