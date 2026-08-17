package rpc

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"runtime/debug"
	"sort"
	"strings"

	"github.com/fxamacker/cbor/v2"
	"github.com/mr-tron/base58"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
)

// sessionRouter owns the accept loop for one message session and dispatches
// every inbound stream by the opRequest that opens it. Because both server
// operations and inline-capability callbacks share that framing, one end of a
// connection can both serve and call — which is what lets rpc run over a single
// connection it does not own.
type sessionRouter struct {
	state *State

	// server handles inbound operations; nil if this end only makes calls.
	server *Server

	// inline resolves callbacks against capabilities this end advertised; nil if
	// this end only serves.
	inline *inlineRegistry
}

// run accepts and dispatches streams until the session fails or ctx is
// cancelled, returning the terminating error.
func (r *sessionRouter) run(ctx context.Context, sess rpcSession) error {
	for {
		st, err := sess.AcceptStream(ctx)
		if err != nil {
			return err
		}
		go r.dispatch(ctx, sess, st)
	}
}

func (r *sessionRouter) dispatch(ctx context.Context, sess rpcSession, st rpcStream) {
	dec := cbor.NewDecoder(st)
	enc := cbor.NewEncoder(st)

	var req opRequest
	if err := dec.Decode(&req); err != nil {
		_ = st.Close()
		return
	}

	if v := req.protocolVersion(); v < minProtocolVersion || v > currentProtocolVersion {
		_ = enc.Encode(opReply{
			Status: "error",
			Error: fmt.Sprintf("unsupported protocol version %d; this peer speaks %d..%d",
				v, minProtocolVersion, currentProtocolVersion),
		})
		_ = st.Close()
		return
	}

	if req.Op == opInlineCall {
		defer st.Close()

		if r.inline == nil {
			_ = enc.Encode(opReply{Status: "error", Error: "no inline capabilities on this session"})
			return
		}
		r.state.serveInlineCalls(ctx, dec, enc, r.inline.lookup)
		return
	}

	if r.server == nil {
		_ = enc.Encode(opReply{Status: "error", Error: "this session does not serve operations"})
		_ = st.Close()
		return
	}

	r.server.handleOp(ctx, sess, st, dec, enc, req)
}

func (s *Server) handleOp(
	ctx context.Context,
	sess rpcSession,
	st rpcStream,
	dec *cbor.Decoder,
	enc *cbor.Encoder,
	req opRequest,
) {
	switch req.Op {
	case opLookup:
		s.msgLookup(enc, req)
		_ = st.Close()
	case opMethods:
		s.msgMethods(enc, req)
		_ = st.Close()
	case opReresolve:
		s.msgReresolve(dec, enc, req)
		_ = st.Close()
	case opReexport:
		s.msgReexport(enc, req)
		_ = st.Close()
	case opRef:
		s.msgRef(enc, req)
		_ = st.Close()
	case opDeref:
		s.msgDeref(enc, req)
		_ = st.Close()
	case opIdentify:
		s.msgIdentify(enc, req)
		_ = st.Close()
	case opCall:
		s.msgCall(ctx, dec, enc, req)
		_ = st.Close()
	case opCallStream:
		s.msgCallStream(ctx, sess, st, dec, enc, req)
	default:
		_ = enc.Encode(opReply{Status: "error", Error: "unknown op: " + req.Op})
		_ = st.Close()
	}
}

// writeReply emits an opReply followed (for control-plane ops) by the CBOR
// payload.
func writeReply(enc *cbor.Encoder, reply opReply, payload any) {
	_ = enc.Encode(reply)
	if payload != nil {
		_ = enc.Encode(payload)
	}
}

// authMsgCap verifies a capability-scoped op signature against the stored pubkey
// of the targeted capability.
func (s *Server) authMsgCap(req opRequest, target string) (ed25519.PublicKey, bool) {
	if err := freshTimestamp(req.Timestamp); err != nil {
		return nil, false
	}
	s.mu.Lock()
	capa, ok := s.objects[req.OID]
	s.mu.Unlock()
	if !ok {
		return nil, false
	}
	if !verifyBytes(capa.pub, msgCanonical(req.Op, target, req.Timestamp), req.Signature) {
		return nil, false
	}
	return capa.pub, true
}

