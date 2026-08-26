package uplink

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

// newTestClient creates a Client that uses a fixed token instead of
// a real AuthClient.
func newTestClient(cloudURL, token string, router *MessageRouter) *Client {
	c := &Client{
		cloudURL: cloudURL,
		router:   router,
		log:      slog.Default(),
		outbox:   make(chan *Envelope, outboxSize),
		getToken: func(ctx context.Context) (string, error) {
			return token, nil
		},
	}
	return c
}

func TestClientConnectsAndReceives(t *testing.T) {
	received := make(chan Envelope, 1)
	router := NewMessageRouter()
	router.Handle("test.message", func(ctx context.Context, data json.RawMessage) error {
		received <- Envelope{Type: "test.message", Data: data}
		return nil
	})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer test-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()

		env := Envelope{
			Type: "test.message",
			Data: json.RawMessage(`{"key":"value"}`),
		}
		if err := wsjson.Write(r.Context(), conn, &env); err != nil {
			return
		}

		time.Sleep(100 * time.Millisecond)
		conn.Close(websocket.StatusNormalClosure, "done")
	}))
	defer srv.Close()

	client := newTestClient(srv.URL, "test-token", router)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go client.Run(ctx)

	select {
	case env := <-received:
		if env.Type != "test.message" {
			t.Errorf("expected type test.message, got %s", env.Type)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for message")
	}
}

func TestClientSendsMessages(t *testing.T) {
	serverReceived := make(chan Envelope, 1)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()

		var env Envelope
		if err := wsjson.Read(r.Context(), conn, &env); err != nil {
			return
		}
		serverReceived <- env

		time.Sleep(100 * time.Millisecond)
		conn.Close(websocket.StatusNormalClosure, "done")
	}))
	defer srv.Close()

	router := NewMessageRouter()
	client := newTestClient(srv.URL, "test-token", router)

	client.OnConnect(func(ctx context.Context) {
		client.SendMessage("hello", map[string]string{"from": "cluster"})
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go client.Run(ctx)

	select {
	case env := <-serverReceived:
		if env.Type != "hello" {
			t.Errorf("expected type hello, got %s", env.Type)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for server to receive message")
	}
}

func TestClientReconnects(t *testing.T) {
	var mu sync.Mutex
	connectCount := 0

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}

		mu.Lock()
		connectCount++
		count := connectCount
		mu.Unlock()

		if count == 1 {
			conn.Close(websocket.StatusGoingAway, "bye")
			return
		}

		defer conn.CloseNow()
		<-r.Context().Done()
	}))
	defer srv.Close()

	router := NewMessageRouter()
	client := newTestClient(srv.URL, "test-token", router)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go client.Run(ctx)

	deadline := time.After(4 * time.Second)
	for {
		mu.Lock()
		c := connectCount
		mu.Unlock()
		if c >= 2 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("timed out waiting for reconnection")
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func TestWSURL(t *testing.T) {
	tests := []struct {
		cloudURL string
		want     string
	}{
		{"https://api.miren.cloud", "wss://api.miren.cloud/api/v1/cluster-channel/ws"},
		{"http://localhost:3001", "ws://localhost:3001/api/v1/cluster-channel/ws"},
		{"https://api.miren.cloud/", "wss://api.miren.cloud/api/v1/cluster-channel/ws"},
	}

	for _, tt := range tests {
		c := &Client{cloudURL: tt.cloudURL}
		got := c.wsURL()
		if got != tt.want {
			t.Errorf("wsURL(%q) = %q, want %q", tt.cloudURL, got, tt.want)
		}
	}
}

func TestOnConnectCalled(t *testing.T) {
	called := make(chan struct{}, 1)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()

		var env Envelope
		wsjson.Read(r.Context(), conn, &env)
		time.Sleep(50 * time.Millisecond)
		conn.Close(websocket.StatusNormalClosure, "done")
	}))
	defer srv.Close()

	router := NewMessageRouter()
	client := newTestClient(srv.URL, "test-token", router)
	client.OnConnect(func(ctx context.Context) {
		select {
		case called <- struct{}{}:
		default:
		}
		client.SendMessage("ping", nil)
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go client.Run(ctx)

	select {
	case <-called:
	case <-ctx.Done():
		t.Fatal("onConnect was not called")
	}
}

// TestOnConnectCallbacksAccumulate guards the fan-out. Registering a second
// callback used to replace the first, which meant a feature reporter
// registering one would silently disable the connector's own time sync and
// org info rather than failing visibly. Both must run, in registration order.
func TestOnConnectCallbacksAccumulate(t *testing.T) {
	var mu sync.Mutex
	var order []string

	done := make(chan struct{})
	var once sync.Once

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()

		time.Sleep(50 * time.Millisecond)
		conn.Close(websocket.StatusNormalClosure, "done")
	}))
	defer srv.Close()

	router := NewMessageRouter()
	client := newTestClient(srv.URL, "test-token", router)

	record := func(name string) func(context.Context) {
		return func(context.Context) {
			mu.Lock()
			order = append(order, name)
			finished := len(order) >= 3
			mu.Unlock()

			if finished {
				once.Do(func() { close(done) })
			}
		}
	}

	client.OnConnect(record("first"))
	client.OnConnect(record("second"))
	client.OnConnect(record("third"))

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go client.Run(ctx) //nolint:errcheck

	select {
	case <-done:
	case <-ctx.Done():
		mu.Lock()
		got := slices.Clone(order)
		mu.Unlock()
		t.Fatalf("not all onConnect callbacks ran, got %v", got)
	}

	mu.Lock()
	defer mu.Unlock()
	if diff := order[:3]; !slices.Equal(diff, []string{"first", "second", "third"}) {
		t.Fatalf("callbacks ran out of registration order: %v", diff)
	}
}

