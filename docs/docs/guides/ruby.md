---
title: Ruby on Miren
description: Deploy Ruby and Rails apps on Miren — automatic Bundler builds, no Dockerfile required.
keywords: [ruby, rails, puma, rack, bundler, sidekiq, secret_key_base, rails_master_key, deploy]
---

import CliCommand from '@site/src/components/CliCommand';

# Ruby on Miren

Miren auto-detects Ruby apps from a `Gemfile`, installs gems with Bundler, and
configures the right web server — no Dockerfile required. Rails, Puma, and Rack apps
all work out of the box.

:::tip[Let your agent do this]
Ask your AI coding agent to "set up this Rails app on Miren" after installing the
[Miren agent skills](/agent-skills). It detects your framework, stages secrets like
`SECRET_KEY_BASE` and `RAILS_MASTER_KEY`, proposes a start command, and deploys —
using this page as its reference.
:::

## Do you need a Dockerfile?

No. Miren detects Ruby from your `Gemfile` and builds the image automatically. The
default Ruby version is **3.2**; override it in [`.miren/app.toml`](/app-configuration)
if you need another. Provide a `Dockerfile.miren` only for custom build steps — see
[Using Dockerfile.miren](/guides#using-dockerfilemiren).

## Set up the app

From your project root:

<CliCommand context="client">
```miren
miren init
miren deploy
```
</CliCommand>

Preview what Miren detects — framework, entrypoint, staged env vars — without building:

<CliCommand context="client">
```miren
miren deploy --analyze
```
</CliCommand>

### Build process

Miren installs system dependencies (`build-essential`, `libpq-dev`, `nodejs`,
`libyaml-dev`, `postgresql-client`), then runs `bundle install` with
`BUNDLE_WITHOUT=development`. If Bootsnap is present, it precompiles the cache; if a
`Rakefile` exists, it runs `rake assets:precompile` when that task is available.

These environment variables are set for you automatically:

- `BUNDLE_PATH=/usr/local/bundle`
- `BUNDLE_WITHOUT=development`
- `RACK_ENV=production`
- `RAILS_ENV=production` (for Rails apps)

### Start command

Miren detects your web server. It must bind to `0.0.0.0` on `$PORT` — Miren injects
`PORT` and routes traffic to it.

| Framework | Detected entrypoint |
|-----------|---------------------|
| Rails | `bundle exec rails server -b 0.0.0.0 -p $PORT` |
| Puma (with config) | `bundle exec puma -C config/puma.rb` |
| Puma (no config) | `bundle exec puma -b tcp://0.0.0.0 -p $PORT` |
| Rack | `bundle exec rackup -p $PORT` |

Override with a `Procfile` to add workers or change the command:

```procfile
# Rails web server
web: bundle exec rails server -b 0.0.0.0 -p $PORT

# Sidekiq background worker
worker: bundle exec sidekiq
```

Use a single `web:` line. To run Puma with a config file instead of the Rails command:

```procfile
web: bundle exec puma -C config/puma.rb
```

See [Services](/services) for running Sidekiq or other workers alongside web.

## Environment variables

`miren init` stages the secrets a Rails app needs on first deploy: it **generates**
`SECRET_KEY_BASE` and **reads** `RAILS_MASTER_KEY` from `config/master.key` (or
`config/credentials/production.key`) if present, pre-setting both on the app. See
[What `miren init` Does for You](/app-configuration#what-miren-init-does-for-you).

Set anything else with `miren env set` — `-e` for plain values, `-s` for secrets
(masked in output and logs):

<CliCommand context="client">
```miren
miren env set -s DATABASE_URL
miren env set -s RAILS_MASTER_KEY=@config/master.key
miren env set -s SECRET_KEY_BASE
```
</CliCommand>

`KEY=@file` reads the value from a file; `-s SECRET_KEY_BASE` (no value) prompts with
masked input. You can also declare variables in `.miren/app.toml`:

```toml
[[env]]
key = "DATABASE_URL"
value = ""
required = true
sensitive = true
description = "Postgres connection string"
```

See [App Configuration — Environment Variables](/app-configuration#environment-variables).

## Agent quick reference

- **Detection:** `Gemfile` in the project
- **Default version:** Ruby 3.2 (override via `[build] version` in `.miren/app.toml`)
- **Install:** `bundle install` with `BUNDLE_WITHOUT=development`
- **Auto env:** `RACK_ENV=production`, `RAILS_ENV=production`, `BUNDLE_PATH`, `BUNDLE_WITHOUT`
- **Staged by `miren init`:** generates `SECRET_KEY_BASE`, reads `RAILS_MASTER_KEY` from `config/master.key`
- **Start command:** Rails/Puma/Rack auto-detected, bind `0.0.0.0:$PORT`; or set a `Procfile`
- **Env vars:** `miren env set -e/-s`, `KEY=@file` to read from disk, or `[[env]]` in `app.toml`
- **Dockerfile:** not needed; add `Dockerfile.miren` only for custom builds

## Next steps

- [App Configuration](/app-configuration) — customize `.miren/app.toml`
- [Services](/services) — web + Sidekiq workers
- [Deployment](/deployment) — how deploys build and activate