func (s *Server) msgLookup(enc *cbor.Encoder, req opRequest) {
	pub := ed25519.PublicKey(req.PubKey)
	if len(pub) != ed25519.PublicKeySize {
		writeReply(enc, opReply{Status: "ok"}, lookupResponse{Error: "invalid public key"})
		return
	}

	s.mu.Lock()
	iface, ok := s.persistent[req.Name]
	s.mu.Unlock()

	if !ok {
		writeReply(enc, opReply{Status: "ok"}, lookupResponse{Error: "unknown object: " + req.Name})
		return
	}

	capa := s.assignCapability(iface, pub, s.state.contactAddr(), req.Name, false)
	capa.RestoreState = &InterfaceState{Category: "!persistent", Interface: req.Name}
	writeReply(enc, opReply{Status: "ok"}, lookupResponse{Capability: capa})
}

func (s *Server) msgMethods(enc *cbor.Encoder, req opRequest) {
	s.mu.Lock()
	hc, ok := s.objects[req.OID]
	s.mu.Unlock()

	if !ok {
		writeReply(enc, opReply{Status: "ok"}, methodsResponse{Error: "unknown capability: " + string(req.OID)})
		return
	}

	methods := make([]string, 0, len(hc.methods))
	for name := range hc.methods {
		methods = append(methods, name)
	}
	sort.Strings(methods)
	writeReply(enc, opReply{Status: "ok"}, methodsResponse{Methods: methods})
}

func (s *Server) msgReresolve(dec *cbor.Decoder, enc *cbor.Encoder, req opRequest) {
	var rs InterfaceState
	if err := dec.Decode(&rs); err != nil {
		writeReply(enc, opReply{Status: "ok"}, lookupResponse{Error: "failed to read body"})
		return
	}

	pub := ed25519.PublicKey(req.PubKey)
	if len(pub) != ed25519.PublicKeySize {
		writeReply(enc, opReply{Status: "ok"}, lookupResponse{Error: "invalid public key"})
		return
	}

	var (
		iface    *Interface
		category string
	)

	if rs.Category == "!persistent" {
		category = rs.Interface
		s.mu.Lock()
		pi, ok := s.persistent[rs.Interface]
		s.mu.Unlock()
		if !ok {
			writeReply(enc, opReply{Status: "ok"}, lookupResponse{Error: "unknown object: " + rs.Interface})
			return
		}
		iface = pi
	} else {
		category = rs.Category
		s.mu.Lock()
		res, ok := s.resolvers[rs.Category]
		s.mu.Unlock()
		if !ok {
			// No resolver means we can't rebuild the interface; without this
			// guard iface stays nil and assignCapability dereferences it.
			writeReply(enc, opReply{Status: "ok"}, lookupResponse{Error: "no resolver for category: " + rs.Category})
			return
		}
		var err error
		iface, err = res.ReconstructFromState(&rs)
		if err != nil {
			writeReply(enc, opReply{Status: "ok"}, lookupResponse{Error: "failed to resolve: " + err.Error()})
			return
		}
		if iface == nil {
			writeReply(enc, opReply{Status: "ok"}, lookupResponse{Error: "unable to restore capability"})
			return
		}
	}

	capa := s.assignCapability(iface, pub, s.state.contactAddr(), category, false)
	capa.RestoreState = &rs
	writeReply(enc, opReply{Status: "ok"}, lookupResponse{Capability: capa})
}

func (s *Server) msgReexport(enc *cbor.Encoder, req opRequest) {
	if _, ok := s.authMsgCap(req, string(req.OID)); !ok {
		writeReply(enc, opReply{Status: "error", Error: "unauthorized"}, lookupResponse{Error: "unauthorized"})
		return
	}

	target := ed25519.PublicKey(req.TargetPK)
	if len(target) != ed25519.PublicKeySize {
		writeReply(enc, opReply{Status: "ok"}, lookupResponse{Error: "invalid target public key"})
		return
	}

	s.mu.Lock()
	hc, ok := s.objects[req.OID]
	s.mu.Unlock()

	if !ok {
		writeReply(enc, opReply{Status: "ok"}, lookupResponse{Error: "unknown capability: " + string(req.OID)})
		return
	}

	capa := s.reexportCapability(req.OID, hc, target, s.state.contactAddr())
	writeReply(enc, opReply{Status: "ok"}, lookupResponse{Capability: capa})
}

func (s *Server) msgRef(enc *cbor.Encoder, req opRequest) {
	if _, ok := s.authMsgCap(req, string(req.OID)); !ok {
		writeReply(enc, opReply{Status: "error", Error: "unauthorized"}, refResponse{Error: "unauthorized"})
		return
	}

	s.mu.Lock()
	hc, ok := s.objects[req.OID]
	if ok {
		hc.refs.Add(1)
	}
	s.mu.Unlock()

	// Write after releasing the lock: a slow or backpressured peer must not
	// block every other operation on this Server.
	if ok {
		writeReply(enc, opReply{Status: "ok"}, refResponse{Status: "ok"})
	} else {
		writeReply(enc, opReply{Status: "ok"}, refResponse{Error: "unknown capability: " + string(req.OID)})
	}
}