// TestOnConnectContextCancelledOnDisconnect pins the lifetime of the context
// handed to connect callbacks. A callback that hands work off to a goroutine
// relies on this to tear that work down when the connection drops; without it
// the work would hold the long-lived Run context and a flapping connection
// would accumulate one straggler per reconnect.
func TestOnConnectContextCancelledOnDisconnect(t *testing.T) {
	cancelled := make(chan struct{})
	var once sync.Once

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()

		// Drop the connection out from under the callback.
		time.Sleep(50 * time.Millisecond)
		conn.Close(websocket.StatusNormalClosure, "done")
	}))
	defer srv.Close()

	router := NewMessageRouter()
	client := newTestClient(srv.URL, "test-token", router)

	client.OnConnect(func(ctx context.Context) {
		go func() {
			<-ctx.Done()
			once.Do(func() { close(cancelled) })
		}()
	})

	// The Run context deliberately outlives the assertion deadline. If the
	// callback were handed the Run context instead of a connection-scoped one,
	// waiting on it would block past the deadline rather than racing it, so
	// this fails rather than flaking when the scoping regresses.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	go client.Run(ctx) //nolint:errcheck

	select {
	case <-cancelled:
	case <-time.After(3 * time.Second):
		t.Fatal("callback context outlived the connection")
	}
}

