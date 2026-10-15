---
title: Private Dependencies
description: Install private packages and private Git dependencies during a Miren build, using an encrypted secret your package manager reads at install time.
keywords: [private dependencies, private packages, npm token, npmrc, netrc, private git, github token, build secrets, registry credentials]
---

import CliCommand from '@site/src/components/CliCommand';

# Private Dependencies

If your app depends on a private package or a private Git repository, give the build a
credential to fetch it with. You store the credential as an encrypted
[secret](./secrets.md), tell Miren where your package manager expects to find it, and
deploy. It works with every language Miren builds automatically, with no Dockerfile
needed.

The credential is only available while dependencies install, and it never ends up in
your image or build cache.

## A complete example: a private npm package

Say your app installs `@acme/ui` from a private npm registry, and your `.npmrc` already
reads the token from the environment:

```ini title=".npmrc"
@acme:registry=https://npm.acme.dev/
//npm.acme.dev/:_authToken=${NPM_TOKEN}
```

**1. Store the token as a secret.** You'll be prompted for the value, and it's never
echoed:

<CliCommand context="client">

```miren
miren secret set registry/npm-token
```

</CliCommand>

**2. Hand it to the build** in `.miren/app.toml`. `ref` names the secret, and `env`
names the variable your `.npmrc` reads:

```toml
[[build.secrets]]
id = "npm"
ref = "registry/npm-token"
env = "NPM_TOKEN"
```

**3. Deploy** as usual. Commit `.npmrc` alongside your code, since Miren reads it from
your project.

<CliCommand context="client">

```miren
miren deploy
```

</CliCommand>

That's the whole pattern. Every recipe below is a variation on it: a secret, plus an
`env` or `file` that puts it where your tools look.

## Choosing `env` or `file`

Each `[[build.secrets]]` entry puts its secret in exactly one place:

- **`env`** sets an environment variable during the install. Most package registries
  read a token this way.
- **`file`** places the secret as a file. Use this for a whole config file, like a
  `~/.netrc` or a `pip.conf`. A path starting with `~/` lands in the home directory of
  whoever runs the install, so you don't need to know which user that is.

To store a file as a secret, pass it with `--value @`:

<CliCommand context="client">

```miren
miren secret set github/netrc --value @netrc
```

</CliCommand>

## Recipes

### Private Git repositories

One secret covers private Git dependencies in every language: Go modules, Ruby gems
with a `git:` source, npm packages from Git URLs, Python Git requirements, Elixir Git
deps, and Rust crates from Git. Write a netrc file with a token that can read the
repositories. For GitHub, use a fine-grained personal access token:

```text title="netrc"
machine github.com login x-access-token password github_pat_...
```

Store it, then mount it as `~/.netrc`:

```toml
[[build.secrets]]
id = "netrc"
ref = "github/netrc"
file = "~/.netrc"
```

SSH URLs like `git@github.com:acme/lib` work as they are. Builds have no SSH key, so
Miren fetches GitHub, GitLab, and Bitbucket SSH URLs over https, where the netrc
applies. You don't need to change your lockfile.

:::note[Go modules need a go.sum entry]
List private Go modules in `go.sum`. Go checks the public module proxy before fetching
a module directly, so a private module missing from `go.sum` fails its checksum
lookup. That lookup sends the module's path to `proxy.golang.org`, but never its code.
:::

### npm, Yarn, and Bun registries

Put the registry and an `${NPM_TOKEN}` placeholder in a committed `.npmrc`, as in the
[example above](#a-complete-example-a-private-npm-package), and pass the token with
`env = "NPM_TOKEN"`. npm, Yarn 1, and Bun all read `.npmrc`.

Yarn 2 and later (Berry) ignore `.npmrc`. If your app pairs a `yarn.lock` with another
language, such as a Rails or Phoenix app, and pins a newer Yarn, commit a `.yarnrc.yml`
that reads the same variable:

```yaml title=".yarnrc.yml"
npmScopes:
  acme:
    npmRegistryServer: "https://npm.acme.dev"
    npmAuthToken: "${NPM_TOKEN}"
```

Bun can also take the registry from `bunfig.toml`:

```toml title="bunfig.toml"
[install.scopes]
"@acme" = { url = "https://npm.acme.dev/", token = "$NPM_TOKEN" }
```

### Ruby gem servers

Bundler reads credentials for a gem server from an environment variable named after
its host: uppercase, with dots replaced by double underscores. For a source at
`https://gems.acme.dev`, store `username:password` (or the token your server expects)
and pass it as:

```toml
[[build.secrets]]
id = "gems"
ref = "registry/gems-acme"
env = "BUNDLE_GEMS__ACME__DEV"
```

### Python package indexes

How you pass credentials depends on which tool your project uses.

**pip** (`requirements.txt`): store a `pip.conf` with the index URL and credentials,
and mount it where pip looks:

```ini title="pip.conf"
[global]
index-url = https://user:token@pypi.acme.dev/simple/
```

Point `index-url` at a registry that also serves public PyPI packages. With
`extra-index-url`, pip installs whichever index has the higher version, so someone who
publishes your internal package name to PyPI could win.

```toml
[[build.secrets]]
id = "pipconf"
ref = "registry/pip-conf"
file = "/etc/pip.conf"
```

**uv**: name the index in `pyproject.toml`, then pass its username and password as two
secrets. uv reads `UV_INDEX_<NAME>_USERNAME` and `UV_INDEX_<NAME>_PASSWORD`, with the
name uppercased:

```toml title="pyproject.toml"
[[tool.uv.index]]
name = "acme"
url = "https://pypi.acme.dev/simple/"
```

```toml
[[build.secrets]]
id = "uv-user"
ref = "registry/pypi-acme-user"
env = "UV_INDEX_ACME_USERNAME"

[[build.secrets]]
id = "uv-pass"
ref = "registry/pypi-acme-token"
env = "UV_INDEX_ACME_PASSWORD"
```

**Poetry**: the same idea, with a source named in `pyproject.toml` and the variables
`POETRY_HTTP_BASIC_<NAME>_USERNAME` and `POETRY_HTTP_BASIC_<NAME>_PASSWORD`.

### Cargo registries

Define the registry in a committed `.cargo/config.toml`, then pass its token as
`CARGO_REGISTRIES_<NAME>_TOKEN`, with the name uppercased and dashes as underscores:

```toml title=".cargo/config.toml"
[registries.acme]
index = "sparse+https://cargo.acme.dev/index/"
```

```toml
[[build.secrets]]
id = "cargo"
ref = "registry/cargo-acme"
env = "CARGO_REGISTRIES_ACME_TOKEN"
```

## Dockerfile builds

A Dockerfile build ignores `env` and `file`. Mount each secret yourself in the `RUN`
step that needs it, using its `id`:

```dockerfile
RUN --mount=type=secret,id=npm \
    NPM_TOKEN="$(cat /run/secrets/npm)" npm ci
```

See [Using a secret at build time](./secrets.md#using-a-secret-at-build-time) for how
BuildKit keeps it out of your image, and what can still leak it.

## When a build fails

- **The secret can't be found.** A build fails rather than continuing without a
  credential it was promised. Check the `ref` with `miren secret list`.
- **The install still can't authenticate.** Check that your config file's placeholder
  matches the `env` name exactly, and that the file is committed. Miren reads `.npmrc`,
  `bunfig.toml`, and `.cargo/config.toml` from your project.
- **An entry has neither `env` nor `file`.** An automatic build fails on it, since the
  secret would have nowhere to go.

For every `[[build.secrets]]` field and its limits, see the
[app.toml reference](./app-toml.md#build-secrets).
