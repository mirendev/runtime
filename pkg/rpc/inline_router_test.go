package rpc

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"testing"

	"github.com/fxamacker/cbor/v2"
	"github.com/stretchr/testify/require"
)

// errReader fails every read with err, standing in for a stream that has been
// reset underneath a decoder.
type errReader struct{ err error }

func (r errReader) Read([]byte) (int, error) { return 0, r.err }

// errSessionReset reproduces what a WebTransport stream read returns once the
// session closes underneath it. webtransport-go resets each open stream with
// its internal sessionCloseErrorCode (0x170d7b68), then fails to map that
// sentinel back into the WebTransport range, so the caller gets this opaque
// error instead of a typed one or a clean EOF. See MIR-1570.
var errSessionReset = fmt.Errorf(
	"stream reset, but failed to convert stream error %d: %w",
	0x170d7b68, errors.New("error code outside of expected range"),
)

// errWriter fails every write, so the encoder callInline replies through cannot
// flush and callInline reports the stream as unusable.
type errWriter struct{}

func (errWriter) Write([]byte) (int, error) { return 0, errors.New("stream gone") }

// inlineCallStream builds the bytes a peer's inlineClient writes for one
// callback: a "call" request followed immediately by its args.
func inlineCallStream(oid OID, method string) *cbor.Decoder {
	var buf bytes.Buffer

	enc := cbor.NewEncoder(&buf)
	if err := enc.Encode(streamRequest{Kind: "call", OID: oid, Method: method}); err != nil {
		panic(err)
	}
	if err := enc.Encode("args"); err != nil {
		panic(err)
	}

	return cbor.NewDecoder(&buf)
}

func serveInlineCallsWithLog(ctx context.Context, readErr error) string {
	var buf bytes.Buffer

	s := &State{StateCommon: &StateCommon{
		log: slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})),
	}}

	s.serveInlineCalls(
		ctx,
		cbor.NewDecoder(errReader{err: readErr}),
		cbor.NewEncoder(io.Discard),
		func(OID) (*NetworkClient, *InlineCapability, bool) { return nil, nil, false },
	)

	return buf.String()
}

func TestServeInlineCalls(t *testing.T) {
	t.Run("stays quiet when the stream ends cleanly", func(t *testing.T) {
		require.Empty(t, serveInlineCallsWithLog(t.Context(), io.EOF))
	})

	t.Run("stays quiet when the call's session is being torn down", func(t *testing.T) {
		// The caller cancels this context before closing the session that
		// resets the stream, so a read failure here is expected teardown
		// rather than something an operator should be told about.
		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		require.Empty(t, serveInlineCallsWithLog(ctx, errSessionReset))
	})

	t.Run("reports a stream that breaks during a live call", func(t *testing.T) {
		out := serveInlineCallsWithLog(t.Context(), errSessionReset)

		require.Contains(t, out, "error decoding stream request")
		require.Contains(t, out, "level=ERROR")
	})
}

// serveOneInlineCallWithLog drives a single dispatched callback whose reply
// cannot be written, so serveInlineCalls exercises the callInline arm.
func serveOneInlineCallWithLog(ctx context.Context) string {
	var buf bytes.Buffer

	s := &State{StateCommon: &StateCommon{
		log: slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})),
	}}

	const oid = OID("inline-cap")

	client := &NetworkClient{State: s, capa: &Capability{OID: oid}}
	capa := &InlineCapability{
		Capability: client.capa,
		Interface: &Interface{
			name: "Sink",
			methods: map[string]Method{
				"Record": {
					Name:    "Record",
					Handler: func(context.Context, Call) error { return nil },
				},
			},
		},
	}

	s.serveInlineCalls(
		ctx,
		inlineCallStream(oid, "Record"),
		cbor.NewEncoder(errWriter{}),
		func(OID) (*NetworkClient, *InlineCapability, bool) { return client, capa, true },
	)

	return buf.String()
}

func TestServeInlineCallsDispatch(t *testing.T) {
	t.Run("stays quiet when the call's session is being torn down", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		require.Empty(t, serveOneInlineCallWithLog(ctx))
	})

	t.Run("reports a callback that fails during a live call", func(t *testing.T) {
		out := serveOneInlineCallWithLog(t.Context())

		require.Contains(t, out, "error calling inline")
		require.Contains(t, out, "level=ERROR")
	})
}