// A streaming tenant needs backpressure, not the drop Send does. Losing one
// batch of a snapshot is not a lost sample the next one repairs — it looks
// exactly like the apps in that batch no longer existing, which is what makes
// silent drops dangerous for anything sent as a sequence.
func TestSendBlockingWaitsForRoomInsteadOfDropping(t *testing.T) {
	c := &Client{
		log:    slog.Default(),
		outbox: make(chan *Envelope, 1),
	}

	c.Send(&Envelope{Type: "first"})

	// Send would drop this one on the floor and report nothing.
	queued := make(chan error, 1)
	go func() {
		queued <- c.SendBlocking(t.Context(), &Envelope{Type: "second"})
	}()

	select {
	case <-queued:
		t.Fatal("SendBlocking returned while the outbox was full")
	case <-time.After(50 * time.Millisecond):
	}

	if got := <-c.outbox; got.Type != "first" {
		t.Fatalf("expected first out of the outbox, got %s", got.Type)
	}

	select {
	case err := <-queued:
		if err != nil {
			t.Fatalf("SendBlocking: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("SendBlocking stayed parked after the outbox drained")
	}

	if got := <-c.outbox; got.Type != "second" {
		t.Fatalf("expected second out of the outbox, got %s", got.Type)
	}
}

// The counterpart risk to blocking: a sender parked on a full outbox when the
// connection dies must be released rather than held until the next drain. The
// connection-scoped context is what does that, so a caller passing the wrong
// one leaks a goroutine per reconnect.
func TestSendBlockingReleasesOnContextCancel(t *testing.T) {
	c := &Client{
		log:    slog.Default(),
		outbox: make(chan *Envelope, 1),
	}

	c.Send(&Envelope{Type: "filler"})

	ctx, cancel := context.WithCancel(context.Background())

	released := make(chan error, 1)
	go func() {
		released <- c.SendBlocking(ctx, &Envelope{Type: "parked"})
	}()

	// Give the sender a moment to actually park before pulling it back out.
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-released:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("SendBlocking outlived its connection context")
	}
}

// Clusters do not disconnect independently — a cloud deploy drops all of them
// at once — so an undithered backoff marches the whole fleet back in lockstep
// and hands cloud the fleet's reconnect work as one spike. What matters is
// that the delays actually spread, not that any single one is a given value.
func TestBackoffJitterSpreadsReconnects(t *testing.T) {
	const base = 10 * time.Second

	buckets := map[int]bool{}
	for range 500 {
		got := jittered(base)

		if got > base {
			t.Fatalf("jittered(%v) = %v, must not exceed the backoff schedule", base, got)
		}
		if min := time.Duration(float64(base) * (1 - backoffJitter)); got < min {
			t.Fatalf("jittered(%v) = %v, below the %v floor", base, got, min)
		}

		buckets[int(got/(base/20))] = true
	}

	// A constant would land in one bucket; a healthy spread covers most of them.
	if len(buckets) < 5 {
		t.Errorf("reconnect delays clustered into %d buckets, expected them spread", len(buckets))
	}
}

// Jittering the reconnect alone only moves the spike, because each tenant
// starts streaming the moment it connects. The spread has to reach that work
// too, which is what SpreadOnConnect is for.
func TestSpreadOnConnectScattersAcrossTheWindow(t *testing.T) {
	const window = time.Minute

	buckets := map[int]bool{}
	for range 500 {
		got := SpreadOnConnect(window)

		if got < 0 || got >= window {
			t.Fatalf("SpreadOnConnect(%v) = %v, outside the window", window, got)
		}
		buckets[int(got/(window/20))] = true
	}

	if len(buckets) < 15 {
		t.Errorf("connect delays covered %d of 20 buckets, expected a wide scatter", len(buckets))
	}
}

// A zero or negative window means the caller wants no spreading, which has to
// mean "go now" rather than panicking or blocking forever.
func TestSpreadOnConnectHandlesNoWindow(t *testing.T) {
	if got := SpreadOnConnect(0); got != 0 {
		t.Errorf("SpreadOnConnect(0) = %v, want 0", got)
	}
	if got := SpreadOnConnect(-time.Second); got != 0 {
		t.Errorf("SpreadOnConnect(-1s) = %v, want 0", got)
	}
	if got := jittered(0); got != 0 {
		t.Errorf("jittered(0) = %v, want 0", got)
	}
}

// With outbox room available and a dead context, both select cases are ready
// and Go chooses at random, so this has to be decided before the select or it
// is right about half the time. A sender that slips an envelope through after
// its connection died can have it land on the next connection instead, which
// for a snapshot means an abandoned epoch arriving whole and authorizing a
// sweep against stale state.
func TestSendBlockingRefusesADeadContextEvenWithRoom(t *testing.T) {
	c := &Client{
		log:    slog.Default(),
		outbox: make(chan *Envelope, outboxSize),
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Repeated because the bug is probabilistic: one attempt passes half the
	// time even with the check missing.
	for range 200 {
		if err := c.SendBlocking(ctx, &Envelope{Type: "should-not-land"}); !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled, got %v", err)
		}
	}

	if got := len(c.outbox); got != 0 {
		t.Errorf("%d envelopes queued on a cancelled context, expected none", got)
	}
}

// SendContext carries relayed RPC frames, where landing one late is worse than
// dropping it: a frame from an ended session that reaches the outbox after a
// reconnect goes out on the new connection, and cloud may still route it by
// session id. That is the resume-across-a-link-break the transport promises not
// to do, so the cancellation has to win deterministically.
func TestSendContextRefusesADeadContextEvenWithRoom(t *testing.T) {
	c := &Client{
		log:    slog.Default(),
		outbox: make(chan *Envelope, outboxSize),
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Repeated for the same reason as the SendBlocking case above: with both
	// select cases ready, one attempt passes half the time even unfixed.
	for range 200 {
		if err := c.SendContext(ctx, &Envelope{Type: "should-not-land"}); !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled, got %v", err)
		}
	}

	if got := len(c.outbox); got != 0 {
		t.Errorf("%d envelopes queued on a cancelled context, expected none", got)
	}
}

// A response that unmarshals cleanly but carries no timestamps must not be
// averaged into a nonsense offset and logged as a healthy sync. Same for an
// org response naming no organization.
func TestControlPlaneHandlersRejectEmptyPayloads(t *testing.T) {
	var buf bytes.Buffer
	c := &Client{
		log:    slog.New(slog.NewTextHandler(&buf, nil)),
		outbox: make(chan *Envelope, outboxSize),
	}

	if err := c.handleTimeResponse(t.Context(), json.RawMessage(`{}`)); err != nil {
		t.Fatalf("handleTimeResponse: %v", err)
	}
	if err := c.handleOrgInfoResponse(t.Context(), json.RawMessage(`{}`)); err != nil {
		t.Fatalf("handleOrgInfoResponse: %v", err)
	}

	out := buf.String()
	if strings.Contains(out, "clock sync complete") {
		t.Error("reported a clock sync from a response with no timestamps")
	}
	if strings.Contains(out, "organization info received") {
		t.Error("reported an organization from a response naming none")
	}
	if got := strings.Count(out, "level=WARN"); got != 2 {
		t.Errorf("expected both incomplete payloads to warn, got %d warnings in:\n%s", got, out)
	}
}

// SendContext waits for room instead of discarding, which is the whole
// difference between it and Send. Both halves matter: it must block while the
// outbox is full, and it must come back as soon as space appears.
func TestSendContextWaitsForRoom(t *testing.T) {
	c := &Client{
		log:    slog.Default(),
		outbox: make(chan *Envelope, 1),
	}

	if err := c.SendContext(t.Context(), &Envelope{Type: "first"}); err != nil {
		t.Fatalf("first send: %v", err)
	}

	full, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	if err := c.SendContext(full, &Envelope{Type: "second"}); err == nil {
		t.Fatal("SendContext returned on a full outbox instead of waiting")
	}

	sent := make(chan error, 1)
	go func() { sent <- c.SendContext(t.Context(), &Envelope{Type: "second"}) }()

	if env := <-c.outbox; env.Type != "first" {
		t.Fatalf("expected the first envelope to be queued, got %q", env.Type)
	}

	select {
	case err := <-sent:
		if err != nil {
			t.Fatalf("second send: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("SendContext did not proceed once the outbox drained")
	}
}

// Send stays lossy on a full outbox. SendContext exists precisely because that
// contract is wrong for some tenants, so it is worth pinning that adding one
// did not quietly change the other.
func TestSendStillDropsWhenFull(t *testing.T) {
	c := &Client{
		log:    slog.Default(),
		outbox: make(chan *Envelope, 1),
	}

	c.Send(&Envelope{Type: "first"})
	c.Send(&Envelope{Type: "dropped"})

	if env := <-c.outbox; env.Type != "first" {
		t.Fatalf("expected the first envelope to survive, got %q", env.Type)
	}
	select {
	case env := <-c.outbox:
		t.Fatalf("expected the second envelope to be dropped, got %q", env.Type)
	default:
	}
}

// coder/websocket caps reads at 32 KiB unless told otherwise, which would make
// a tenant tunnelling bulk payloads fail at a size nothing in this package
// names. The ceiling is ours, so prove a message well past the default arrives.
func TestClientAcceptsLargeMessages(t *testing.T) {
	const payload = 64 * 1024

	received := make(chan int, 1)
	router := NewMessageRouter()
	router.Handle("test.large", func(_ context.Context, data json.RawMessage) error {
		received <- len(data)
		return nil
	})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()

		body, err := json.Marshal(strings.Repeat("x", payload))
		if err != nil {
			return
		}
		if err := wsjson.Write(r.Context(), conn, &Envelope{Type: "test.large", Data: body}); err != nil {
			return
		}

		<-r.Context().Done()
	}))
	defer srv.Close()

	client := newTestClient(srv.URL, "test-token", router)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go client.Run(ctx) //nolint:errcheck

	select {
	case n := <-received:
		if n < payload {
			t.Fatalf("payload arrived truncated: got %d bytes, want at least %d", n, payload)
		}
	case <-ctx.Done():
		t.Fatal("large message never arrived; the read limit is probably still the default")
	}
}
