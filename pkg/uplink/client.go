package uplink

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand/v2"
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
	// outboxSize is deep enough that a tenant using SendContext to apply
	// backpressure does not immediately push the best-effort tenants over their
	// drop threshold. The two coexist in one queue: a blocked sender waits for
	// room, while Send discards as soon as there is none.
	outboxSize       = 256
	initialBackoff   = 1 * time.Second
	maxBackoff       = 60 * time.Second
	wsEndpoint       = "/api/v1/cluster-channel/ws"
	writeTimeout     = 10 * time.Second
	handshakeTimeout = 10 * time.Second

	// backoffJitter is the fraction of a delay that is randomized away.
	//
	// Clusters do not disconnect independently. The common cause is something
	// on the other end — a cloud deploy, a load balancer rotation — which drops
	// every cluster at the same instant. An undithered backoff then marches the
	// entire fleet back in lockstep, and since each one opens a snapshot on
	// connect, cloud takes the whole fleet's reconnect work as a single spike
	// rather than as a rate.
	//
	// Subtracting rather than adding matters: it keeps the delay bounded by the
	// backoff schedule instead of letting the worst case drift past maxBackoff.
	backoffJitter = 0.3

	// readLimit is the largest inbound message we accept. coder/websocket
	// defaults to 32 KiB, which is ample for coordination messages but far too
	// small for a tenant tunnelling bulk payloads through an envelope.
	//
	// The size is set by the worst case such a tenant can face rather than by
	// anything this package does: a payload it did not choose the framing for,
	// base64'd into JSON, which costs a third again. 2 MiB leaves room for a
	// peer framing at pkg/rpc's 1 MiB default and still keeps a bound on what an
	// untrusted sender can make us buffer.
	readLimit = 2 << 20
)

// Client maintains a persistent WebSocket connection to the cloud
// coordination service with automatic reconnection.
type Client struct {
	cloudURL   string
	authClient *cloudauth.AuthClient
	router     *MessageRouter
	log        *slog.Logger
	outbox     chan *Envelope

	mu           sync.Mutex
	onConnect    []func(ctx context.Context)
	onSession    []func(ctx context.Context, session Session)
	capabilities []CapabilityOffer
	session      *sessionConfig

	// NewClient enables this for production's pre-handshake path. Keeping it
	// explicit lets narrow package tests construct a Client without also having
	// to emulate the two legacy bootstrap exchanges.
	legacyBootstrap bool

	// getToken overrides auth token acquisition for testing.
	// When nil, authClient.GetToken is used.
	getToken func(ctx context.Context) (string, error)
}

type sessionConfig struct {
	runtimeVersion string
}

// ClientOption changes how a Client establishes its connections.
type ClientOption func(*Client)

// WithSession enables the negotiated session handshake. Callers should gate
// this while the protocol is experimental; cloud must support the handshake
// before a runtime starts requiring a welcome.
func WithSession(runtimeVersion string) ClientOption {
	return func(c *Client) {
		c.session = &sessionConfig{runtimeVersion: runtimeVersion}
	}
}

// NewClient creates a new WebSocket client.
//
// Without WithSession, the client preserves the legacy clock-sync and
// organization-lookup requests on every connection. With it, those link facts
// arrive in session.welcome instead. Tenants layer their own handlers,
// capability offers, and hooks on top.
func NewClient(cloudURL string, authClient *cloudauth.AuthClient, router *MessageRouter, log *slog.Logger, opts ...ClientOption) *Client {
	c := &Client{
		cloudURL:        cloudURL,
		authClient:      authClient,
		router:          router,
		log:             log,
		outbox:          make(chan *Envelope, outboxSize),
		legacyBootstrap: true,
	}
	for _, opt := range opts {
		opt(c)
	}

	router.Handle(TypeTimeResponse, c.handleTimeResponse)
	router.Handle(TypeOrgInfoResponse, c.handleOrgInfoResponse)

	return c
}

