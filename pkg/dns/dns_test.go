package dns

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	mdns "github.com/miekg/dns"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	compute_v1alpha "miren.dev/runtime/api/compute/compute_v1alpha"
	core_v1alpha "miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/testutils"
	"miren.dev/runtime/pkg/entity/types"
	"miren.dev/runtime/pkg/slogfmt"
)

func newTestServer(t *testing.T) *Server {
	t.Helper()
	log := slog.New(slogfmt.NewTestHandler(t, &slog.HandlerOptions{Level: slog.LevelDebug}))
	return &Server{
		log:             log,
		ipToApp:         make(map[string]string),
		ipToSandbox:     make(map[string]string),
		ipToService:     make(map[string]string),
		appServiceToIPs: make(map[string]map[string][]string),
		sandboxes:       make(map[string]sandboxMapping),
	}
}

func TestIPReuseBetweenSandboxes(t *testing.T) {
	// This test covers a race condition where:
	// 1. Sandbox A is created at IP X
	// 2. Sandbox A dies, but entity cleanup is delayed (1 hour)
	// 3. IP X is reused by new Sandbox B (after 30-min cooldown)
	// 4. Sandbox A's entity is finally deleted
	// 5. The IP should still be resolvable because Sandbox B is using it

	s := newTestServer(t)

	const (
		sharedIP     = "10.8.24.19"
		appName      = "testapp"
		serviceName  = "web"
		oldSandboxID = "sandbox/testapp-web-OLD"
		newSandboxID = "sandbox/testapp-web-NEW"
	)

	// Old sandbox (now STOPPED) was registered at IP
	s.AddSandboxMapping(oldSandboxID, sharedIP, appName, serviceName)

	// New sandbox (RUNNING) gets the same IP after cooldown
	s.AddSandboxMapping(newSandboxID, sharedIP, appName, serviceName)

	// Old sandbox entity is finally deleted (1 hour cleanup delay)
	s.handleSandboxDeleteByID(oldSandboxID)

	// The IP should still resolve because new sandbox is using it
	assert.Equal(t, appName, s.lookupAppForIP(sharedIP),
		"IP should still resolve after old sandbox deletion because new sandbox is using it")
}

func TestDeleteSandboxCleansUpWhenNoReuse(t *testing.T) {
	// When a sandbox is deleted and no other entity uses the IP,
	// the mappings should be cleaned up.

	s := newTestServer(t)

	const (
		ip        = "10.8.24.20"
		appName   = "testapp"
		service   = "web"
		sandboxID = "sandbox/testapp-web-123"
	)

	s.AddSandboxMapping(sandboxID, ip, appName, service)
	assert.Equal(t, appName, s.lookupAppForIP(ip))

	s.handleSandboxDeleteByID(sandboxID)

	assert.Empty(t, s.lookupAppForIP(ip),
		"IP should not resolve after sandbox deletion when no other sandbox uses it")
}

func TestDeleteSandboxWithDifferentIPs(t *testing.T) {
	// Deleting one sandbox should not affect another sandbox with a different IP.

	s := newTestServer(t)

	const (
		ip1        = "10.8.24.20"
		ip2        = "10.8.24.21"
		appName    = "testapp"
		service    = "web"
		sandbox1ID = "sandbox/testapp-web-1"
		sandbox2ID = "sandbox/testapp-web-2"
	)

	s.AddSandboxMapping(sandbox1ID, ip1, appName, service)
	s.AddSandboxMapping(sandbox2ID, ip2, appName, service)

	s.handleSandboxDeleteByID(sandbox1ID)

	assert.Empty(t, s.lookupAppForIP(ip1), "ip1 should not resolve after deletion")
	assert.Equal(t, appName, s.lookupAppForIP(ip2), "ip2 should still resolve")
}

