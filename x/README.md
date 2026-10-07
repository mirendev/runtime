# x/

Go packages from the Miren runtime that are meant to be imported by code outside this repository.

Miren's own services use these, and you're welcome to as well. They live under `x/` because they come with no compatibility promise: the API may change between versions, so pin what you import.

## Packages

- [`workloadid`](./workloadid): mint and verify Miren workload identity tokens. Code running in a Miren sandbox can get a short-lived OIDC token for any audience, and whoever receives it can check that a cluster they trust minted it.

## Importing

`x/` is its own Go module, separate from the rest of the runtime, so importing it doesn't pull in the runtime's dependencies:

```sh
go get miren.dev/runtime/x@latest
```

## Adding a package

Every requirement in `x/go.mod` is inherited by every importer, even one that uses a single package. Keep it to small, stable libraries. A package that needs something heavy should get a nested module of its own.

Packages here can't import the rest of the runtime. The runtime imports them instead, which keeps them the source of truth for anything they define.

The root `go.mod` both requires a published version of `x` and replaces it with `./x`. The `replace` only applies inside this repository, so anything that requires `miren.dev/runtime` by version gets `x` from the `require` line. When runtime code starts depending on a change here, bump that line to a commit that has the change (`go get miren.dev/runtime/x@<commit>`).
