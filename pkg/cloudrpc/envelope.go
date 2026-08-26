// Package cloudrpc lets a caller reach this cluster's RPC server through Miren
// Cloud, over the same uplink the cluster already holds open.
//
// It exists because the direct path assumes something that often is not true:
// that whoever wants to run `miren app list` can open a connection to the
// cluster. A cluster behind NAT can accept nothing, yet it is already talking to
// cloud. This turns that outbound link into a way in.
//
// The cluster is a tenant of pkg/uplink here, in the same way Miren Anywhere is:
// it registers handlers and owns none of the link's lifecycle. What rides on top
// is pkg/rpc's message transport, which runs the whole RPC protocol over any
// pipe of discrete byte messages — see pkg/rpc/PROTOCOL.md. The uplink is such a
// pipe once its envelopes are unwrapped, so the frames go through untouched and
// cloud never parses one.
//
// Authentication is unchanged by the detour, which is the point. Each operation
// still carries the caller's own bearer token, and this cluster's authenticator
// and RBAC judge it exactly as they would on a direct connection. Cloud
// authorizes the connection it is relaying and vouches for nothing beyond that.
package cloudrpc

// Control messages for a relayed RPC session, namespaced as their own family so
// the uplink stays a shared pipe. Cloud opens a session; either side may send
// data or close it.
const (
	TypeOpen  = "rpc.open"
	TypeData  = "rpc.data"
	TypeClose = "rpc.close"
)

// Open asks the cluster to start serving RPC on a new session. Cloud mints the
// id, because cloud is the only party that knows how many callers it is relaying
// for.
type Open struct {
	SessionID string `json:"session_id"`
}

// Data carries one message of the session's byte pipe.
//
// Payload is []byte rather than a string so encoding/json base64s it: the
// uplink's envelopes are JSON, and these frames are CBOR. The cost is the usual
// third again in size, which is why the session caps its frames well under the
// uplink's read limit.
type Data struct {
	SessionID string `json:"session_id"`
	Payload   []byte `json:"payload"`
}

// Close ends a session. Reason is for the log on the other side; nothing
// branches on it.
type Close struct {
	SessionID string `json:"session_id"`
	Reason    string `json:"reason,omitempty"`
}
