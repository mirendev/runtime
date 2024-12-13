package set

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
