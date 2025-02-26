package lsvd

import (
	"fmt"
	"math"
	"sort"
)

const (
	MaxBlocks = math.MaxUint16
	MaxLBA    = (1 << 48) - 1
)

type LBA uint64

type Extent struct {
	LBA    LBA
	Blocks uint32
}

func ExtentFrom(a, b LBA) (Extent, bool) {
	if b < a {
		return Extent{}, false
	}
	return Extent{LBA: a, Blocks: uint32(b - a + 1)}, true
}

func (e Extent) ByteSize() int {
	return int(e.Blocks) * BlockSize
}

func (e Extent) String() string {
	return fmt.Sprintf("%d:%d", e.LBA, e.Blocks)
}

func (e Extent) Contains(lba LBA) bool {
	return lba >= e.LBA && lba < (e.LBA+LBA(e.Blocks))
}

func (e Extent) Last() LBA {
	return (e.LBA + LBA(e.Blocks) - 1)
}

func (e Extent) Range() (LBA, LBA) {
	return e.LBA, e.LBA + LBA(e.Blocks) - 1
}

func (e Extent) Cover(y Extent) Cover {
	es, ef := e.Range()
	ys, yf := y.Range()

	if ef < ys || yf < es {
		return CoverNone
	}

	if es == ys && ef == yf {
		return CoverExact
	}

	// es    ys     yf    ef
	if es <= ys && ef >= yf {
		// e is a superange of y
		return CoverSuperRange
	}

	return CoverPartly
}

// Returns a new Extent that the region of +e+ that overlaps with +y+.
func (e Extent) Clamp(y Extent) (Extent, bool) {
	es, ef := e.Range()
	ys, yf := y.Range()

	if ef < ys || yf < es {
		return Extent{}, false
	}

	if es == ys && ef == yf {
		return y, true
	}

	var start, end LBA

	if ys <= es {
		start = es
	} else {
		start = ys
	}

	if yf >= ef {
		end = ef
	} else {
		end = yf
	}

	return ExtentFrom(start, end)
}

func (e Extent) Sub(o Extent) ([]Extent, bool) {
	pre, suf, ok := e.SubSpecific(o)
	if !ok {
		return nil, false
	}

	var xs []Extent

	if pre.Valid() {
		xs = append(xs, pre)
	}

	if suf.Valid() {
		xs = append(xs, suf)
	}

	return xs, true
}

func (e Extent) SubSpecific(o Extent) (Extent, Extent, bool) {
	es, ef := e.Range()
	os, of := o.Range()

	if ef < os || es > of {
		return Extent{}, Extent{}, false
	}

	if es == os && ef == of {
		return Extent{}, Extent{}, true
	}

	if es >= os {
		suffix, ok := ExtentFrom(of+1, ef)
		if !ok {
			return Extent{}, Extent{}, false
		}

		return Extent{}, suffix, true
	}

	// o falls within e but not at the beginning

	prefix, ok := ExtentFrom(es, os-1)
	if !ok {
		return Extent{}, Extent{}, false
	}

	if of >= ef {
		return prefix, Extent{}, true
	}

	suffix, ok := ExtentFrom(of+1, ef)
	if !ok {
		return Extent{}, Extent{}, false
	}

	return prefix, suffix, true
}

func (e Extent) Valid() bool {
	return e.Blocks > 0
}

func (e Extent) SubMany(subs []Extent) ([]Extent, bool) {
	sort.Slice(subs, func(i, j int) bool {
		a := subs[i]
		b := subs[j]

		if a.LBA < b.LBA {
			return true
		}

		if a.LBA == b.LBA {
			return a.Blocks < b.Blocks
		}

		return false
	})

	var holes []Extent

	considering := e

	for _, s := range subs {
		prefix, suffix, ok := considering.SubSpecific(s)
		if !ok {
			return nil, false
		}

		if prefix.Valid() {
			holes = append(holes, prefix)
		}

		if suffix.Valid() {
			considering = suffix
		} else {
			considering = Extent{}
			break
		}
	}

	if considering.Valid() {
		holes = append(holes, considering)
	}

	return holes, true
}

type Mask struct {
	remaining []Extent
}

func (e Extent) StartMask() *Mask {
	return &Mask{remaining: []Extent{e}}
}

func (m *Mask) Cover(x Extent) error {
	return nil
}

func (h *Mask) Holes() []Extent {
	return nil
}
