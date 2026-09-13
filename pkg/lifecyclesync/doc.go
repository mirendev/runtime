// Package lifecyclesync makes the on-disk lifecycle ledger visible beyond the
// host. It watches the ledger for changes and is the runtime half of the
// server-lifecycle uplink capability, through which cloud starts operations
// and follows them.
package lifecyclesync
