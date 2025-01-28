package set

import "iter"

type Set[K comparable] map[K]struct{}

func New[K comparable]() Set[K] {
	return make(Set[K])
}

func (s Set[K]) Add(k K) {
	s[k] = struct{}{}
}

func (s Set[K]) Remove(k K) {
	delete(s, k)
}

func (s Set[K]) Contains(k K) bool {
	_, ok := s[k]
	return ok
}

func (s Set[K]) Len() int {
	return len(s)
}

func (s Set[K]) Empty() bool {
	return s.Len() == 0
}

func (s Set[K]) Values() []K {
	vals := make([]K, 0, len(s))
	for k := range s {
		vals = append(vals, k)
	}
	return vals
}

// Each returns an iterator over the set's elements
func (s Set[K]) Each() iter.Seq[K] {
	return func(yield func(K) bool) {
		for k := range s {
			if !yield(k) {
				return
			}
		}
	}
}
