package controller

import "sync"

// RingSet maintains a fixed-size set of recently seen int64 values using a ring buffer.
// Controllers use this to track recently written entity revisions, allowing them to skip
// watch events for their own writes and reduce unnecessary reconciliation noise.
//
// Thread-safe for concurrent access by multiple workers and the watch goroutine.
type RingSet struct {
	mu     sync.RWMutex
	buffer []int64
	seen   map[int64]bool
	head   int
	size   int
	cap    int
}

func NewRingSet(capacity int) *RingSet {
	return &RingSet{
		buffer: make([]int64, capacity),
		seen:   make(map[int64]bool),
		cap:    capacity,
	}
}

func (r *RingSet) Add(value int64) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Handle zero capacity gracefully (degenerate case)
	if r.cap == 0 {
		return
	}

	if r.seen[value] {
		return // already present
	}

	if r.size == r.cap {
		// Remove oldest value
		oldest := r.buffer[r.head]
		delete(r.seen, oldest)
	} else {
		r.size++
	}

	r.buffer[r.head] = value
	r.seen[value] = true
	r.head = (r.head + 1) % r.cap
}

func (r *RingSet) Contains(value int64) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.seen[value]
}
