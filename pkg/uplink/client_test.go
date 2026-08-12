package uplink

import (
	"bytes"
	"context"
	"encoding/json"
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
