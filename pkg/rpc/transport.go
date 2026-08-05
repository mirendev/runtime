package rpc

import (
	"context"
	"io"
)

// cancelReadCode is the application error code used to abort a blocked stream
// read when an RPC call's context is cancelled. WebTransport passes it through
// as a stream error code; the message transport has no per-stream error code and
// simply unblocks the read. Cancellation is ultimately detected by inspecting
// the call's context, so the specific value is internal.
const cancelReadCode uint64 = 0x13

// rpcStream is a single bidirectional sub-stream within an rpcSession. Both
// transports that back it — WebTransport over QUIC and msgmux over a
// MessageConn — provide ordered, independent sub-streams with their own
// lifetimes.
type rpcStream interface {
	io.ReadWriteCloser

	// CancelRead aborts a Read currently blocked on this stream. The code is
	// passed through to transports that support per-stream error codes
	// (WebTransport); transports without that notion (msgmux) ignore it and
	// simply unblock the read.
	CancelRead(code uint64)
}

// rpcSession is a multiplexed connection over which either peer may open or
// accept sub-streams at any time. This symmetric capability is what allows a
// server to call back on a client-advertised capability over the same
// connection. WebTransport provides it natively over QUIC; msgmux layers it over
// a MessageConn's discrete messages.
type rpcSession interface {
	OpenStreamSync(ctx context.Context) (rpcStream, error)
	AcceptStream(ctx context.Context) (rpcStream, error)
	Close() error
}
