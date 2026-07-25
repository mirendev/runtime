package rpc

import (
	"context"
	"errors"
	"io"
	"sync"

	"github.com/fxamacker/cbor/v2"
	"miren.dev/runtime/pkg/cond"
)

// inlineLookup resolves the inline capability a server-initiated callback is
// addressed to, along with the client that advertised it (the client supplies
// the caller identity for the resulting handler invocation).
type inlineLookup func(OID) (*NetworkClient, *InlineCapability, bool)

// callbackRouter delivers server-initiated inline-capability calls back to the
// capabilities a call advertised.
//
// The HTTP transports give every call its own session, so each call can own a
// private accept loop over it. Message transports share one session across all
// concurrent calls on a connection — there is no second connection to dial — so
// callbacks must instead be demultiplexed by OID through a session-scoped
// registry. OIDs are 16 random bytes (Server.assignCapability), so entries from
// different calls cannot collide.
type callbackRouter interface {
	// register makes caps reachable for the duration of one call and returns a
	// function that withdraws them.
	register(ctx context.Context, c *NetworkClient, sess rpcSession, caps map[OID]*InlineCapability) func()
}

// perCallRouter is the router for transports that dedicate a session to each
// call. It runs an accept loop bound to the call's lifetime.
type perCallRouter struct{}

func (perCallRouter) register(
	ctx context.Context,
	c *NetworkClient,
	sess rpcSession,
	caps map[OID]*InlineCapability,
) func() {
	ctx, cancel := context.WithCancel(ctx)

	go c.State.acceptInlineStreams(ctx, sess, func(oid OID) (*NetworkClient, *InlineCapability, bool) {
		ic, ok := caps[oid]
		return c, ic, ok
	})

	return cancel
}

// inlineRegistry routes callbacks by OID for a session shared across calls. The
// accept loop is owned by the session, not by any one call; calls only add and
// remove their capabilities.
type inlineRegistry struct {
	mu      sync.Mutex
	entries map[OID]inlineEntry
}

type inlineEntry struct {
	client *NetworkClient
	capa   *InlineCapability
}

func (r *inlineRegistry) register(
	_ context.Context,
	c *NetworkClient,
	_ rpcSession,
	caps map[OID]*InlineCapability,
) func() {
	r.mu.Lock()
	if r.entries == nil {
		r.entries = make(map[OID]inlineEntry, len(caps))
	}
	for oid, ic := range caps {
		r.entries[oid] = inlineEntry{client: c, capa: ic}
	}
	r.mu.Unlock()

	return func() {
		r.mu.Lock()
		for oid := range caps {
			delete(r.entries, oid)
		}
		r.mu.Unlock()
	}
}

func (r *inlineRegistry) lookup(oid OID) (*NetworkClient, *InlineCapability, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	e, ok := r.entries[oid]
	return e.client, e.capa, ok
}

// acceptInlineStreams accepts server-initiated streams on sess and serves the
// inline-capability calls they carry.
func (s *State) acceptInlineStreams(ctx context.Context, sess rpcSession, lookup inlineLookup) {
	for {
		str, err := sess.AcceptStream(ctx)
		if err != nil {
			return
		}

		go s.serveInlineStream(ctx, str, lookup)
	}
}

// serveInlineStream handles one server-initiated stream on a transport whose
// callback streams carry no opRequest prelude (HTTP/3 and WebTransport).
func (s *State) serveInlineStream(ctx context.Context, str rpcStream, lookup inlineLookup) {
	defer str.Close()

	s.serveInlineCalls(ctx, cbor.NewDecoder(str), cbor.NewEncoder(str), lookup)
}

// serveInlineCalls reads calls against inline capabilities until the stream
// ends. A stream is pooled by the peer's inlineClient and so may carry many
// calls in sequence.
func (s *State) serveInlineCalls(ctx context.Context, dec *cbor.Decoder, enc *cbor.Encoder, lookup inlineLookup) {
	for {
		var rs streamRequest

		if err := dec.Decode(&rs); err != nil {
			if !errors.Is(err, io.EOF) {
				s.log.Error("rpc.callstream call: error decoding stream request", "error", err)
			}
			return
		}

		switch rs.Kind {
		case "call":
			c, iface, ok := lookup(rs.OID)
			if !ok {
				_ = enc.Encode(refResponse{Status: "error", Error: "unknown capability"})
				continue
			}

			mm := iface.methods[rs.Method]
			if mm.Handler == nil {
				_ = enc.Encode(refResponse{Status: "error", Error: "unknown method"})
				continue
			}

			ctx, cancel := context.WithCancel(ctx)
			err := c.callInline(ctx, mm, rs.OID, rs.Method, iface.Interface, enc, dec)
			cancel()
			if err != nil {
				if !errors.Is(err, context.Canceled) && !errors.Is(err, cond.ErrClosed{}) {
					s.log.Error("rpc.callstream: error calling inline", "error", err)
				}
				return
			}
		default:
			s.log.Error("rpc.callstream: unknown call stream request", "kind", rs.Kind)
		}
	}
}
