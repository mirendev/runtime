package testutils

import (
	"net"
	"testing"
)

// GetFreePort returns a free TCP port by binding to :0 and immediately closing.
// The OS assigns an available port which is then released for the caller to use.
// Fails the test if a port cannot be obtained.
func GetFreePort(t testing.TB) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to get free port: %v", err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}
