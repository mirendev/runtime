package rpc

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/fxamacker/cbor/v2"
	"go.opentelemetry.io/otel/propagation"
	"miren.dev/runtime/pkg/cond"
)

// msgOpTransport drives the client side of a message transport. Every call —
// unary, control-plane, or callstream — is multiplexed over the single session
// wrapping the connection, because a message transport may be handed a
// connection it does not own and cannot re-dial. Server-initiated callbacks are
// therefore routed by OID through a session-scoped registry rather than by
// which call happens to own the session.
type msgOpTransport struct {
	// owner is the client that established the connection; it supplies the
	// State used by the session's accept loop.
	owner *NetworkClient

	// dial opens a fresh connection. It is nil when the session was adopted
	// from a caller that owns the underlying connection.
	dial func() (MessageConn, error)

	inline inlineRegistry

	mu       sync.Mutex
	ctrlSess rpcSession
}

func (t *msgOpTransport) controlSession(ctx context.Context) (rpcSession, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.ctrlSess != nil {
		return t.ctrlSess, nil
	}
	if t.dial == nil {
		return nil, errors.New("rpc: message session closed and not redialable")
	}
	conn, err := t.dial()
	if err != nil {
		return nil, err
	}
	t.ctrlSess = newMsgSession(conn, true, 0)
	// A connection this transport dialed lives as long as the State does.
	t.startAcceptLoop(t.owner.State.top, t.ctrlSess)
	return t.ctrlSess, nil
}

// adopt installs a session over a connection the caller owns, bounding it by the
// caller's context rather than by any single call.
func (t *msgOpTransport) adopt(ctx context.Context, sess rpcSession) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.ctrlSess = sess
	t.startAcceptLoop(ctx, sess)
}

// startAcceptLoop serves inbound streams for the lifetime of the session. The
// session both routes callbacks against capabilities we advertised and serves
// operations against objects this State exposes, so the peer can call back into
// us over the connection we opened.
// Callers must hold t.mu.
func (t *msgOpTransport) startAcceptLoop(ctx context.Context, sess rpcSession) {
	st := t.owner.State
	router := &sessionRouter{state: st, server: st.server, inline: &t.inline}
	go router.run(ctx, sess) //nolint:errcheck // loop ends with the session
}

// roundTrip performs one request/reply op over the shared control session: open
// a stream, write the opRequest (and optional payload), read the opReply. The
// returned decoder is positioned to read the reply payload; done() closes the
// stream and must be called.
func (t *msgOpTransport) roundTrip(ctx context.Context, req opRequest, payload any) (opReply, *cbor.Decoder, func(), error) {
	sess, err := t.controlSession(ctx)
	if err != nil {
		return opReply{}, nil, nil, err
	}

	st, err := sess.OpenStreamSync(ctx)
	if err != nil {
		return opReply{}, nil, nil, err
	}
	done := func() { _ = st.Close() }

	req.Version = currentProtocolVersion

	enc := cbor.NewEncoder(st)
	if err := enc.Encode(req); err != nil {
		done()
		return opReply{}, nil, nil, err
	}
	if payload != nil {
		if err := enc.Encode(payload); err != nil {
			done()
			return opReply{}, nil, nil, err
		}
	}

	dec := cbor.NewDecoder(st)
	var reply opReply
	if err := dec.Decode(&reply); err != nil {
		done()
		return opReply{}, nil, nil, err
	}

	return reply, dec, done, nil
}

// messageSchemes are the remote prefixes served by the message transport.
var messageSchemes = []string{"mem://", "ws://", "wss://", "tcp://"}

// isMessageRemote reports whether a remote address names a message transport.
func isMessageRemote(remote string) bool {
	for _, scheme := range messageSchemes {
		if strings.HasPrefix(remote, scheme) {
			return true
		}
	}
	return false
}

