# Miren RPC message protocol

This is the wire format for the **message transport** — the way Miren RPC runs
over any reliable, ordered, bidirectional pipe of discrete byte messages. It
backs the `mem://`, `ws://`, `wss://` and `tcp://` schemes, and any connection
handed to `State.ServeMessageConn` or `State.ClientFromMessageConn`.

It is not the HTTP/3 + WebTransport transport, which remains the default and
speaks HTTP with the paths under `/_rpc/`.

Everything is [CBOR](https://cbor.io/). Structs use integer keys (`keyasint`),
so field names never appear on the wire.

**Status: version 1.** The Go implementation in this package is the only one
today. The format is documented and versioned so that a second implementation
does not require a breaking change, but the layouts below may still gain fields
(never renumber or repurpose one).

## Layers

```
  your backend            a socket, a broker, an envelope protocol
        |
  MessageConn             Send([]byte) / Recv() []byte / Close()
        |
  msgmux                  frames: multiplexes streams over messages
        |
  operations              opRequest / opReply: one operation per stream
```

The backend supplies message framing. Everything above assumes messages arrive
whole, in order, exactly once, and that a closed connection eventually surfaces
as an error from `Recv`.

## Layer 1: msgmux frames

Every message on the connection is one frame:

| Key | Field  | Type   | Meaning                       |
|-----|--------|--------|-------------------------------|
| 0   | Stream | uint32 | Stream id                     |
| 1   | Flags  | uint8  | Bit set, below                |
| 2   | Data   | bytes  | Payload; omitted when empty   |

Flags:

| Bit    | Name | Meaning                                              |
|--------|------|------------------------------------------------------|
| `0x01` | SYN  | Opens the stream                                     |
| `0x02` | DATA | Carries payload bytes                                |
| `0x04` | FIN  | No more data in this direction                       |
| `0x08` | RST  | Abort: discard the stream, fail any blocked reader   |

Rules:

- **Stream ids.** The dialing peer uses odd ids, the accepting peer even, so the
  two never collide. Ids increase by 2 and are not reused.
- **Opening.** A stream opens with an eager SYN, sent as its own frame before any
  data. The peer must be able to accept a stream whose opener reads before it
  writes.
- **Data for an unknown stream** that carries no SYN is dropped, not an error.
- **Closing.** FIN closes one direction. A stream is forgotten once both
  directions are finished. RST tears it down immediately from either side.
- **Frame size.** A payload larger than the session's frame cap is split across
  several DATA frames; the reader concatenates. The default cap is 1 MiB, and
  `WithMaxFrameSize` lowers it to fit a backend or envelope with its own limit.
  The cap bounds throughput, never correctness.
- **No flow control.** Deliberately: the backend already frames messages and
  applies its own backpressure. This is what makes msgmux smaller than a
  general-purpose stream multiplexer such as yamux, which must invent both over
  a byte stream.

## Layer 2: operations

**Every stream opens with an `opRequest`**, including server-initiated
callbacks. That invariant is what lets one accept loop route a whole connection,
and so lets a peer both serve and call over a single connection — including one
it dialed outbound through a firewall it could never accept through.

The operation's payload follows the `opRequest` as further CBOR values on the
same stream.

### opRequest

| Key | Field     | Type              | Meaning                                        |
|-----|-----------|-------------------|------------------------------------------------|
| 0   | Op        | string            | Operation selector, below                      |
| 1   | OID       | string            | Target capability                              |
| 2   | Name      | string            | Object name, for `lookup`                      |
| 3   | Method    | string            | Method name, for `call` / `callstream`         |
| 4   | PubKey    | bytes             | Caller's ed25519 public key                    |
| 5   | TargetPK  | bytes             | Recipient's public key, for `reexport`         |
| 6   | Contact   | string            | Address the caller can be reached at, if any   |
| 7   | Timestamp | string            | RFC3339Nano, signed                            |
| 8   | Signature | bytes             | ed25519 over the canonical string, below       |
| 9   | Bearer    | string            | Bearer token; there is no header to carry one  |
| 10  | Trace     | map[string]string | W3C trace context                              |
| 11  | Version   | uint              | Protocol version; absent or 0 means 1          |

### opReply

The first value written back, mapping the HTTP transport's
`rpc-status` / `rpc-error` trailers into the message world.

| Key | Field    | Type   | Meaning                       |
|-----|----------|--------|-------------------------------|
| 0   | Status   | string | Below                         |
| 1   | Error    | string | Human-readable message        |
| 2   | Category | string | Error category                |
| 3   | Code     | string | Error code                    |

Statuses: `ok`, `error`, `panic`, `unknown-capability`, `forbidden`.

`unknown-capability` is distinct because it is recoverable: a client holding
restore state re-resolves the capability and retries, rather than failing.

### Operations

| Op           | Purpose                                                     |
|--------------|-------------------------------------------------------------|
| `lookup`     | Resolve a named object to a capability                      |
| `call`       | Unary call; args follow, then `opReply` and the result      |
| `callstream` | Streaming call; the stream stays open for both directions   |
| `methods`    | List a capability's methods and parameters                  |
| `reresolve`  | Rebuild a capability from its restore state                 |
| `reexport`   | Re-issue a capability for a different holder                |
| `ref`        | Add a reference to a capability                             |
| `deref`      | Drop a reference to a capability                            |
| `identify`   | Prove the caller's key; returns the observed address        |
| `inline`     | Carries calls against an inline capability the peer advertised |

`inline` is the reverse direction: the peer opens a stream against a capability
you advertised in a `callstream` call. The prelude names only the capability,
because the stream is pooled and carries many calls; each one is a
`streamRequest` with its own method.

## Authentication

There is no TLS client certificate to inspect, so a caller proves itself by
signing a canonical string with the ed25519 key the capability was issued to.

```
canonical = "<op> <target> <timestamp>"
```

`target` is `"<oid>/<method>"` for `call` and `callstream`, `"<oid>"` for
capability-scoped operations, and the empty string for `identify` — which leaves
two consecutive spaces, e.g. `identify  2026-07-23T18:04:05.000000001Z`. The
separator is a single space in all three positions; nothing is trimmed.

The signature is raw bytes in field 8 — not base58, unlike the HTTP transports,
where it has to survive a header.

A timestamp older than 10 minutes is rejected, so a captured frame cannot be
replayed indefinitely.

`Bearer` is independent and optional. When present, it is offered to the
configured `Authenticator` as `"Bearer <token>"`, and the identity it returns
supersedes the signature identity — that is how a message transport reaches
per-user authorization. Without one, the identity is the capability's key, with
method `signed`.

## Versioning

`opRequest.Version` names the version a stream speaks. Absent or `0` means
version 1, so a peer predating the field reads as speaking it.

A version outside the receiver's supported range is rejected with an `opReply`
naming the range. This is a field rather than a handshake deliberately: a
mismatch costs one rejected stream instead of a round trip on every connection.

The `identify` reply also carries `min_version` and `max_version`, so a client
that wants to adapt up front can ask instead of probing.

## Implementing another peer

Minimum for a client:

1. Frame your transport into `MessageConn` semantics.
2. Implement msgmux: odd stream ids, eager SYN, FIN/RST, splitting at the frame
   cap.
3. Open a stream, send `opRequest{Op: "lookup", Name: ..., PubKey: ...}`, read
   `opReply` then a `lookupResponse` for the capability.
4. For each call, open a stream and send
   `opRequest{Op: "call", OID, Method, Timestamp, Signature}` followed by the
   args; read `opReply` and then the result.

Add an accept loop handling `inline` only if you advertise capabilities for the
peer to call back on.

Things worth getting right that are easy to miss:

- The SYN frame is separate and comes first.
- Sign the canonical string exactly, including the single spaces.
- One stream per operation. Do not reuse an operation stream — except an
  `inline` stream, which is explicitly pooled.
- Read the whole `opReply` before the payload; they are separate CBOR values on
  the same stream, not a wrapper.