func (s *Server) msgDeref(enc *cbor.Encoder, req opRequest) {
	if _, ok := s.authMsgCap(req, string(req.OID)); !ok {
		writeReply(enc, opReply{Status: "error", Error: "unauthorized"}, refResponse{Error: "unauthorized"})
		return
	}

	if s.Deref(req.OID) {
		writeReply(enc, opReply{Status: "ok"}, refResponse{Status: "ok"})
	} else {
		writeReply(enc, opReply{Status: "ok"}, refResponse{Error: "unknown capability: " + string(req.OID)})
	}
}

func (s *Server) msgIdentify(enc *cbor.Encoder, req opRequest) {
	pub := ed25519.PublicKey(req.PubKey)
	if err := freshTimestamp(req.Timestamp); err != nil {
		writeReply(enc, opReply{Status: "ok"}, identifyResponse{Error: "invalid identity"})
		return
	}
	if !verifyBytes(pub, msgCanonical(opIdentify, "", req.Timestamp), req.Signature) {
		writeReply(enc, opReply{Status: "ok"}, identifyResponse{Error: "invalid identity"})
		return
	}
	writeReply(enc, opReply{Status: "ok"}, identifyResponse{
		Ok:         true,
		Address:    req.Contact,
		Identity:   base58.Encode(pub),
		MinVersion: minProtocolVersion,
		MaxVersion: currentProtocolVersion,
	})
}

type callOutcome struct {
	status   string
	errMsg   string
	category string
	code     string
	results  any
}

// prepareMethodCall authenticates a call/callstream op, builds the
// signed-pubkey identity, looks up the method, and applies the public/non-public
// authorization gate. On failure it returns an opReply describing why.
func (s *Server) prepareMethodCall(ctx context.Context, req opRequest) (context.Context, *heldCapability, Method, ed25519.PublicKey, opReply, bool) {
	target := string(req.OID) + "/" + req.Method

	if err := freshTimestamp(req.Timestamp); err != nil {
		return ctx, nil, Method{}, nil, opReply{Status: "error", Error: "stale request"}, false
	}

	s.mu.Lock()
	iface, ok := s.objects[req.OID]
	s.mu.Unlock()
	if !ok {
		return ctx, nil, Method{}, nil, opReply{Status: "unknown-capability", Error: "unknown object: " + string(req.OID)}, false
	}

	if !verifyBytes(iface.pub, msgCanonical(req.Op, target, req.Timestamp), req.Signature) {
		return ctx, nil, Method{}, nil, opReply{Status: "error", Error: "failed to verify signature"}, false
	}

	iface.touch()

	mm := iface.methods[req.Method]
	if mm.Handler == nil {
		return ctx, nil, Method{}, nil, opReply{Status: "error", Error: "unknown method: " + req.Method}, false
	}

	// A bearer token, if the caller sent one, names a richer identity than the
	// capability does — a user or a workload rather than a key. Fall back to the
	// verified signature, which is all a message transport otherwise offers:
	// there is no TLS client certificate to inspect.
	identity, ok := s.msgIdentity(ctx, req, iface.pub)
	if !ok {
		// A token was presented but did not authenticate. Fail closed rather
		// than silently downgrading to the capability identity, which would let
		// a forged or expired token still execute.
		if !mm.Public {
			logAccess(ctx, s.state.audit(), msgRemoteFromContext(ctx), mm, "unauthorized")
		}
		return ctx, nil, Method{}, nil, opReply{Status: "forbidden", Error: "invalid bearer token"}, false
	}
	ctx = ContextWithIdentity(ctx, identity)

	if len(req.Trace) > 0 {
		ctx = Propagator().Extract(ctx, propagation.MapCarrier(req.Trace))
	}

	// Audited on the same terms as the HTTP path: non-public methods only, both
	// outcomes. Calls arriving this way were previously absent from the trail
	// entirely, which is the wrong gap to have — a cloud-routed call is exactly
	// the one an operator wants a record of.
	if !mm.Public {
		if s.state.authorizer != nil {
			resource := strings.ToLower(mm.InterfaceName)
			action := strings.ToLower(mm.Name)
			if err := s.state.authorizer.Authorize(ctx, identity, resource, action); err != nil {
				s.state.log.Warn("authorization denied",
					"resource", resource, "action", action, "subject", identity.Subject, "error", err)
				logAccess(ctx, s.state.audit(), msgRemoteFromContext(ctx), mm, "forbidden", "error", err)
				return ctx, nil, Method{}, nil, opReply{Status: "forbidden", Error: err.Error()}, false
			}
		}

		logAccess(ctx, s.state.audit(), msgRemoteFromContext(ctx), mm, "ok")
	}

	return ctx, iface, mm, iface.pub, opReply{}, true
}