func TestSandboxStatusTransitionRemovesFromDNS(t *testing.T) {
	// This test verifies that when a sandbox transitions from RUNNING to
	// STOPPED or DEAD, it is immediately removed from DNS mappings.
	// Previously, UPDATE events for non-RUNNING sandboxes were ignored,
	// causing stale IPs to remain in DNS until the entity DELETE (up to 1 hour).

	s := newTestServer(t)
	ctx := context.Background()

	const (
		ip        = "10.8.24.30"
		appName   = "testapp"
		service   = "web"
		sandboxID = "sandbox/testapp-web-status-test"
	)

	// Simulate a sandbox that was previously RUNNING and tracked
	s.AddSandboxMapping(sandboxID, ip, appName, service)
	assert.Equal(t, appName, s.lookupAppForIP(ip), "sandbox should be tracked initially")

	// Sandbox transitions to STOPPED (e.g., process exited)
	stoppedSandbox := &compute_v1alpha.Sandbox{
		ID:     entity.Id(sandboxID),
		Status: compute_v1alpha.STOPPED,
	}
	s.handleSandboxUpdate(ctx, stoppedSandbox, nil)

	// The sandbox should be removed from DNS immediately
	assert.Empty(t, s.lookupAppForIP(ip),
		"sandbox should be removed from DNS when status changes to STOPPED")

	// Verify all mappings are cleaned up
	s.mu.RLock()
	_, hasEntityMapping := s.sandboxes[sandboxID]
	_, hasServiceMapping := s.ipToService[ip]
	s.mu.RUnlock()

	assert.False(t, hasEntityMapping, "the sandbox's claim should be removed")
	assert.False(t, hasServiceMapping, "ipToService mapping should be removed")
}

func TestSandboxDeadStatusRemovesFromDNS(t *testing.T) {
	// Same as above but for DEAD status

	s := newTestServer(t)
	ctx := context.Background()

	const (
		ip        = "10.8.24.31"
		appName   = "testapp"
		service   = "api"
		sandboxID = "sandbox/testapp-api-dead-test"
	)

	s.AddSandboxMapping(sandboxID, ip, appName, service)
	assert.Equal(t, appName, s.lookupAppForIP(ip))

	// Sandbox marked as DEAD (e.g., health check failed)
	deadSandbox := &compute_v1alpha.Sandbox{
		ID:     entity.Id(sandboxID),
		Status: compute_v1alpha.DEAD,
	}
	s.handleSandboxUpdate(ctx, deadSandbox, nil)

	assert.Empty(t, s.lookupAppForIP(ip),
		"sandbox should be removed from DNS when status changes to DEAD")
}

func TestResolveUnknownIPWithoutEntityClient(t *testing.T) {
	// resolveUnknownIP should return false gracefully when entityClient is nil
	s := newTestServer(t)
	assert.False(t, s.resolveUnknownIP("10.8.24.50"),
		"resolveUnknownIP should return false when entityClient is nil")
}