// setupMsgTransport wires the client's connection factory from the remote's
// scheme. A remote with no recognised scheme belongs to a session that was
// adopted rather than dialed, and so has no factory at all.
func (c *NetworkClient) setupMsgTransport() {
	switch {
	case strings.HasPrefix(c.remote, "mem://"):
		name := strings.TrimPrefix(c.remote, "mem://")
		c.ops = &msgOpTransport{
			owner: c,
			dial: func() (MessageConn, error) {
				return defaultMemRegistry.dial(name)
			},
		}

	case strings.HasPrefix(c.remote, "ws://"), strings.HasPrefix(c.remote, "wss://"):
		c.setupWSTransport()

	case strings.HasPrefix(c.remote, "tcp://"):
		c.setupTCPTransport()

	default:
		c.ops = &msgOpTransport{owner: c}
	}
}

func nowStamp() string {
	return time.Now().Format(time.RFC3339Nano)
}

// bearer returns the configured bearer token, which rides in the operation
// frame rather than an Authorization header on this transport.
func (c *NetworkClient) bearer() string {
	if c.State == nil || c.State.opts == nil {
		return ""
	}
	return c.State.opts.bearerToken
}

func traceMap(ctx context.Context) map[string]string {
	m := propagation.MapCarrier{}
	Propagator().Inject(ctx, m)
	if len(m) == 0 {
		return nil
	}
	return map[string]string(m)
}

func (c *NetworkClient) msgResolveCapability(name string) error {
	req := opRequest{Op: opLookup, Name: name, PubKey: c.State.pubkey, Contact: c.remote}
	_, dec, done, err := c.ops.roundTrip(c.State.top, req, nil)
	if err != nil {
		return NewResolveHTTPError(err, "error performing message request: %v", err)
	}
	defer done()

	var lr lookupResponse
	if err := dec.Decode(&lr); err != nil {
		return NewResolveDecodeError(name, c.remote, err)
	}
	if lr.Error != "" {
		return NewResolveLookupError(name, c.remote, lr.Error)
	}

	c.capa = lr.Capability
	c.oid = lr.Capability.OID
	return nil
}

func (c *NetworkClient) msgSendIdentity(ctx context.Context) error {
	ts := nowStamp()
	req := opRequest{
		Op:        opIdentify,
		PubKey:    c.State.pubkey,
		Contact:   c.remote,
		Timestamp: ts,
		Signature: signBytes(c.State.privkey, msgCanonical(opIdentify, "", ts)),
		Trace:     traceMap(ctx),
	}
	_, dec, done, err := c.ops.roundTrip(ctx, req, nil)
	if err != nil {
		return err
	}
	defer done()

	var lr identifyResponse
	if err := dec.Decode(&lr); err != nil {
		return err
	}
	if lr.Error != "" {
		return errors.New(lr.Error)
	}
	if !lr.Ok {
		return errors.New("identity rejected")
	}
	c.serverObservedAddress = lr.Address
	return nil
}

func (c *NetworkClient) msgReresolveCapability(rs *InterfaceState) error {
	req := opRequest{Op: opReresolve, PubKey: c.State.pubkey, Contact: c.remote}
	_, dec, done, err := c.ops.roundTrip(c.State.top, req, rs)
	if err != nil {
		return err
	}
	defer done()

	var lr lookupResponse
	if err := dec.Decode(&lr); err != nil {
		return err
	}
	if lr.Error != "" {
		return errors.New(lr.Error)
	}
	c.capa = lr.Capability
	c.oid = lr.Capability.OID
	return nil
}

func (c *NetworkClient) msgFetchMethods(ctx context.Context) (methodsResponse, error) {
	req := opRequest{Op: opMethods, OID: c.oid}
	_, dec, done, err := c.ops.roundTrip(ctx, req, nil)
	if err != nil {
		return methodsResponse{}, err
	}
	defer done()

	var mr methodsResponse
	if err := dec.Decode(&mr); err != nil {
		return methodsResponse{}, err
	}
	if mr.Error != "" {
		return methodsResponse{}, errors.New(mr.Error)
	}
	return mr, nil
}

