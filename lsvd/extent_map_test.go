package lsvd

import (
	"log/slog"
	"testing"

	"github.com/lab47/mode"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"
)

func TestExtentMap(t *testing.T) {
	t.Logf("build mode: %s", mode.Mode())

	log := slog.Default()

	s1 := SegmentId(ulid.MustNew(ulid.Now(), ulid.DefaultEntropy()))

	t.Run("disjoint updates prefix", func(t *testing.T) {
		r := require.New(t)

		d := NewExtentMap()

		x := Extent{47, 10}
		a, err := d.Update(log, ExtentLocation{
			ExtentHeader: ExtentHeader{
				Extent: x,
				Offset: 47},
			Segment: s1,
		}, nil)
		r.NoError(err)
		r.Len(a, 0)

		y := Extent{0, 8}
		a, err = d.Update(log, ExtentLocation{
			ExtentHeader: ExtentHeader{Offset: 0, Extent: y},
			Segment:      s1,
		}, nil)
		r.NoError(err)
		r.Len(a, 0)

		r1, ok := d.m.Get(0)
		r.True(ok)

		r.Equal(y, r1.Live())

		r2, ok := d.m.Get(47)
		r.True(ok)

		r.Equal(x, r2.Live())
	})

	t.Run("disjoint updates suffix", func(t *testing.T) {
		r := require.New(t)

		d := NewExtentMap()

		y := Extent{0, 8}
		a, err := d.Update(log, ExtentLocation{
			Segment:      s1,
			ExtentHeader: ExtentHeader{Offset: 0, Extent: y},
		}, nil)
		r.NoError(err)
		r.Len(a, 0)

		x := Extent{47, 10}
		a, err = d.Update(log, ExtentLocation{
			Segment:      s1,
			ExtentHeader: ExtentHeader{Offset: 47, Extent: x},
		}, nil)
		r.NoError(err)
		r.Len(a, 0)

		r1, ok := d.m.Get(0)
		r.True(ok)

		r.Equal(y, r1.Live())

		r2, ok := d.m.Get(47)
		r.True(ok)

		r.Equal(x, r2.Live())
	})

	t.Run("splits the ranges on update", func(t *testing.T) {
		r := require.New(t)

		d := NewExtentMap()

		x := Extent{LBA: 0, Blocks: 10}

		_, err := d.Update(log, ExtentLocation{
			Segment:      s1,
			ExtentHeader: ExtentHeader{Offset: 1, Extent: x},
		}, nil)
		r.NoError(err)

		y := Extent{1, 1}
		a, err := d.Update(log, ExtentLocation{
			Segment:      s1,
			ExtentHeader: ExtentHeader{Offset: 2, Extent: y},
		}, nil)
		r.NoError(err)
		r.Len(a, 1)

		r.Equal(Extent{1, 1}, a[0].Live)
		r.Equal(uint32(1), a[0].Offset)

		r.Equal(3, d.m.Len())

		r1, ok := d.m.Get(0)
		r.True(ok)

		r.Equal(Extent{0, 1}, r1.Live())
		r.Equal(uint32(1), r1.offset)

		r2, ok := d.m.Get(1)
		r.True(ok)

		r.Equal(Extent{1, 1}, r2.Live())
		r.Equal(uint32(2), r2.offset)

		r3, ok := d.m.Get(2)
		r.True(ok)

		r.Equal(Extent{2, 8}, r3.Live())
		r.Equal(uint32(1), r3.offset)
	})

	t.Run("wipes out a smaller range", func(t *testing.T) {
		r := require.New(t)

		m := NewExtentMap()

		_, err := m.Update(log, ExtentLocation{
			ExtentHeader: ExtentHeader{Offset: 1, Extent: Extent{2, 1}},
		}, nil)
		r.NoError(err)

		a, err := m.Update(log, ExtentLocation{
			ExtentHeader: ExtentHeader{Offset: 2, Extent: Extent{0, 10}},
		}, nil)
		r.NoError(err)
		r.Len(a, 1)
		r.Equal(Extent{2, 1}, a[0].Live)
		r.Equal(uint32(1), a[0].Offset)

		r.Equal(1, m.m.Len())

		_, ok := m.m.Get(2)
		r.False(ok)

		r1, ok := m.m.Get(0)
		r.True(ok)

		r.Equal(Extent{0, 10}, r1.Live())
	})

	t.Run("adjusts an earlier overlapping range", func(t *testing.T) {
		r := require.New(t)

		m := NewExtentMap()

		_, err := m.Update(log, ExtentLocation{
			ExtentHeader: ExtentHeader{Offset: 1, Extent: Extent{0, 5}},
		}, nil)
		r.NoError(err)

		a, err := m.Update(log, ExtentLocation{
			ExtentHeader: ExtentHeader{Offset: 2, Extent: Extent{3, 10}},
		}, nil)
		r.NoError(err)
		r.Len(a, 1)

		r.Equal(Extent{3, 2}, a[0].Live)
		r.Equal(uint32(1), a[0].Offset)

		r.Equal(2, m.m.Len())

		r1, ok := m.m.Get(0)
		r.True(ok)

		r.Equal(Extent{0, 3}, r1.Live())

		r2, ok := m.m.Get(3)
		r.True(ok)

		r.Equal(Extent{3, 10}, r2.Live())
	})

	t.Run("adjusts a later overlapping range", func(t *testing.T) {
		r := require.New(t)

		m := NewExtentMap()

		_, err := m.Update(log, ExtentLocation{
			ExtentHeader: ExtentHeader{Offset: 1, Extent: Extent{3, 10}},
		}, nil)
		r.NoError(err)

		a, err := m.Update(log, ExtentLocation{
			ExtentHeader: ExtentHeader{Offset: 2, Extent: Extent{0, 5}},
		}, nil)
		r.NoError(err)
		r.Len(a, 1)

		r.Equal(Extent{3, 2}, a[0].Live)
		r.Equal(uint32(1), a[0].Offset)

		r.Equal(2, m.m.Len())

		r1, ok := m.m.Get(0)
		r.True(ok)

		r.Equal(Extent{0, 5}, r1.Live())

		r2, ok := m.m.Get(5)
		r.True(ok)

		r.Equal(Extent{5, 8}, r2.Live())
	})

	t.Run("adjusts a later boundary range", func(t *testing.T) {
		r := require.New(t)

		m := NewExtentMap()

		_, err := m.Update(log, ExtentLocation{
			ExtentHeader: ExtentHeader{Offset: 1, Extent: Extent{3, 2}},
		}, nil)
		r.NoError(err)

		_, err = m.Update(log, ExtentLocation{
			ExtentHeader: ExtentHeader{Offset: 2, Extent: Extent{0, 5}},
		}, nil)
		r.NoError(err)

		r.Equal(1, m.m.Len())

		r1, ok := m.m.Get(0)
		r.True(ok)

		r.Equal(Extent{0, 5}, r1.Live())
	})

	t.Run("removes a range that starts at the same place and is smaller", func(t *testing.T) {
		r := require.New(t)

		m := NewExtentMap()

		_, err := m.Update(log, ExtentLocation{
			ExtentHeader: ExtentHeader{Offset: 1, Extent: Extent{1, 1}},
		}, nil)
		r.NoError(err)

		a, err := m.Update(log, ExtentLocation{
			ExtentHeader: ExtentHeader{Offset: 2, Extent: Extent{1, 5}},
		}, nil)
		r.NoError(err)
		r.Len(a, 1)

		r.Equal(Extent{1, 1}, a[0].Live)
		r.Equal(uint32(1), a[0].Offset)

		t.Log(m.Render())

		r.Equal(1, m.m.Len())

		r1, ok := m.m.Get(1)
		r.True(ok)

		r.Equal(Extent{1, 5}, r1.Live())
	})

	t.Run("doesn't removes non overlapping range", func(t *testing.T) {
		r := require.New(t)

		m := NewExtentMap()

		_, err := m.Update(log, ExtentLocation{
			ExtentHeader: ExtentHeader{Offset: 1, Extent: Extent{0, 1}},
		}, nil)
		r.NoError(err)

		_, err = m.Update(log, ExtentLocation{
			ExtentHeader: ExtentHeader{Offset: 2, Extent: Extent{1, 1}},
		}, nil)
		r.NoError(err)

		t.Log(m.Render())

		r.Equal(2, m.m.Len())

		_, err = m.Update(log, ExtentLocation{
			ExtentHeader: ExtentHeader{Offset: 2, Extent: Extent{1, 1}},
		}, nil)
		r.NoError(err)

		r.Equal(2, m.m.Len())

		r1, ok := m.m.Get(0)
		r.True(ok)

		r.Equal(Extent{0, 1}, r1.Live())
	})

	t.Run("removes multiple ranges", func(t *testing.T) {
		r := require.New(t)

		m := NewExtentMap()

		_, err := m.Update(log, ExtentLocation{
			ExtentHeader: ExtentHeader{Offset: 1, Extent: Extent{1, 1}},
		}, nil)
		r.NoError(err)

		_, err = m.Update(log, ExtentLocation{
			ExtentHeader: ExtentHeader{Offset: 2, Extent: Extent{2, 1}},
		}, nil)
		r.NoError(err)

		a, err := m.Update(log, ExtentLocation{
			ExtentHeader: ExtentHeader{Offset: 2, Extent: Extent{0, 5}},
		}, nil)
		r.NoError(err)
		r.Len(a, 2)

		r.Equal(Extent{1, 1}, a[0].Live)
		r.Equal(uint32(1), a[0].Offset)
		r.Equal(Extent{2, 1}, a[1].Live)
		r.Equal(uint32(2), a[1].Offset)

		r.Equal(1, m.m.Len())

		r1, ok := m.m.Get(0)
		r.True(ok)

		r.Equal(Extent{0, 5}, r1.Live())
	})

	t.Run("adjusts multiple ranges", func(t *testing.T) {
		r := require.New(t)

		m := NewExtentMap()

		_, err := m.Update(log, ExtentLocation{
			ExtentHeader: ExtentHeader{Offset: 1, Extent: Extent{8, 1}},
		}, nil)
		r.NoError(err)

		a, err := m.Update(log, ExtentLocation{
			ExtentHeader: ExtentHeader{Offset: 2, Extent: Extent{11, 1}},
		}, nil)
		r.NoError(err)

		r.Len(a, 0)

		a, err = m.Update(log, ExtentLocation{
			ExtentHeader: ExtentHeader{Offset: 3, Extent: Extent{12, 10}},
		}, nil)
		r.NoError(err)

		r.Len(a, 0)

		a, err = m.Update(log, ExtentLocation{
			ExtentHeader: ExtentHeader{Offset: 4, Extent: Extent{10, 5}},
		}, nil)
		r.NoError(err)

		r.Len(a, 2)
		r.Equal(Extent{11, 1}, a[0].Live)
		r.Equal(uint32(2), a[0].Offset)
		r.Equal(Extent{12, 3}, a[1].Live)
		r.Equal(uint32(3), a[1].Offset)

		r.Equal(3, m.m.Len())

		r1, ok := m.m.Get(8)
		r.True(ok)

		r.Equal(Extent{8, 1}, r1.Live())

		r2, ok := m.m.Get(10)
		r.True(ok)

		r.Equal(Extent{10, 5}, r2.Live())

		r3, ok := m.m.Get(15)
		r.True(ok)

		t.Log(m.Render())

		r.Equal(Extent{15, 7}, r3.Live())
		r.Equal(Extent{12, 10}.Last(), Extent{15, 7}.Last())
	})

	t.Run("emits affected range once only", func(t *testing.T) {
		r := require.New(t)

		m := NewExtentMap()

		_, err := m.Update(log, ExtentLocation{
			ExtentHeader: ExtentHeader{Offset: 1, Extent: Extent{8, 1}},
		}, nil)
		r.NoError(err)

		a, err := m.Update(log, ExtentLocation{
			ExtentHeader: ExtentHeader{Offset: 2, Extent: Extent{11, 1}},
		}, nil)
		r.NoError(err)

		r.Len(a, 0)

		a, err = m.Update(log, ExtentLocation{
			ExtentHeader: ExtentHeader{Offset: 3, Extent: Extent{12, 10}},
		}, nil)
		r.NoError(err)

		r.Len(a, 0)

		a, err = m.Update(log, ExtentLocation{
			ExtentHeader: ExtentHeader{Offset: 4, Extent: Extent{10, 5}},
		}, nil)
		r.NoError(err)

		r.Len(a, 2)
		r.Equal(Extent{11, 1}, a[0].Live)
		r.Equal(uint32(2), a[0].Offset)
		r.Equal(Extent{12, 3}, a[1].Live)
		r.Equal(uint32(3), a[1].Offset)

		a, err = m.Update(log, ExtentLocation{
			ExtentHeader: ExtentHeader{Offset: 5, Extent: Extent{10, 5}},
		}, nil)
		r.NoError(err)

		r.Len(a, 1)
		r.Equal(Extent{10, 5}, a[0].Live)
		r.Equal(uint32(4), a[0].Offset)
	})

	t.Run("report all pbas for a range", func(t *testing.T) {
		r := require.New(t)

		m := NewExtentMap()

		_, err := m.Update(log, ExtentLocation{
			ExtentHeader: ExtentHeader{Offset: 1, Extent: Extent{0, 5}},
		}, nil)
		r.NoError(err)

		_, err = m.Update(log, ExtentLocation{
			ExtentHeader: ExtentHeader{Offset: 2, Extent: Extent{5, 5}},
		}, nil)
		r.NoError(err)

		_, err = m.Update(log, ExtentLocation{
			ExtentHeader: ExtentHeader{Offset: 3, Extent: Extent{10, 5}},
		}, nil)
		r.NoError(err)

		_, err = m.Update(log, ExtentLocation{
			ExtentHeader: ExtentHeader{Offset: 4, Extent: Extent{15, 5}},
		}, nil)
		r.NoError(err)

		_, err = m.Update(log, ExtentLocation{
			ExtentHeader: ExtentHeader{Offset: 4, Extent: Extent{100, 5}},
		}, nil)
		r.NoError(err)

		r.Equal(5, m.m.Len())

		pbas, err := m.Resolve(log, Extent{7, 20}, nil)
		r.NoError(err)

		r.Len(pbas, 3)

		r.Equal(uint32(2), pbas[0].Offset)
		r.Equal(uint32(3), pbas[1].Offset)
		r.Equal(uint32(4), pbas[2].Offset)
	})

	t.Run("resolves a range that matches the lba", func(t *testing.T) {
		r := require.New(t)

		m := NewExtentMap()

		_, err := m.Update(log, ExtentLocation{
			ExtentHeader: ExtentHeader{
				Extent: Extent{0, 5},
				Offset: 1},
		}, nil)
		r.NoError(err)

		r.Equal(1, m.m.Len())

		t.Log(m.Render())

		pbas, err := m.Resolve(log, Extent{0, 5}, nil)
		r.NoError(err)

		r.Len(pbas, 1)

		r.Equal(uint32(1), pbas[0].Offset)
	})

	t.Run("resolves a range that starts before the lba", func(t *testing.T) {
		r := require.New(t)

		m := NewExtentMap()

		_, err := m.Update(log, ExtentLocation{
			ExtentHeader: ExtentHeader{
				Extent: Extent{1, 1},
				Offset: 1},
		}, nil)
		r.NoError(err)

		r.Equal(1, m.m.Len())

		t.Log(m.Render())

		pbas, err := m.Resolve(log, Extent{0, 5}, nil)
		r.NoError(err)

		r.Len(pbas, 1)

		r.Equal(uint32(1), pbas[0].Offset)
	})

	t.Run("tc", func(t *testing.T) {
		r := require.New(t)

		m := NewExtentMap()

		inject := []Extent{
			{5799956, 5},
			{LBA: 5799968, Blocks: 32},
			{LBA: 5799936, Blocks: 1},
		}

		for i, e := range inject {
			_, err := m.Update(log, ExtentLocation{
				ExtentHeader: ExtentHeader{Offset: uint32(i), Extent: e},
			}, nil)
			r.NoError(err)
		}

		r.Equal(3, m.m.Len())

		t.Log(m.Render())

		_, err := m.Update(log, ExtentLocation{
			ExtentHeader: ExtentHeader{Offset: 2, Extent: Extent{5799956, 13}},
		}, nil)
		r.NoError(err)

		r.Equal(3, m.m.Len())

		_, ok := m.m.Get(5799968)
		r.False(ok)

		r1, ok := m.m.Get(5799969)
		r.True(ok)

		r.Equal(Extent{5799969, 31}, r1.Live())

		r2, ok := m.m.Get(5799956)
		r.True(ok)

		r.Equal(Extent{5799956, 13}, r2.Live())
	})

	t.Run("tc2", func(t *testing.T) {
		r := require.New(t)

		m := NewExtentMap()

		inject := []Extent{
			{7234450, 40},
			{7234490, 1},
			{7234491, 5},
			{7234496, 1},
		}

		for i, e := range inject {
			_, err := m.Update(log, ExtentLocation{
				ExtentHeader: ExtentHeader{Offset: uint32(i), Extent: e},
			}, nil)
			r.NoError(err)
		}

		r.Equal(4, m.m.Len())

		t.Log(m.Render())

		h := Extent{7234460, 31}
		pes, err := m.Resolve(log, h, nil)
		r.NoError(err)

		r.Len(pes, 2)

		r.Equal(LBA(7234450), pes[0].LBA)
		r.Equal(LBA(7234490), pes[1].LBA)
	})
}
