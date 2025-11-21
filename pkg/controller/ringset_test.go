package controller

import (
	"sync"
	"testing"
)

func TestRingSet_BasicOperations(t *testing.T) {
	rs := NewRingSet(3)

	// Test Contains on empty set
	if rs.Contains(1) {
		t.Error("Empty set should not contain 1")
	}

	// Test Add and Contains
	rs.Add(1)
	if !rs.Contains(1) {
		t.Error("Set should contain 1 after Add(1)")
	}

	// Test multiple adds
	rs.Add(2)
	rs.Add(3)
	if !rs.Contains(1) || !rs.Contains(2) || !rs.Contains(3) {
		t.Error("Set should contain 1, 2, and 3")
	}

	// Test ring buffer wraparound - adding 4 should evict 1
	rs.Add(4)
	if rs.Contains(1) {
		t.Error("Set should not contain 1 after wraparound")
	}
	if !rs.Contains(2) || !rs.Contains(3) || !rs.Contains(4) {
		t.Error("Set should contain 2, 3, and 4")
	}

	// Test adding duplicate - should not change anything
	rs.Add(3)
	if !rs.Contains(2) || !rs.Contains(3) || !rs.Contains(4) {
		t.Error("Set should still contain 2, 3, and 4 after duplicate add")
	}
}

func TestRingSet_Wraparound(t *testing.T) {
	rs := NewRingSet(2)

	// Fill the buffer
	rs.Add(1)
	rs.Add(2)

	// Add more, causing wraparound
	rs.Add(3) // Should evict 1
	if rs.Contains(1) || !rs.Contains(2) || !rs.Contains(3) {
		t.Error("Expected set to contain {2, 3} after first wraparound")
	}

	rs.Add(4) // Should evict 2
	if !rs.Contains(3) || !rs.Contains(4) || rs.Contains(2) {
		t.Error("Expected set to contain {3, 4} after second wraparound")
	}
}

func TestRingSet_Concurrent(t *testing.T) {
	rs := NewRingSet(100)
	var wg sync.WaitGroup

	// Spawn multiple goroutines adding values
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(start int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				rs.Add(int64(start*50 + j))
			}
		}(i)
	}

	// Spawn multiple goroutines checking values
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(start int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				_ = rs.Contains(int64(start*50 + j))
			}
		}(i)
	}

	wg.Wait()
	// Test just verifies no race conditions or panics
}

func TestRingSet_DuplicateAddsSamePosition(t *testing.T) {
	rs := NewRingSet(3)

	// Add value
	rs.Add(1)
	if !rs.Contains(1) {
		t.Error("Set should contain 1")
	}

	// Add same value again - should be no-op
	rs.Add(1)
	if !rs.Contains(1) {
		t.Error("Set should still contain 1")
	}

	// Verify size didn't grow incorrectly
	rs.Add(2)
	rs.Add(3)
	rs.Add(4) // Should evict 1

	if rs.Contains(1) {
		t.Error("Duplicate add of 1 should not have affected ring buffer eviction")
	}
}

func TestRingSet_ZeroCapacity(t *testing.T) {
	// Edge case: zero capacity should handle gracefully
	rs := NewRingSet(0)
	rs.Add(1)
	// With zero capacity, nothing should be retained
	// (Though this is a degenerate case that shouldn't happen in practice)
}