func (c *NetworkClient) msgRequestReexport(ctx context.Context, capa *Capability, target ed25519.PublicKey) (*Capability, error) {
	ts := nowStamp()
	req := opRequest{
		Op:        opReexport,
		OID:       capa.OID,
		TargetPK:  target,
		Contact:   c.remote,
		Timestamp: ts,
		Signature: signBytes(c.State.privkey, msgCanonical(opReexport, string(capa.OID), ts)),
		Trace:     traceMap(ctx),
	}
	_, dec, done, err := c.ops.roundTrip(ctx, req, nil)
	if err != nil {
		return nil, err
	}
	defer done()

	var lr lookupResponse
	if err := dec.Decode(&lr); err != nil {
		return nil, err
	}
	if lr.Error != "" {
		return nil, errors.New(lr.Error)
	}
	return lr.Capability, nil
}

func (c *NetworkClient) msgDerefOID(ctx context.Context, oid OID) error {
	ts := nowStamp()
	req := opRequest{
		Op:        opDeref,
		OID:       oid,
		Timestamp: ts,
		Signature: signBytes(c.State.privkey, msgCanonical(opDeref, string(oid), ts)),
	}
	_, dec, done, err := c.ops.roundTrip(ctx, req, nil)
	if err != nil {
		return err
	}
	defer done()

	var rr refResponse
	if err := dec.Decode(&rr); err != nil {
		return err
	}
	if rr.Error != "" {
		return errors.New(rr.Error)
	}
	return nil
}

func (c *NetworkClient) msgUnaryCall(ctx context.Context, method string, args, result any) error {
	for {
		ts := nowStamp()
		req := opRequest{
			Op:        opCall,
			OID:       c.oid,
			Method:    method,
			Timestamp: ts,
			Signature: signBytes(c.State.privkey, msgCanonical(opCall, string(c.oid)+"/"+method, ts)),
			Bearer:    c.bearer(),
			Trace:     traceMap(ctx),
		}
		reply, dec, done, err := c.ops.roundTrip(ctx, req, args)
		if err != nil {
			return err
		}

		switch reply.Status {
		case "ok":
			err := dec.Decode(result)
			done()
			return err
		case "unknown-capability":
			done()
			if c.capa != nil && c.capa.RestoreState != nil {
				if rerr := c.msgReresolveCapability(c.capa.RestoreState); rerr == nil {
					continue
				}
			}
			return cond.NotFound("capability", c.oid)
		case "error":
			done()
			return cond.RemoteError(reply.Category, reply.Code, reply.Error)
		case "forbidden":
			done()
			// An authorization denial is a permission error, not a protocol one;
			// without this case it would surface as "unknown response status".
			return cond.RemoteError("permission", "forbidden", reply.Error)
		case "panic":
			done()
			return cond.Panic(reply.Error)
		default:
			done()
			return fmt.Errorf("unknown response status to %s/%s: %s", c.oid, method, reply.Status)
		}
	}
}

func (c *NetworkClient) msgCallWithCaps(ctx context.Context, method string, args, result any, caps map[OID]*InlineCapability) error {
	// The session is shared with every other call on this connection, so it is
	// deliberately not closed when this call finishes.
	sess, err := c.ops.controlSession(ctx)
	if err != nil {
		return err
	}

	ts := nowStamp()
	prelude := opRequest{
		Op:        opCallStream,
		OID:       c.oid,
		Method:    method,
		Timestamp: ts,
		Signature: signBytes(c.State.privkey, msgCanonical(opCallStream, string(c.oid)+"/"+method, ts)),
		Bearer:    c.bearer(),
		Trace:     traceMap(ctx),
		Version:   currentProtocolVersion,
	}

	return c.handleCallStream(ctx, sess, &c.ops.inline, &prelude, args, result, caps)
}
