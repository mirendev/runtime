// Package workloadid mints and verifies Miren workload identity tokens.
//
// Every Miren cluster is its own OIDC issuer. Code running in a sandbox can ask
// the cluster for a short-lived JWT scoped to some audience, and whoever
// receives that token can check it against the cluster's published keys. This
// package holds both halves so services stop carrying their own copies:
//
//   - Minting: [Minter] asks the sandbox's token server for a token, and
//     [Keeper] keeps one audience's token fresh in the background.
//   - Verifying: [Verifier] checks a token from one of a fixed set of trusted
//     clusters, built on [Validator], which verifies any OIDC issuer.
//
// Miren uses this package in its own services. You're welcome to use it too,
// but it lives under x/ because its API may change between versions.
package workloadid
