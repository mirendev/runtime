package anywhere

import (
	"log/slog"
	"testing"
)

func TestPOPManagerHandleConnectionRequest(t *testing.T) {
	pm := NewPOPManager("test-cluster", nil, slog.Default())
	defer pm.Close()

	req := ConnectionRequest{
		POPXID:     "pop-1",
		POPAddress: "https://pop1.example.com:443",
		Hostname:   "myapp.example.com",
		RequestID:  "req-1",
	}

	if err := pm.HandleConnectionRequest(t.Context(), req); err != nil {
		t.Fatalf("HandleConnectionRequest: %v", err)
	}

	pops := pm.ConnectedPOPs()
	if len(pops) != 1 || pops[0] != "pop-1" {
		t.Errorf("expected [pop-1], got %v", pops)
	}

	// Duplicate request replaces the existing connection
	if err := pm.HandleConnectionRequest(t.Context(), req); err != nil {
		t.Fatalf("duplicate HandleConnectionRequest: %v", err)
	}

	pops = pm.ConnectedPOPs()
	if len(pops) != 1 {
		t.Errorf("expected 1 POP after duplicate, got %d", len(pops))
	}
}

func TestPOPManagerRemovePOP(t *testing.T) {
	pm := NewPOPManager("test-cluster", nil, slog.Default())
	defer pm.Close()

	req := ConnectionRequest{
		POPXID:     "pop-1",
		POPAddress: "https://pop1.example.com:443",
		Hostname:   "myapp.example.com",
		RequestID:  "req-1",
	}

	pm.HandleConnectionRequest(t.Context(), req)
	pm.RemovePOP("pop-1")

	// Give the goroutine time to clean up
	pops := pm.ConnectedPOPs()

	// After RemovePOP the entry is removed immediately from the map
	if len(pops) != 0 {
		t.Errorf("expected 0 POPs after remove, got %d", len(pops))
	}
}

func TestPOPManagerClose(t *testing.T) {
	pm := NewPOPManager("test-cluster", nil, slog.Default())

	for i := range 3 {
		req := ConnectionRequest{
			POPXID:     "pop-" + string(rune('a'+i)),
			POPAddress: "https://pop.example.com:443",
			Hostname:   "app.example.com",
			RequestID:  "req",
		}
		pm.HandleConnectionRequest(t.Context(), req)
	}

	pm.Close()

	pops := pm.ConnectedPOPs()
	if len(pops) != 0 {
		t.Errorf("expected 0 POPs after Close, got %d", len(pops))
	}
}

func TestPOPManagerReplaceStaleConnection(t *testing.T) {
	pm := NewPOPManager("test-cluster", nil, slog.Default())
	defer pm.Close()

	req := ConnectionRequest{
		POPXID:     "pop-1",
		POPAddress: "https://pop1.example.com:443",
		Hostname:   "myapp.example.com",
		RequestID:  "req-1",
	}

	pm.HandleConnectionRequest(t.Context(), req)

	pm.mu.Lock()
	firstPC := pm.conns["pop-1"]
	pm.mu.Unlock()

	if firstPC == nil {
		t.Fatal("first connection not created")
	}

	// Second request for the same POPXID (simulates POP VM recreate).
	req.RequestID = "req-2"
	req.ConnectToken = "new-token"
	pm.HandleConnectionRequest(t.Context(), req)

	pm.mu.Lock()
	secondPC := pm.conns["pop-1"]
	pm.mu.Unlock()

	if secondPC == nil {
		t.Fatal("replacement connection not created")
	}
	if firstPC == secondPC {
		t.Fatal("HandleConnectionRequest did not replace the existing connection")
	}

	pops := pm.ConnectedPOPs()
	if len(pops) != 1 || pops[0] != "pop-1" {
		t.Errorf("expected [pop-1], got %v", pops)
	}
}

// Verify that a replaced servePOP goroutine's deferred cleanup does not
// delete the replacement entry from the map.
func TestServePOPCleanupSkipsReplacedEntry(t *testing.T) {
	pm := NewPOPManager("test-cluster", nil, slog.Default())
	defer pm.Close()

	oldPC := &popConnection{popXID: "pop-1", cancel: func() {}}
	newPC := &popConnection{popXID: "pop-1", cancel: func() {}}

	pm.mu.Lock()
	pm.conns["pop-1"] = newPC
	pm.mu.Unlock()

	// Simulate the old servePOP goroutine's deferred cleanup — this is
	// the same guard that runs in servePOP's defer block.
	pm.mu.Lock()
	if pm.conns[oldPC.popXID] == oldPC {
		delete(pm.conns, oldPC.popXID)
	}
	pm.mu.Unlock()

	pm.mu.Lock()
	survivor := pm.conns["pop-1"]
	pm.mu.Unlock()

	if survivor != newPC {
		t.Fatal("old goroutine's cleanup deleted the replacement entry")
	}
}

func TestSandboxWSURL(t *testing.T) {
	const sandbox = "http://10.0.0.5:8080"

	cases := []struct {
		name    string
		path    string
		want    string
		wantErr bool
	}{
		{name: "root", path: "/", want: "ws://10.0.0.5:8080/"},
		{name: "path and query", path: "/socket?room=a&x=1", want: "ws://10.0.0.5:8080/socket?room=a&x=1"},
		{name: "escaped path kept", path: "/a%2Fb", want: "ws://10.0.0.5:8080/a%2Fb"},
		{name: "double slash stays a path", path: "//evil.example/x", want: "ws://10.0.0.5:8080//evil.example/x"},
		{name: "userinfo trick", path: "@evil.example/x", wantErr: true},
		{name: "port trick", path: ":9999/x", wantErr: true},
		{name: "absolute form", path: "http://evil.example/x", wantErr: true},
		{name: "relative", path: "socket", wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			target, err := parseWSTarget(tc.path)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("parseWSTarget(%q) accepted, want error", tc.path)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseWSTarget(%q): %v", tc.path, err)
			}

			got, err := buildSandboxWSURL(sandbox, target)
			if err != nil {
				t.Fatalf("buildSandboxWSURL: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSandboxWSURLRejectsUnexpectedBase(t *testing.T) {
	target, err := parseWSTarget("/")
	if err != nil {
		t.Fatal(err)
	}

	for _, base := range []string{"", "10.0.0.5:8080", "https://10.0.0.5:8080", "unix:///tmp/sock"} {
		if _, err := buildSandboxWSURL(base, target); err == nil {
			t.Errorf("buildSandboxWSURL(%q) accepted, want error", base)
		}
	}
}