func (c *Client) sendLegacyInitialRequests() {
	c.log.Info("sending legacy initial requests")
	//nolint:errcheck // best-effort: a dropped request is retried next connect
	c.SendMessage(TypeTimeRequest, TimeRequest{
		ClientTransmitTime: time.Now().UTC(),
	})
	//nolint:errcheck // best-effort: a dropped request is retried next connect
	c.SendMessage(TypeOrgInfoRequest, struct{}{})
}

// handleTimeResponse computes the clock offset between this cluster and cloud
// and logs it.
//
// The offset is not retained, because nothing in the runtime reads it: cloud
// runs its own reconciliation against the timestamps a cluster reports, so a
// reporter sends raw wall clock and lets the other end do the math. Keeping an
// accessor here for a caller that doesn't exist would just be a claim about
// how the runtime works that isn't true. The exchange still earns its place by
// surfacing skew in the log, and by giving cloud its half of the round trip.
func (c *Client) handleTimeResponse(_ context.Context, data json.RawMessage) error {
	t4 := time.Now().UTC()

	var resp TimeResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return err
	}

	// Simplified NTP offset calculation:
	// offset = ((T2 - T1) + (T3 - T4)) / 2
	t1 := resp.ClientTransmitTime
	t2 := resp.ServerReceiveTime
	t3 := resp.ServerTransmitTime

	// A response missing any timestamp unmarshals cleanly into zero values, and
	// the arithmetic below would turn that into an offset of roughly the Unix
	// epoch and log it as a healthy sync. Since the whole reason this exchange
	// stays is the accuracy of what it logs, an incomplete payload has to be
	// visibly suspicious rather than quietly averaged.
	if t1.IsZero() || t2.IsZero() || t3.IsZero() {
		c.log.Warn("ignoring incomplete time response from cloud")
		return nil
	}

	offset := (t2.Sub(t1) + t3.Sub(t4)) / 2

	c.log.Info("clock sync complete", "offset", offset)
	return nil
}

// handleOrgInfoResponse logs the organization cloud says this cluster belongs
// to. Like the clock offset, it is not retained: cloud derives the org from its
// own records rather than from anything the cluster asserts, so the runtime has
// no use for a copy. It is worth logging because it confirms which tenant the
// link resolved to.
func (c *Client) handleOrgInfoResponse(_ context.Context, data json.RawMessage) error {
	var resp OrgInfoResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return err
	}

	if resp.OrganizationID == "" {
		c.log.Warn("ignoring org info response with no organization")
		return nil
	}

	c.log.Info("organization info received", "org_id", resp.OrganizationID)
	return nil
}

// OnConnect registers a callback invoked each time the link is ready. For a
// negotiated connection that means after session.welcome has been validated;
// for a legacy connection it means immediately after the WebSocket connects.
// The handler can use Send to queue messages for the new connection.
//
// Callbacks accumulate rather than replace, so each tenant can add its own.
// They run in registration order on the connection goroutine, so a callback
// that needs to do real work should hand off to its own goroutine.
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

// OfferCapability adds a protocol family to the next session hello. Offers
// are snapshotted for each connection, so tenants should register before Run.
func (c *Client) OfferCapability(offer CapabilityOffer) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, existing := range c.capabilities {
		if existing.Name == offer.Name {
			panic(fmt.Sprintf("uplink capability %q offered more than once", offer.Name))
		}
	}
	c.capabilities = append(c.capabilities, offer)
}

// OnSession registers a callback invoked after cloud's welcome has been
// validated. It is never called for a legacy connection. The context ends with
// the negotiated session, and the Session value does not change underneath the
// callback. Callbacks run in registration order before the read and write loops
// start, so any real work should be handed off to a goroutine.
func (c *Client) OnSession(fn func(ctx context.Context, session Session)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onSession = append(c.onSession, fn)
}

// connectCallbacks returns a snapshot of the registered callbacks so they
// can be invoked without holding the lock.
func (c *Client) connectCallbacks() []func(ctx context.Context) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.onConnect)
}

