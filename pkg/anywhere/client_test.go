package anywhere

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
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
