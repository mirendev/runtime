package lsvd

import "sync"

// PreviousCache manages holding onto a single segment creator as
// the previous cache.
type PreviousCache struct {
	prevCacheMu   sync.Mutex
	prevCacheCond *sync.Cond
	prevCache     *SegmentCreator
}

func NewPreviousCache() *PreviousCache {
	p := &PreviousCache{}
	p.prevCacheCond = sync.NewCond(&p.prevCacheMu)

	return p
}

func (p *PreviousCache) Load() *SegmentCreator {
	p.prevCacheMu.Lock()
	defer p.prevCacheMu.Unlock()

	return p.prevCache
}

func (p *PreviousCache) Clear() {
	p.prevCacheMu.Lock()
	defer p.prevCacheMu.Unlock()

	p.prevCache = nil

	p.prevCacheCond.Signal()
}

func (p *PreviousCache) SetWhenClear(sc *SegmentCreator) {
	p.prevCacheMu.Lock()
	defer p.prevCacheMu.Unlock()

	for p.prevCache != nil {
		p.prevCacheCond.Wait()
	}

	p.prevCache = sc
}
