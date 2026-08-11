package anywhere

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"miren.dev/runtime/pkg/cloudauth"
)

const (
	outboxSize     = 64
	initialBackoff = 1 * time.Second
	maxBackoff     = 60 * time.Second
	wsEndpoint     = "/api/v1/cluster-channel/ws"
	writeTimeout   = 10 * time.Second
)

// Client maintains a persistent WebSocket connection to the cloud
// coordination service with automatic reconnection.
type Client struct {
	cloudURL   string
	authClient *cloudauth.AuthClient
	router     *MessageRouter
	log        *slog.Logger
	outbox     chan *Envelope

	mu        sync.Mutex
	onConnect []func(ctx context.Context)

	// getToken overrides auth token acquisition for testing.
	// When nil, authClient.GetToken is used.
	getToken func(ctx context.Context) (string, error)
}

// NewClient creates a new WebSocket client.
func NewClient(cloudURL string, authClient *cloudauth.AuthClient, router *MessageRouter, log *slog.Logger) *Client {
	return &Client{
		cloudURL:   cloudURL,
		authClient: authClient,
		router:     router,
		log:        log,
		outbox:     make(chan *Envelope, outboxSize),
	}
}

// OnConnect registers a callback invoked each time a WebSocket
// connection is established. The handler can use Send to queue
// messages for the new connection.
//
// Callbacks accumulate rather than replace: the connector registers its
// own for time sync and org info, and feature reporters add theirs on top.
// They run in registration order on the connection goroutine, so a
// callback that needs to do real work should hand off to its own.
//
// The context is scoped to the connection, so work handed off that way is
// cancelled when the connection drops. That is the right lifetime for it:
// anything still queued at that point is discarded on reconnect anyway, so
// finishing would only produce a message nobody sends.
func (c *Client) OnConnect(fn func(ctx context.Context)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onConnect = append(c.onConnect, fn)
}

// connectCallbacks returns a snapshot of the registered callbacks so they
// can be invoked without holding the lock.
func (c *Client) connectCallbacks() []func(ctx context.Context) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.onConnect)
}

// Send queues an envelope for delivery to the cloud. Non-blocking;
// drops the message if the outbox is full.
func (c *Client) Send(env *Envelope) {
	select {
	case c.outbox <- env:
	default:
		c.log.Warn("outbox full, dropping message", "type", env.Type)
	}
}

// SendMessage marshals data and queues it for delivery.
func (c *Client) SendMessage(msgType string, data any) error {
	raw, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("marshal %s: %w", msgType, err)
	}
	c.Send(&Envelope{Type: msgType, Data: raw})
	return nil
}

// Run maintains the WebSocket connection with reconnection. It blocks
// until the context is cancelled.
func (c *Client) Run(ctx context.Context) error {
	backoff := initialBackoff

	for {
		start := time.Now()
		err := c.runOnce(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}

		// If the connection was up for a while, reset backoff so the
		// next reconnect attempt starts fast.
		if time.Since(start) >= 30*time.Second {
			backoff = initialBackoff
		}

		c.log.Warn("websocket disconnected, reconnecting",
			"error", err, "backoff", backoff)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}

		backoff = min(backoff*2, maxBackoff)
	}
}

// runOnce connects and processes messages until an error occurs.
func (c *Client) runOnce(ctx context.Context) error {
	conn, err := c.connect(ctx)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer conn.CloseNow()

	// Derived before the callbacks run, not after, so what they receive is
	// scoped to this connection. A callback that hands off to a goroutine can
	// then be torn down when the connection drops, rather than outliving it
	// and leaving one straggler per reconnect.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	c.drainOutbox()

	for _, fn := range c.connectCallbacks() {
		fn(ctx)
	}

	errCh := make(chan error, 2)

	go func() { errCh <- c.readLoop(ctx, conn) }()
	go func() { errCh <- c.writeLoop(ctx, conn) }()

	err = <-errCh
	cancel()
	<-errCh
	return err
}

func (c *Client) connect(ctx context.Context) (*websocket.Conn, error) {
	var token string
	var err error

	if c.getToken != nil {
		token, err = c.getToken(ctx)
	} else {
		token, err = c.authClient.GetToken(ctx)
	}
	if err != nil {
		return nil, fmt.Errorf("get auth token: %w", err)
	}

	wsURL := c.wsURL()
	c.log.Info("connecting to cloud", "url", wsURL)

	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		HTTPHeader: http.Header{
			"Authorization": []string{"Bearer " + token},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", wsURL, err)
	}

	c.log.Info("connected to cloud")
	return conn, nil
}

func (c *Client) wsURL() string {
	base := strings.TrimRight(c.cloudURL, "/")
	scheme := "wss"
	if strings.HasPrefix(base, "http://") {
		scheme = "ws"
		base = strings.TrimPrefix(base, "http://")
	} else {
		base = strings.TrimPrefix(base, "https://")
	}
	return scheme + "://" + base + wsEndpoint
}

func (c *Client) readLoop(ctx context.Context, conn *websocket.Conn) error {
	for {
		var env Envelope
		if err := wsjson.Read(ctx, conn, &env); err != nil {
			return fmt.Errorf("read: %w", err)
		}

		if err := c.router.Dispatch(ctx, env); err != nil {
			c.log.Warn("dispatch error", "type", env.Type, "error", err)
		}
	}
}

func (c *Client) writeLoop(ctx context.Context, conn *websocket.Conn) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case env := <-c.outbox:
			writeCtx, cancel := context.WithTimeout(ctx, writeTimeout)
			err := wsjson.Write(writeCtx, conn, env)
			cancel()
			if err != nil {
				return fmt.Errorf("write: %w", err)
			}
		}
	}
}

func (c *Client) drainOutbox() {
	for {
		select {
		case <-c.outbox:
		default:
			return
		}
	}
}
