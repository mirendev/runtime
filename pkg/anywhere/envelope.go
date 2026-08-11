package anywhere

// TypeConnectionRequest is sent from cloud to cluster when a POP server needs
// the cluster to establish an HTTP/3 connection.
//
// This is the one seam between Miren's two links: the data plane's setup
// handshake is delivered over the control plane. Everything else about POP
// connections happens on the data plane itself.
const TypeConnectionRequest = "connection.request"

// ConnectionRequest is the payload for connection.request messages.
type ConnectionRequest struct {
	POPXID       string `json:"pop_xid"`
	POPAddress   string `json:"pop_address"`
	Hostname     string `json:"hostname"`
	RequestID    string `json:"request_id"`
	ConnectToken string `json:"connect_token"`
}
