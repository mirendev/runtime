package lsvd

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPreviousCacheAcquireRelease(t *testing.T) {
	t.Run("acquire returns nil when cache is empty", func(t *testing.T) {
		pc := NewPreviousCache()
		sc := pc.Acquire()
		require.Nil(t, sc)
	})

	t.Run("acquire returns SegmentCreator when cache is set", func(t *testing.T) {
		pc := NewPreviousCache()
		sc := &SegmentCreator{}
		pc.SetWhenClear(sc)

		acquired := pc.Acquire()
		require.Same(t, sc, acquired)

		// Must release
		pc.Release()
	})

	t.Run("clear blocks until all readers release", func(t *testing.T) {
		pc := NewPreviousCache()
		sc := &SegmentCreator{}
		pc.SetWhenClear(sc)

		// Acquire a reference
		acquired := pc.Acquire()
		require.Same(t, sc, acquired)

		var clearDone atomic.Bool
		var wg sync.WaitGroup
		wg.Add(1)

		// Start clearing in background - should block
		go func() {
			defer wg.Done()
			pc.Clear()
			clearDone.Store(true)
		}()

		// Give the goroutine time to start and block
		time.Sleep(50 * time.Millisecond)

		// Clear should still be blocked
		require.False(t, clearDone.Load(), "Clear should block while reader is active")

		// Release the reference
		pc.Release()

		// Wait for clear to complete
		wg.Wait()

		// Clear should have completed
		require.True(t, clearDone.Load(), "Clear should complete after release")

		// Cache should be empty now
		require.Nil(t, pc.Load())
	})

	t.Run("multiple readers all block clear", func(t *testing.T) {
		pc := NewPreviousCache()
		sc := &SegmentCreator{}
		pc.SetWhenClear(sc)

		// Acquire multiple references
		const numReaders = 5
		for i := 0; i < numReaders; i++ {
			acquired := pc.Acquire()
			require.Same(t, sc, acquired)
		}

		var clearDone atomic.Bool
		var wg sync.WaitGroup
		wg.Add(1)

		// Start clearing in background
		go func() {
			defer wg.Done()
			pc.Clear()
			clearDone.Store(true)
		}()

		// Give the goroutine time to start
		time.Sleep(50 * time.Millisecond)

		// Release all but one reader
		for i := 0; i < numReaders-1; i++ {
			pc.Release()
			time.Sleep(10 * time.Millisecond)
			require.False(t, clearDone.Load(), "Clear should still block with active readers")
		}

		// Release the last reader
		pc.Release()

		// Wait for clear to complete
		wg.Wait()
		require.True(t, clearDone.Load())
	})

	t.Run("new acquire after clear returns nil", func(t *testing.T) {
		pc := NewPreviousCache()
		sc := &SegmentCreator{}
		pc.SetWhenClear(sc)

		// Acquire and release
		acquired := pc.Acquire()
		require.Same(t, sc, acquired)
		pc.Release()

		// Clear the cache
		pc.Clear()

		// New acquire should return nil
		acquired = pc.Acquire()
		require.Nil(t, acquired)
	})

	t.Run("concurrent acquires and clear", func(t *testing.T) {
		pc := NewPreviousCache()
		sc := &SegmentCreator{}
		pc.SetWhenClear(sc)

		const numReaders = 100
		var wg sync.WaitGroup
		var successfulAcquires atomic.Int32

		// Start many readers concurrently
		for i := 0; i < numReaders; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				acquired := pc.Acquire()
				if acquired != nil {
					successfulAcquires.Add(1)
					// Simulate some work
					time.Sleep(time.Millisecond)
					pc.Release()
				}
			}()
		}

		// Also clear concurrently
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Wait a tiny bit to let some readers start
			time.Sleep(5 * time.Millisecond)
			pc.Clear()
		}()

		// Wait for everything to complete without deadlock
		done := make(chan struct{})
		go func() {
			wg.Wait()
			close(done)
		}()

		select {
		case <-done:
			// Success - no deadlock
		case <-time.After(5 * time.Second):
			t.Fatal("test timed out - possible deadlock")
		}

		// After clear, cache should be empty
		require.Nil(t, pc.Load())
	})
}