func TestResolveUnknownIPFindsAndRegistersSandbox(t *testing.T) {
	// When a sandbox container makes DNS queries before the entity watcher
	// processes the RUNNING status update, resolveUnknownIP should find
	// the sandbox in the entity store and register it for DNS.

	ctx := context.Background()

	inmem, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	const (
		sandboxIP   = "10.8.24.60"
		appName     = "myapp"
		serviceName = "web"
		versionName = "myapp-v1"
		sandboxName = "myapp-web-1"
	)

	// Create app entity
	app := &core_v1alpha.App{}
	appID, err := inmem.Client.Create(ctx, appName, app)
	assert.NoError(t, err)

	// Create app version entity referencing the app
	appVer := &core_v1alpha.AppVersion{App: appID}
	_, err = inmem.Client.Create(ctx, versionName, appVer)
	assert.NoError(t, err)

	// Create sandbox entity in PENDING status with network info and service label.
	// The sandbox is PENDING (not RUNNING) to test that resolveUnknownIP
	// works regardless of status — the container is clearly running if it's
	// making DNS queries.
	sb := &compute_v1alpha.Sandbox{
		Status: compute_v1alpha.PENDING,
		Network: []compute_v1alpha.Network{
			{Address: sandboxIP + "/24"},
		},
		Spec: compute_v1alpha.SandboxSpec{
			Version: entity.Id("app_version/" + versionName),
		},
	}
	_, err = inmem.Client.Create(ctx, sandboxName, sb,
		entityserver.WithLabels(types.Labels{
			{Key: "service", Value: serviceName},
		}),
	)
	assert.NoError(t, err)

	// Create DNS server with the entity client
	log := slog.New(slogfmt.NewTestHandler(t, &slog.HandlerOptions{Level: slog.LevelDebug}))
	s := &Server{
		log:             log,
		entityClient:    inmem.EAC,
		ipToApp:         make(map[string]string),
		ipToSandbox:     make(map[string]string),
		ipToService:     make(map[string]string),
		appServiceToIPs: make(map[string]map[string][]string),
		sandboxes:       make(map[string]sandboxMapping),
	}

	// IP should not be registered yet
	assert.Empty(t, s.lookupAppForIP(sandboxIP))

	// resolveUnknownIP should find the sandbox and register it
	assert.True(t, s.resolveUnknownIP(sandboxIP),
		"resolveUnknownIP should find and register the sandbox")

	// Verify the sandbox is now registered
	assert.Equal(t, appName, s.lookupAppForIP(sandboxIP),
		"sandbox should be registered with correct app name")

	// Verify service mapping
	s.mu.RLock()
	assert.Equal(t, serviceName, s.ipToService[sandboxIP],
		"service mapping should be registered")
	assert.Contains(t, s.appServiceToIPs[appName][serviceName], sandboxIP,
		"app+service to IP mapping should be registered")
	s.mu.RUnlock()
}

func TestResolveUnknownIPNoMatchingIP(t *testing.T) {
	// resolveUnknownIP should return false when no sandbox has the queried IP.

	ctx := context.Background()

	inmem, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	// Create a sandbox with a different IP
	sb := &compute_v1alpha.Sandbox{
		Status: compute_v1alpha.RUNNING,
		Network: []compute_v1alpha.Network{
			{Address: "10.8.24.99/24"},
		},
	}
	_, err := inmem.Client.Create(ctx, "other-sandbox", sb,
		entityserver.WithLabels(types.Labels{
			{Key: "service", Value: "web"},
		}),
	)
	assert.NoError(t, err)

	log := slog.New(slogfmt.NewTestHandler(t, &slog.HandlerOptions{Level: slog.LevelDebug}))
	s := &Server{
		log:             log,
		entityClient:    inmem.EAC,
		ipToApp:         make(map[string]string),
		ipToSandbox:     make(map[string]string),
		ipToService:     make(map[string]string),
		appServiceToIPs: make(map[string]map[string][]string),
		sandboxes:       make(map[string]sandboxMapping),
	}

	assert.False(t, s.resolveUnknownIP("10.8.24.100"),
		"resolveUnknownIP should return false for non-matching IP")
}

func TestStoppedStatusNeverAdded(t *testing.T) {
	// Sandboxes that are STOPPED or DEAD should never be added to DNS,
	// even if they have network info.

	s := newTestServer(t)
	ctx := context.Background()

	const (
		ip        = "10.8.24.32"
		sandboxID = "sandbox/testapp-stopped"
	)

	// A STOPPED sandbox with network info should not be tracked
	stoppedSandbox := &compute_v1alpha.Sandbox{
		ID:     entity.Id(sandboxID),
		Status: compute_v1alpha.STOPPED,
		Network: []compute_v1alpha.Network{
			{Address: ip + "/24"},
		},
	}
	s.handleSandboxUpdate(ctx, stoppedSandbox, nil)

	assert.Empty(t, s.lookupAppForIP(ip),
		"STOPPED sandbox should not be added to DNS")
}

