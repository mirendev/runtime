package rpc

import (
	"context"
)

// defaultMaxFrameData bounds the payload carried in a single msgmux frame, so
// that a backend with a maximum message size (e.g. a broker capping messages
// around a megabyte) can carry arbitrarily large stream writes as multiple
// frames. Override it per session with WithMaxFrameSize.
const defaultMaxFrameData = 1 << 20 // 1 MiB

// MessageConn is the pure message interface a message-oriented backend
// implements: a reliable, ordered, bidirectional, point-to-point pipe of
// discrete byte messages between two peers. The framework layers a stream
// multiplexer (msgmux) on top to recover the rpcSession semantics that
// callbacks and streaming require.
//
// Recv blocks until a message is available and returns io.EOF once the peer
// has closed the connection and no buffered messages remain.
type MessageConn interface {
	Send([]byte) error
	Recv() ([]byte, error)
	Close() error
}

// MessageSessionOption configures a session built over a MessageConn.
type MessageSessionOption func(*messageSessionOptions)

type messageSessionOptions struct {
	maxFrame int
}

// WithMaxFrameSize bounds the payload rpc places in a single message. Set it
// below the backend's own message-size limit — an envelope protocol wrapping
// these messages, or a broker capping message size. Stream writes larger than
// the bound are split across frames, so the bound costs throughput, never
// correctness.
func WithMaxFrameSize(n int) MessageSessionOption {
	return func(o *messageSessionOptions) {
		o.maxFrame = n
	}
}

func buildMessageSessionOptions(opts []MessageSessionOption) messageSessionOptions {
	var o messageSessionOptions
	for _, fn := range opts {
		fn(&o)
	}
	return o
}

// ServeMessageConn serves RPC over a connection the caller owns, and blocks
// until the connection fails or ctx is cancelled.
//
// This is the entry point for backends rpc does not manage — most importantly a
// socket carrying somebody else's envelope protocol, where the caller unwraps
// inbound payloads into Recv and wraps outbound payloads from Send. rpc never
// dials, listens, or closes such a connection.
//
// Capabilities minted on this connection are reachable only for its lifetime;
// they carry no dialable address.
func (s *State) ServeMessageConn(ctx context.Context, conn MessageConn, opts ...MessageSessionOption) error {
	o := buildMessageSessionOptions(opts)

	sess := newMsgSession(conn, false, o.maxFrame)
	router := &sessionRouter{state: s, server: s.server}

	return router.run(ctx, sess)
}

// ClientFromMessageConn builds a client for the named remote object over a
// connection the caller owns, the dialing counterpart to ServeMessageConn.
//
// The resulting session serves as well as calls: the peer may invoke objects
// this State has exposed over the same connection. That is what allows a party
// that dialled outbound — through a firewall it could not accept through — to
// still be called back into.
func (s *State) ClientFromMessageConn(
	ctx context.Context,
	conn MessageConn,
	name string,
	opts ...MessageSessionOption,
) (*NetworkClient, error) {
	o := buildMessageSessionOptions(opts)

	client := &NetworkClient{
		State:         s,
		transportKind: transportMsg,
		remote:        adoptedRemote,
	}
	// dial stays nil: the connection belongs to the caller and cannot be
	// re-established from here.
	client.ops = &msgOpTransport{owner: client}
	client.ops.adopt(ctx, newMsgSession(conn, true, o.maxFrame))

	if err := client.resolveCapability(name); err != nil {
		return nil, err
	}

	return client, nil
}

// opRequest is the first frame of an operation stream. It carries the operation
// selector plus the metadata that, on the HTTP transports, lives in the request
// path, headers, and auth signature. The operation payload (CBOR args /
// InterfaceState / etc.) follows as subsequent bytes on the same stream.
type opRequest struct {
	Op     string `cbor:"0,keyasint"`
	OID    OID    `cbor:"1,keyasint,omitempty"`
	Name   string `cbor:"2,keyasint,omitempty"`
	Method string `cbor:"3,keyasint,omitempty"`

	PubKey   []byte `cbor:"4,keyasint,omitempty"`
	TargetPK []byte `cbor:"5,keyasint,omitempty"`
	Contact  string `cbor:"6,keyasint,omitempty"`

	Timestamp string `cbor:"7,keyasint,omitempty"`
	Signature []byte `cbor:"8,keyasint,omitempty"`

	Bearer string            `cbor:"9,keyasint,omitempty"`
	Trace  map[string]string `cbor:"10,keyasint,omitempty"`

	// Version is the protocol version this stream speaks. Zero means version 1,
	// so a peer predating the field is treated as speaking it. See PROTOCOL.md.
	Version uint `cbor:"11,keyasint,omitempty"`
}

// Protocol versions carried in opRequest.Version. A field rather than a
// handshake: a version mismatch costs one rejected stream, not a round trip on
// every connection.
const (
	// protocolVersion1 is the only version defined. Zero on the wire means this.
	protocolVersion1 = 1

	// currentProtocolVersion is what this implementation sends.
	currentProtocolVersion = protocolVersion1

	// minProtocolVersion is the oldest version this implementation accepts.
	minProtocolVersion = protocolVersion1
)

// protocolVersion normalises a version off the wire, mapping the zero value
// onto version 1.
func (r opRequest) protocolVersion() uint {
	if r.Version == 0 {
		return protocolVersion1
	}
	return r.Version
}

// opReply is the first frame of an operation reply, mapping the HTTP
// rpc-status/rpc-error[/-category/-code] trailers into the message world. The
// reply payload (CBOR result / lookupResponse / etc.) follows on the stream.
type opReply struct {
	Status   string `cbor:"0,keyasint"`
	Error    string `cbor:"1,keyasint,omitempty"`
	Category string `cbor:"2,keyasint,omitempty"`
	Code     string `cbor:"3,keyasint,omitempty"`
}

// Operation selectors carried in opRequest.Op. Every stream on a message
// session opens with an opRequest, including server-initiated callbacks
// (opInlineCall), so a single accept loop can route the whole session and
// either peer may both serve and call over one connection.
const (
	opCall       = "call"
	opCallStream = "callstream"
	opLookup     = "lookup"
	opMethods    = "methods"
	opReresolve  = "reresolve"
	opReexport   = "reexport"
	opRef        = "ref"
	opDeref      = "deref"
	opIdentify   = "identify"

	// opInlineCall opens a stream carrying calls against an inline capability
	// the peer advertised. The stream is pooled by the caller and may carry many
	// calls, so the prelude names only the capability, not a method.
	opInlineCall = "inline"
)
