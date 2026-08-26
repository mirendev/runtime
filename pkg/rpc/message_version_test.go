package rpc

import (
	"testing"

	"github.com/fxamacker/cbor/v2"
	"github.com/stretchr/testify/require"
)

// The version gate is what lets a second implementation appear later without a
// breaking change, so it needs to actually reject and actually accept.
func TestProtocolVersionGate(t *testing.T) {
	t.Run("absent version reads as version 1", func(t *testing.T) {
		r := require.New(t)

		r.Equal(uint(protocolVersion1), opRequest{}.protocolVersion())
		r.Equal(uint(protocolVersion1), opRequest{Version: 1}.protocolVersion())
		r.Equal(uint(7), opRequest{Version: 7}.protocolVersion())
	})

	// A peer speaking a version this build does not know must be told so, rather
	// than having its frames misread as the version we do know.
	t.Run("rejects an unsupported version with the supported range", func(t *testing.T) {
		r := require.New(t)
		ctx := t.Context()

		clientEnd, serverEnd := newMemPipe()

		state, err := NewState(ctx, WithSkipVerify)
		r.NoError(err)

		router := &sessionRouter{state: state, server: state.server}
		go router.run(ctx, newMsgSession(serverEnd, false, 0, 0)) //nolint:errcheck // ends with ctx

		sess := newMsgSession(clientEnd, true, 0, 0)
		st, err := sess.OpenStreamSync(ctx)
		r.NoError(err)

		err = cbor.NewEncoder(st).Encode(opRequest{
			Op:      opLookup,
			Name:    "whatever",
			Version: currentProtocolVersion + 1,
		})
		r.NoError(err)

		var reply opReply
		r.NoError(cbor.NewDecoder(st).Decode(&reply))

		r.Equal("error", reply.Status)
		r.Contains(reply.Error, "unsupported protocol version")
	})

	// A peer predating the field sends no version at all; it must still work.
	t.Run("accepts a request carrying no version", func(t *testing.T) {
		r := require.New(t)
		ctx := t.Context()

		clientEnd, serverEnd := newMemPipe()

		state, err := NewState(ctx, WithSkipVerify)
		r.NoError(err)

		router := &sessionRouter{state: state, server: state.server}
		go router.run(ctx, newMsgSession(serverEnd, false, 0, 0)) //nolint:errcheck // ends with ctx

		sess := newMsgSession(clientEnd, true, 0, 0)
		st, err := sess.OpenStreamSync(ctx)
		r.NoError(err)

		err = cbor.NewEncoder(st).Encode(opRequest{
			Op:     opLookup,
			Name:   "nothing-exposed",
			PubKey: state.pubkey,
		})
		r.NoError(err)

		dec := cbor.NewDecoder(st)

		var reply opReply
		r.NoError(dec.Decode(&reply))

		// The lookup itself fails (nothing is exposed), but it was dispatched
		// rather than rejected by the version gate.
		r.Equal("ok", reply.Status)

		var lr lookupResponse
		r.NoError(dec.Decode(&lr))
		r.Contains(lr.Error, "unknown object")
	})
}
