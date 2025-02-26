// Compute Shannon Entropy of a byte stream
// H = - Σ P(x) * log P(x)

package entropy

import (
	"io"
	"math"
)

type Estimator interface {
	io.Writer
	Value() float64
	Reset()
}

type shannon struct {
	frequencies [256]int
	total       int
}

func (s *shannon) Reset() {
	clear(s.frequencies[:])
	s.total = 0
}

func (s *shannon) Write(data []byte) (int, error) {
	for _, b := range data {
		s.frequencies[b] += 1
	}
	s.total += len(data)
	return len(data), nil
}

func (s *shannon) Value() float64 {
	var entropy float64
	for _, count := range s.frequencies {
		if count > 0 {
			//  ent = ent + freq * math.log(freq, 2)
			freq := float64(count) / float64(s.total)
			entropy += freq * math.Log2(freq)
		}
	}
	return -entropy
}

func NewEstimator() Estimator {
	s := &shannon{}
	s.Reset()
	return s
}
