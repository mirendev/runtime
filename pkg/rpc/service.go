package rpc

// ServiceID represents an RPC service identifier.
// These are used with RPCClient() and server.ExposeValue() to identify services.
//
// Currently a type alias for incremental adoption. When all call sites use
// constants, this can become a distinct type (type ServiceID string) and
// function signatures can be updated for full type safety.
type ServiceID = string

// Runtime service identifiers.
// Add new services here to maintain type safety and avoid string typos.
const (
	ServiceRunner ServiceID = "dev.miren.runtime/runner"

	// ServiceNodeAdmin is served by each runner, for work the coordinator asks
	// one specific node to do to itself.
	ServiceNodeAdmin ServiceID = "dev.miren.runtime/nodeadmin"

	// ServiceSqliteBackup stores LTX transaction files replicated from
	// SQLite-provider disks on runners.
	ServiceSqliteBackup ServiceID = "dev.miren.runtime/sqlite-backup"
)
