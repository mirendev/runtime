package lsvd

import "sync"

// PreviousCache manages holding onto a single segment creator as
// the previous cache. It uses reference counting to ensure the
// SegmentCreator is not closed while reads are still in progress.
type PreviousCache struct {
	prevCacheMu   sync.Mutex
	prevCacheCond *sync.Cond
	prevCache     *SegmentCreator
	activeReaders int
}

func NewPreviousCache() *PreviousCache {
	p := &PreviousCache{}
	p.prevCacheCond = sync.NewCond(&p.prevCacheMu)

	return p
}

// Load returns the current SegmentCreator without incrementing the reference count.
// This should only be used when the caller does not need to perform I/O operations
// on the returned SegmentCreator, as it may be closed at any time.
func (p *PreviousCache) Load() *SegmentCreator {
	p.prevCacheMu.Lock()
	defer p.prevCacheMu.Unlock()

	return p.prevCache
}

// Acquire returns the current SegmentCreator and increments the reference count.
// The caller MUST call Release() when done using the SegmentCreator.
// Returns nil if there is no previous cache.
func (p *PreviousCache) Acquire() *SegmentCreator {
	p.prevCacheMu.Lock()
	defer p.prevCacheMu.Unlock()

	if p.prevCache != nil {
		p.activeReaders++
	}

	return p.prevCache
}

// Release decrements the reference count after the caller is done using the
// SegmentCreator obtained from Acquire(). Must be called exactly once for
// each successful Acquire() call that returned a non-nil value.
func (p *PreviousCache) Release() {
	p.prevCacheMu.Lock()
	defer p.prevCacheMu.Unlock()

	p.activeReaders--
	if p.activeReaders == 0 {
		p.prevCacheCond.Broadcast()
	}
}

// Clear waits for all active readers to finish and then clears the cache.
// After this returns, no readers will be able to access the old SegmentCreator.
func (p *PreviousCache) Clear() {
	p.prevCacheMu.Lock()
	defer p.prevCacheMu.Unlock()

	// Wait for all active readers to finish
	for p.activeReaders > 0 {
		p.prevCacheCond.Wait()
	}

	p.prevCache = nil

	p.prevCacheCond.Broadcast()
}

func (p *PreviousCache) SetWhenClear(sc *SegmentCreator) {
	p.prevCacheMu.Lock()
	defer p.prevCacheMu.Unlock()

	for p.prevCache != nil {
		p.prevCacheCond.Wait()
	}

	p.prevCache = sc
}
