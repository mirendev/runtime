package network

// TokenServerPort is where the sandbox controller serves the workload identity
// token server and the metrics push relay, on each node's bridge router. It
// lives here so the bridge firewall opens the same port the controller binds.
// Untagged, unlike the bridge code, because the controller needs it on every
// platform it builds for.
const TokenServerPort = 7123