func TestAppDiscovery(t *testing.T) {
	s := newTestServer(t)
	s.AddSandboxMapping("sandbox/caller-web", "10.8.0.2", "caller", "web")
	s.AddSandboxMapping("sandbox/target-web-1", "10.8.0.3", "other-app", "web")
	s.AddSandboxMapping("sandbox/target-web-2", "10.8.0.4", "other-app", "web")
	s.AddSandboxMapping("sandbox/target-db", "10.8.0.5", "other-app", "db")

	for _, tt := range []struct {
		name string
		ips  []string
	}{
		{"web.app.miren.", []string{"10.8.0.2"}},
		{"web.caller.app.miren.", []string{"10.8.0.2"}},
		{"web.other-app.app.miren.", []string{"10.8.0.3", "10.8.0.4"}},
		{"WEB.OTHER-APP.APP.MIREN.", []string{"10.8.0.3", "10.8.0.4"}},
		{"db.other-app.app.miren.", []string{"10.8.0.5"}},
		{"db.app.miren.", nil},
		{"web.missing.app.miren.", nil},
		{"missing.other-app.app.miren.", nil},
		{"web.other-app.extra.app.miren.", nil},
		{"web..app.miren.", nil},
		{".other-app.app.miren.", nil},
		{".app.miren.", nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			for _, qtype := range []uint16{mdns.TypeA, mdns.TypeAAAA} {
				for _, source := range []string{"10.8.0.2", "10.8.0.99"} {
					request := new(mdns.Msg)
					request.SetQuestion(tt.name, qtype)
					w := &captureResponseWriter{remote: &net.UDPAddr{IP: net.ParseIP(source)}}
					s.handleRequest(w, request)
					require.NotNil(t, w.message)
					assert.Equal(t, mdns.RcodeSuccess, w.message.Rcode)
					assert.True(t, w.message.Authoritative)
					var ips []string
					for _, rr := range w.message.Answer {
						a, ok := rr.(*mdns.A)
						require.True(t, ok)
						assert.Equal(t, tt.name, a.Hdr.Name)
						ips = append(ips, a.A.String())
					}
					if qtype == mdns.TypeAAAA || source == "10.8.0.99" {
						assert.Empty(t, ips, "IPv6 remains unsupported; unknown callers cannot discover apps")
					} else {
						assert.ElementsMatch(t, tt.ips, ips)
					}
				}
			}
		})
	}

	t.Run("TXT service discovery", func(t *testing.T) {
		for _, tt := range []struct {
			name string
			text string
		}{
			{"app.miren.", "web"},
			{"caller.app.miren.", "web"},
			{"other-app.app.miren.", "db web"},
			{"OTHER-APP.APP.MIREN.", "db web"},
			{"missing.app.miren.", ""},
			{"web.other-app.app.miren.", ""},
			{"other-app.extra.app.miren.", ""},
			{"other-app..app.miren.", ""},
			{".app.miren.", ""},
		} {
			t.Run(tt.name, func(t *testing.T) {
				for _, source := range []string{"10.8.0.2", "10.8.0.99"} {
					request := new(mdns.Msg)
					request.SetQuestion(tt.name, mdns.TypeTXT)
					w := &captureResponseWriter{remote: &net.UDPAddr{IP: net.ParseIP(source)}}
					s.handleRequest(w, request)
					require.NotNil(t, w.message)
					assert.Equal(t, mdns.RcodeSuccess, w.message.Rcode)
					assert.True(t, w.message.Authoritative)
					if tt.text == "" || source == "10.8.0.99" {
						assert.Empty(t, w.message.Answer)
						continue
					}
					require.Len(t, w.message.Answer, 1)
					rr, ok := w.message.Answer[0].(*mdns.TXT)
					require.True(t, ok)
					assert.Equal(t, tt.name, rr.Hdr.Name)
					assert.Equal(t, []string{tt.text}, rr.Txt)
				}
			})
		}
	})

	t.Run("same-app service listing and reverse DNS", func(t *testing.T) {
		for _, tt := range []struct {
			name   string
			qtype  uint16
			answer string
		}{
			{"app.miren.", mdns.TypeTXT, "web"},
			{"2.0.8.10.in-addr.arpa.", mdns.TypePTR, "web.app.miren."},
		} {
			request := new(mdns.Msg)
			request.SetQuestion(tt.name, tt.qtype)
			w := &captureResponseWriter{remote: &net.TCPAddr{IP: net.ParseIP("10.8.0.2")}}
			s.handleRequest(w, request)
			require.Len(t, w.message.Answer, 1)
			switch rr := w.message.Answer[0].(type) {
			case *mdns.TXT:
				assert.Equal(t, []string{tt.answer}, rr.Txt)
			case *mdns.PTR:
				assert.Equal(t, tt.answer, rr.Ptr)
			default:
				t.Fatalf("unexpected answer: %v", rr)
			}
		}
	})
}