// msgIdentity resolves the caller's identity for an operation on a message
// transport. The capability signature has already been verified by the caller.
//
// With no bearer token the identity is the capability's key. With a token, the
// authenticator's identity is used; a token that is present but fails to
// authenticate returns ok=false so the caller can fail closed rather than
// downgrade to the weaker capability identity.
func (s *Server) msgIdentity(ctx context.Context, req opRequest, capaPub ed25519.PublicKey) (*Identity, bool) {
	signed := &Identity{Subject: base58.Encode(capaPub), Method: AuthMethodSigned}

	if req.Bearer == "" || s.state.authenticator == nil {
		return signed, true
	}

	identity, err := s.state.authenticator.Authenticate(ctx, &Credentials{
		Authorization: "Bearer " + req.Bearer,
	})
	if err != nil {
		s.state.log.Warn("rpc: bearer authentication failed", "error", err)
		return nil, false
	}
	if identity == nil {
		return nil, false
	}

	return identity, true
}

func (s *Server) msgCall(ctx context.Context, dec *cbor.Decoder, enc *cbor.Encoder, req opRequest) {
	ctx, iface, mm, caller, reply, ok := s.prepareMethodCall(ctx, req)
	if !ok {
		_ = enc.Encode(reply)
		return
	}

	ctx, span := Tracer().Start(ctx, "rpc.handle."+mm.InterfaceName+"."+mm.Name)
	defer span.End()
	span.SetAttributes(attribute.String("oid", string(req.OID)))

	call := &NetworkCall{
		s:        s,
		oid:      req.OID,
		method:   req.Method,
		caller:   caller,
		category: iface.category,
		dec:      dec, // args follow the opRequest on this stream
	}

	if iface.aroundContext != nil {
		var cancel func()
		ctx, cancel = iface.aroundContext(ctx, call)
		defer cancel()
	}

	oc := s.invokeUnary(ctx, mm, call)
	switch oc.status {
	case "ok":
		_ = enc.Encode(opReply{Status: "ok"})
		_ = enc.Encode(oc.results)
	case "panic":
		_ = enc.Encode(opReply{Status: "panic", Error: oc.errMsg})
	default:
		_ = enc.Encode(opReply{Status: "error", Error: oc.errMsg, Category: oc.category, Code: oc.code})
	}
}

// invokeUnary runs a unary handler, mirroring handleCalls (no cond.Wrap; panic
// recovery), and returns the outcome.
func (s *Server) invokeUnary(ctx context.Context, mm Method, call *NetworkCall) (oc callOutcome) {
	defer func() {
		if r := recover(); r != nil {
			s.state.log.Error("panic in RPC handler",
				"panic", r, "method", call.method, "stack", string(debug.Stack()))
			oc = callOutcome{status: "panic", errMsg: fmt.Sprint(r)}
		}
	}()

	err := mm.Handler(ctx, call)
	if err != nil {
		msg, category, code := errorFields(err)
		return callOutcome{status: "error", errMsg: msg, category: category, code: code}
	}

	res := call.results
	if res == nil {
		res = struct{}{}
	}
	return callOutcome{status: "ok", results: res}
}

func (s *Server) msgCallStream(ctx context.Context, sess rpcSession, st rpcStream, dec *cbor.Decoder, enc *cbor.Encoder, req opRequest) {
	defer st.Close()

	cs := &controlStream{dec: dec, enc: enc}

	ctx, iface, mm, caller, reply, ok := s.prepareMethodCall(ctx, req)
	if !ok {
		// The callstream client reads streamRequest frames, not opReply, so the
		// rejection is restated as one. An authorization denial carries the same
		// category the unary path gives it.
		category, code := reply.Category, reply.Code
		if reply.Status == "forbidden" {
			category, code = "permission", "forbidden"
		}

		cs.NoReply(streamRequest{Kind: "error", Error: reply.Error, Category: category, Code: code}, nil)
		return
	}

	ctx, span := Tracer().Start(ctx, "rpc.handle."+mm.InterfaceName+"."+mm.Name)
	defer span.End()
	span.SetAttributes(attribute.String("oid", string(req.OID)))

	call := &NetworkCall{
		s:          s,
		oid:        req.OID,
		method:     req.Method,
		caller:     caller,
		category:   iface.category,
		dec:        dec,
		wsSession:  sess,
		msgSession: true,
		ctrl:       cs,
	}

	s.runCallStream(ctx, cs, mm, call)
}
