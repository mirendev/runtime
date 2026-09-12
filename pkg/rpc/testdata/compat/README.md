# RPC version compatibility

From an iso shell, run `hack/test-rpc-compat /path/to/older/runtime`. Both source
trees must be visible inside the container. The script builds this same peer
against each tree's dependencies with the race detector, then runs the matrix.
The older tree is read without edits; temporary binaries are removed on exit.

The peers use runtime's generated `EntityAccess.WatchIndex` and `SandboxExec.Exec`
APIs. Deterministic handlers check ordered watch events, cancellation of an
established watch, exec input/output and terminal-size callbacks, and exit codes.
A forwarding handler uses the same stream adapters as the exec proxy. This checks
the RPC wire paths without starting etcd, containerd, or a real sandbox process.

The matrix covers both client/server versions with and without packet loss,
then every old/new combination of client, coordinator, and runner with loss on
the runner link. The UDP proxy drops every 17th packet and delays every 11th
remaining packet by 20 ms, independently in each direction of each flow. It
reports the actual counts; QUIC packetization and network scheduling still vary
between runs.

For MIR-1771, the baseline was runtime commit
`ee48139d6ce7faa909dca0bd678df84b3c07c2b9`, using webtransport-go v0.9.0 and
quic-go v0.57.1. Keep a v0.9 baseline available while the compatibility fork is
in use. This matrix checks transport compatibility; it does not promise that
unrelated application API changes are compatible across arbitrary revisions.