func TestCrossAppDNSConnection(t *testing.T) {
	// Distinct loopback addresses exercise real DNS transports and direct TCP
	// connections without requiring a deployed cluster or privileged networking.
	targetListener, err := net.Listen("tcp", "127.0.0.2:0")
	require.NoError(t, err)
	target := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		assert.NoError(t, err)
		assert.Equal(t, "127.0.0.3", host)
		io.WriteString(w, "other-app/web")
	}))
	target.Listener.Close()
	target.Listener = targetListener
	target.Start()
	t.Cleanup(target.Close)

	s := newServer("127.0.0.1:0", nil, nil, slog.Default())
	s.AddSandboxMapping("sandbox/caller", "127.0.0.3", "caller", "web")
	s.AddSandboxMapping("sandbox/target", "127.0.0.2", "other-app", "web")
	s.AddSandboxMapping("sandbox/target-db", "127.0.0.4", "other-app", "db")
	errCh := make(chan error, 1)
	go func() { errCh <- s.ListenAndServe() }()
	select {
	case <-s.ready:
	case err := <-errCh:
		t.Fatalf("DNS startup failed: %v", err)
	case <-time.After(2 * time.Second):
		s.Shutdown()
		t.Fatal("DNS listeners did not start")
	}
	t.Cleanup(func() {
		require.NoError(t, s.Shutdown())
		require.NoError(t, <-errCh)
	})

	for _, network := range []string{"udp", "tcp"} {
		t.Run(network, func(t *testing.T) {
			var source net.Addr = &net.UDPAddr{IP: net.ParseIP("127.0.0.3")}
			if network == "tcp" {
				source = &net.TCPAddr{IP: net.ParseIP("127.0.0.3")}
			}
			client := &mdns.Client{Net: network, Timeout: time.Second, Dialer: &net.Dialer{LocalAddr: source}}
			listQuery := new(mdns.Msg)
			listQuery.SetQuestion("other-app.app.miren.", mdns.TypeTXT)
			list, _, err := client.Exchange(listQuery, s.boundAddr)
			require.NoError(t, err)
			require.Len(t, list.Answer, 1)
			assert.Equal(t, []string{"db web"}, list.Answer[0].(*mdns.TXT).Txt)

			response, _, err := client.Exchange(query("web.other-app.app.miren."), s.boundAddr)
			require.NoError(t, err)
			require.Len(t, response.Answer, 1)
			ip := response.Answer[0].(*mdns.A).A.String()
			require.Equal(t, "127.0.0.2", ip)

			dialer := &net.Dialer{LocalAddr: &net.TCPAddr{IP: net.ParseIP("127.0.0.3")}, Timeout: time.Second}
			transport := &http.Transport{DialContext: dialer.DialContext}
			defer transport.CloseIdleConnections()
			httpClient := &http.Client{Transport: transport, Timeout: time.Second}
			port := targetListener.Addr().(*net.TCPAddr).Port
			resp, err := httpClient.Get(fmt.Sprintf("http://%s:%d/", ip, port))
			require.NoError(t, err)
			defer resp.Body.Close()
			body, err := io.ReadAll(resp.Body)
			require.NoError(t, err)
			assert.Equal(t, "other-app/web", string(body))
			t.Logf("%s DNS selected %s; direct connection on listener port %d returned %q", network, ip, port, body)
		})
	}
}
