# lbd kernel module source

`src/` is a verbatim copy of `src/` from [miren.dev/lbd], at the version `go.mod`
pins. It is checked in so `pkg/lbdmod` can embed it in the miren binary and hand
it to the lbd builder image, which compiles it against the running kernel.

**Do not edit anything under `src/`.** It is generated. Changes to the module
belong in the lbd repo.

To update: bump `miren.dev/lbd` in `go.mod`, then run `hack/sync-lbd-src.sh`. CI
runs `hack/sync-lbd-src.sh --check` so the copy and the pin cannot drift.
`src/VERSION` records which version the current copy came from.

`src/lz4/` is LZ4 by Yann Collet, vendored by lbd under its own BSD 2-Clause
license.

[miren.dev/lbd]: https://github.com/mirendev/lbd
