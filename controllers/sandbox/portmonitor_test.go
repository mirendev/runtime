package sandbox

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestPortListening(t *testing.T) {
	// Sample /proc/net/tcp content with a listener on port 6667 (0x1A0B)
	// and an established connection on port 3000 (0x0BB8)
	content := `  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 00000000:1A0B 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 12345 1 0000000000000000 100 0 0 10 0
   1: 0100007F:0BB8 0100007F:C000 01 00000000:00000000 00:00000000 00000000  1000        0 67890 1 0000000000000000 20 0 0 10 -1
`

	dir := t.TempDir()
	path := filepath.Join(dir, "tcp")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		port int
		want bool
	}{
		{6667, true},  // listening (state 0A)
		{3000, false}, // established (state 01), not listening
		{8080, false}, // not present at all
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("port_%d", tt.port), func(t *testing.T) {
			got := portListening(path, tt.port)
			if got != tt.want {
				t.Errorf("portListening(path, %d) = %v, want %v", tt.port, got, tt.want)
			}
		})
	}
}

func TestPortListeningIPv6(t *testing.T) {
	// Sample /proc/net/tcp6 content with a listener on port 3000
	content := `  sl  local_address                         remote_address                        st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 00000000000000000000000000000000:0BB8 00000000000000000000000000000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 12345 1 0000000000000000 100 0 0 10 0
`

	dir := t.TempDir()
	path := filepath.Join(dir, "tcp6")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	if !portListening(path, 3000) {
		t.Error("expected port 3000 to be listening in tcp6")
	}

	if portListening(path, 8080) {
		t.Error("expected port 8080 to not be listening in tcp6")
	}
}

func TestPortListeningMissingFile(t *testing.T) {
	if portListening("/nonexistent/path/tcp", 80) {
		t.Error("expected false for missing file")
	}
}

func TestParseHexAddr(t *testing.T) {
	tests := []struct {
		hex  string
		want string
	}{
		{"0100007F", "127.0.0.1"},
		{"00000000", "0.0.0.0"},
		{"0101A8C0", "192.168.1.1"},
		// IPv6 ::1 (loopback): last word little-endian = 01000000...
		{"00000000000000000000000001000000", "::1"},
		// IPv6 unspecified ::
		{"00000000000000000000000000000000", "::"},
	}

	for _, tt := range tests {
		t.Run(tt.hex, func(t *testing.T) {
			addr, ok := parseHexAddr(tt.hex)
			if !ok {
				t.Fatalf("parseHexAddr(%q) returned ok=false", tt.hex)
			}
			if addr.String() != tt.want {
				t.Errorf("parseHexAddr(%q) = %s, want %s", tt.hex, addr.String(), tt.want)
			}
		})
	}

	if _, ok := parseHexAddr("zzzz"); ok {
		t.Error("expected ok=false for invalid hex")
	}
}

func TestListListeningPorts(t *testing.T) {
	// Listener on 0.0.0.0:6667 (0x1A0B), listener on 127.0.0.1:8080 (0x1F90),
	// and an established (non-listening) connection that must be ignored.
	content := `  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 00000000:1A0B 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 12345 1 0000000000000000 100 0 0 10 0
   1: 0100007F:1F90 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 23456 1 0000000000000000 100 0 0 10 0
   2: 0100007F:0BB8 0100007F:C000 01 00000000:00000000 00:00000000 00000000  1000        0 67890 1 0000000000000000 20 0 0 10 -1
`
	dir := t.TempDir()
	path := filepath.Join(dir, "tcp")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	got, ok := listListeningPorts(path)
	if !ok {
		t.Fatal("expected ok=true for a readable file")
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 listening sockets, got %d: %+v", len(got), got)
	}

	if got[0].port != 6667 || !got[0].addr.IsUnspecified() {
		t.Errorf("socket 0 = %+v, want 0.0.0.0:6667", got[0])
	}
	if got[1].port != 8080 || !got[1].addr.IsLoopback() {
		t.Errorf("socket 1 = %+v, want 127.0.0.1:8080", got[1])
	}
}

func TestListeningPortsForPIDClassification(t *testing.T) {
	// A routable listener and a loopback-only listener under the current
	// process, exercised through the real /proc/<pid>/net/tcp path.
	routableLn, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	defer routableLn.Close()
	routablePort := routableLn.Addr().(*net.TCPAddr).Port

	loopbackLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer loopbackLn.Close()
	loopbackPort := loopbackLn.Addr().(*net.TCPAddr).Port

	routable, loopback, ok := listeningPortsForPID(os.Getpid())
	if !ok {
		t.Fatal("expected ok=true reading this process's sockets")
	}

	if !slices.Contains(routable, routablePort) {
		t.Errorf("expected routable ports %v to contain %d", routable, routablePort)
	}
	// The loopback port must not be advertised as routable.
	if slices.Contains(routable, loopbackPort) {
		t.Errorf("loopback port %d should not appear in routable set %v", loopbackPort, routable)
	}
	if !slices.Contains(loopback, loopbackPort) {
		t.Errorf("expected loopback ports %v to contain %d", loopback, loopbackPort)
	}
}

func TestDiagnoseListening(t *testing.T) {
	pm := &PortMonitor{
		tasks: map[string]*monitorTask{"c1": {pid: 1234}},
		listPorts: func(pid int) ([]int, []int, bool) {
			return []int{8080}, []int{5000}, true
		},
	}

	routable, loopback, ok := pm.DiagnoseListening("c1")
	if !ok {
		t.Fatal("expected ok=true for a monitored container")
	}
	if len(routable) != 1 || routable[0] != 8080 {
		t.Errorf("routable = %v, want [8080]", routable)
	}
	if len(loopback) != 1 || loopback[0] != 5000 {
		t.Errorf("loopback = %v, want [5000]", loopback)
	}

	// An unmonitored container reports ok=false.
	if _, _, ok := pm.DiagnoseListening("unknown"); ok {
		t.Error("expected ok=false for an unmonitored container")
	}
}

func TestCheckPortWithCurrentProcess(t *testing.T) {
	// Listen on a random port and verify checkPort finds it via our own PID
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	port := ln.Addr().(*net.TCPAddr).Port
	pid := os.Getpid()

	if !checkPort(pid, port) {
		t.Errorf("checkPort(%d, %d) = false, want true for listening port", pid, port)
	}

	if checkPort(pid, port+1) {
		t.Errorf("checkPort(%d, %d) = true, want false for non-listening port", pid, port+1)
	}
}

func TestListListeningPortsUnreadable(t *testing.T) {
	_, ok := listListeningPorts(filepath.Join(t.TempDir(), "missing"))
	if ok {
		t.Fatal("an unreadable socket table must not look like a process listening on nothing")
	}
}

func TestDiagnoseListeningUnreadable(t *testing.T) {
	pm := &PortMonitor{
		tasks: map[string]*monitorTask{"c1": {pid: 1234}},
		listPorts: func(pid int) ([]int, []int, bool) {
			return nil, nil, false
		},
	}
	if _, _, ok := pm.DiagnoseListening("c1"); ok {
		t.Fatal("expected ok=false when the sockets couldn't be read")
	}
}
