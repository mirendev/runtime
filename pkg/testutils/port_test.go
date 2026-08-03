package testutils

import "testing"

// TestGetFreePortReturnsDistinctPorts pins the guarantee that resolves MIR-720:
// GetFreePort releases each port before returning it, so successive calls used
// to be able to hand back the same number (etcd then failed to bind its client
// and peer URLs to one port). Every port must now be unique within the process.
func TestGetFreePortReturnsDistinctPorts(t *testing.T) {
	const n = 200
	seen := make(map[int]bool, n)
	for i := 0; i < n; i++ {
		port := GetFreePort(t)
		if port <= 0 {
			t.Fatalf("call %d: GetFreePort returned non-positive port %d", i, port)
		}
		if seen[port] {
			t.Fatalf("call %d: GetFreePort returned duplicate port %d", i, port)
		}
		seen[port] = true
	}
}