func (c *Client) sessionSnapshot() ([]CapabilityOffer, []func(context.Context, Session)) {
	c.mu.Lock()
	defer c.mu.Unlock()

	offers := make([]CapabilityOffer, len(c.capabilities))
	for i, offer := range c.capabilities {
		offers[i] = offer
		offers[i].Versions = slices.Clone(offer.Versions)
		offers[i].Offer = slices.Clone(offer.Offer)
	}
	return offers, slices.Clone(c.onSession)
}

// Handle registers a handler for an inbound message type, which is how a
// feature becomes a tenant of the link. Message types are namespaced per
// family (app.*, deploy.*) so the link stays a shared pipe rather than
// accumulating per-feature special cases.
func (c *Client) Handle(msgType string, handler MessageHandler) {
	c.router.Handle(msgType, handler)
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

// SendContext queues an envelope and waits for room rather than dropping, for
// a tenant that cannot treat loss as a self-healing condition. The best-effort
// Send above remains the right call for state a later report repairs; this one
// is for a stream whose gap nothing downstream can reconstruct.
//
// What it promises is narrow: the envelope reached the outbox of the connection
// live at the time, so it will be written unless that connection dies first. It
// is not an acknowledgement, and a reconnect discards whatever is still queued
// (see drainOutbox). A tenant that needs more than "delivered or the link
// broke" has to notice the break — which, for anything session-shaped, means
// tearing the session down and letting the caller retry.
func (c *Client) SendContext(ctx context.Context, env *Envelope) error {
	// Checked before the select for the same reason SendBlocking does it: with
	// outbox room and a dead context both cases are ready, and Go picks between
	// them at random. Here the stake is a relayed session — a sender that
	// straddles a reconnect could land an rpc.data frame from an ended session
	// on the *new* connection, which cloud may still route by session id. That
	// is precisely the resume-across-a-link-break this transport promises not
	// to do.
	if err := ctx.Err(); err != nil {
		return err
	}

	select {
	case c.outbox <- env:
		return nil
	case <-ctx.Done():
		return ctx.Err()
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

// SendBlocking queues an envelope, waiting for room instead of dropping when
// the outbox is full. It returns when the envelope is queued, or with the
// context's error if the connection goes away first.
//
// Send's drop-on-overflow is the right behavior for a single small message
// whose loss self-heals, but wrong for a stream. A tenant sending a bounded
// sequence — a snapshot in batches, a backfill walking a watermark — is
// pushing faster than one message per connect, and silent drops there don't
// read as "we lost a sample," they read as "that batch never existed." For a
// snapshot that is indistinguishable from apps having been deleted.
//
// Blocking makes the write loop's drain rate the natural throttle, which is
// also what lets two streaming tenants share the link without coordinating:
// neither can starve the other by filling the outbox, because filling it just
// slows the filler down. Callers must pass the connection-scoped context so a
// sender parked here is released when the connection drops rather than waking
// up to write into a socket that is gone.
func (c *Client) SendBlocking(ctx context.Context, env *Envelope) error {
	// Checked before the select rather than left to it. When the outbox has
	// room and the context is already dead, both cases below are ready and Go
	// picks between them at random, so a sender whose connection just went away
	// would queue an envelope about half the time. That envelope usually dies
	// in the next connect's drainOutbox, but a sender that was slow enough to
	// straddle the reconnect can land it on the *new* connection instead, which
	// for a snapshot means an abandoned epoch arriving intact and authorizing a
	// sweep against a stale picture. Making the contract deterministic is worth
	// more than the nanosecond, particularly for something other tenants build
	// on.
	if err := ctx.Err(); err != nil {
		return err
	}

	select {
	case c.outbox <- env:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// SendMessageBlocking marshals data and queues it, waiting for outbox room.
// See SendBlocking for when to prefer this over SendMessage.
func (c *Client) SendMessageBlocking(ctx context.Context, msgType string, data any) error {
	raw, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("marshal %s: %w", msgType, err)
	}
	return c.SendBlocking(ctx, &Envelope{Type: msgType, Data: raw})
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

		delay := jittered(backoff)

		c.log.Warn("websocket disconnected, reconnecting",
			"error", err, "backoff", delay)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}

		backoff = min(backoff*2, maxBackoff)
	}
}

// jittered spreads a delay over [(1-backoffJitter)·d, d] so a fleet knocked
// offline together does not come back together.
func jittered(d time.Duration) time.Duration {
	if d <= 0 {
		return d
	}
	return d - time.Duration(rand.Float64()*backoffJitter*float64(d))
}

// SpreadOnConnect returns a delay a tenant should wait before starting the
// work it does on each connect, so that work is spread across the fleet rather
// than landing as one spike.
//
// Exported rather than kept inside Run because jittering the reconnect alone
// only moves the spike: a fleet that comes back staggered but then has every
// cluster immediately stream a snapshot has changed when the pile-up happens,
// not whether it does. Tenants need the same treatment for their own connect
// work.
//
// The window is the caller's choice because it depends on what the work is
// worth delaying. Anything on the ephemeral tier can afford a generous one:
// nothing downstream distinguishes a snapshot that starts now from one that
// starts a minute from now.
func SpreadOnConnect(window time.Duration) time.Duration {
	if window <= 0 {
		return 0
	}
	return time.Duration(rand.Float64() * float64(window))
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

	if c.session != nil {
		session, callbacks, err := c.establishSession(ctx, conn)
		if err != nil {
			return fmt.Errorf("establish session: %w", err)
		}
		c.log.Info("session established",
			"session_id", session.ID,
			"organization_id", session.OrganizationID,
			"handshake_version", session.HandshakeVersion,
			"capabilities", len(session.Capabilities),
			"clock_offset", session.ClockOffset)
		for _, fn := range callbacks {
			fn(ctx, session)
		}
	} else if c.legacyBootstrap {
		c.sendLegacyInitialRequests()
	}

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

func (c *Client) establishSession(ctx context.Context, conn *websocket.Conn) (Session, []func(context.Context, Session), error) {
	offers, callbacks := c.sessionSnapshot()
	hello := SessionHello{
		HandshakeVersions: []uint{HandshakeVersion1},
		RuntimeVersion:    c.session.runtimeVersion,
		ClientTime:        time.Now().UTC(),
		Capabilities:      offers,
	}
	raw, err := json.Marshal(hello)
	if err != nil {
		return Session{}, nil, fmt.Errorf("marshal hello: %w", err)
	}

	handshakeCtx, cancel := context.WithTimeout(ctx, handshakeTimeout)
	defer cancel()
	if err := wsjson.Write(handshakeCtx, conn, &Envelope{Type: TypeSessionHello, Data: raw}); err != nil {
		return Session{}, nil, fmt.Errorf("write hello: %w", err)
	}

	var env Envelope
	if err := wsjson.Read(handshakeCtx, conn, &env); err != nil {
		return Session{}, nil, fmt.Errorf("read welcome: %w", err)
	}
	receivedAt := time.Now().UTC()

	switch env.Type {
	case TypeSessionReject:
		var reject SessionReject
		if err := json.Unmarshal(env.Data, &reject); err != nil {
			return Session{}, nil, fmt.Errorf("decode session rejection: %w", err)
		}
		return Session{}, nil, fmt.Errorf("cloud rejected session: %s", reject.Reason)
	case TypeSessionWelcome:
		var welcome SessionWelcome
		if err := json.Unmarshal(env.Data, &welcome); err != nil {
			return Session{}, nil, fmt.Errorf("decode welcome: %w", err)
		}
		session, err := validateWelcome(hello, welcome, receivedAt)
		if err != nil {
			return Session{}, nil, err
		}
		return session, callbacks, nil
	default:
		return Session{}, nil, fmt.Errorf("expected %s, received %s", TypeSessionWelcome, env.Type)
	}
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

	conn.SetReadLimit(readLimit)

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
