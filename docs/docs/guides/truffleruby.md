---
title: TruffleRuby on Miren
description: Deploy Ruby on the GraalVM TruffleRuby runtime on Miren using a Dockerfile.miren and Sinatra.
keywords: [truffleruby, ruby, graalvm, sinatra, puma, bundler, dockerfile, deploy]
---

import CliCommand from '@site/src/components/CliCommand';

# TruffleRuby on Miren

TruffleRuby is an alternative Ruby implementation built on GraalVM. Miren auto-detects
standard (MRI) Ruby from a `Gemfile` — see [Ruby on Miren](/guides/ruby) — but to pin a
specific runtime like TruffleRuby you use a `Dockerfile.miren`. Your Ruby code and gems
run unchanged.

:::tip[Let your agent do this]
Ask your AI coding agent to "set up this app on TruffleRuby on Miren" after installing
the [Miren agent skills](/agent-skills). It adds the `Dockerfile.miren`, wires up Bundler
and the server, and deploys — using this page as its reference.
:::

## Do you need a Dockerfile?

Yes — to run on TruffleRuby specifically. (Miren's auto-detection would pick MRI Ruby.)
Add a `Dockerfile.miren` built on the GraalVM TruffleRuby image. See
[Using Dockerfile.miren](/guides#using-dockerfilemiren).

:::tip[Want native support?]
Miren auto-detects and builds common stacks (Python, Node, Bun, Go, Ruby, Rust)
without a Dockerfile. This language isn't one of them yet — if you'd like first-class
support, [request it](https://linear.miren.garden/suggest).
:::

## The app

A normal Sinatra app with a `Gemfile` — nothing TruffleRuby-specific:

```ruby
# Gemfile
source 'https://rubygems.org'

gem 'sinatra'
gem 'puma'
gem 'rackup'
```

```ruby
# app.rb
require 'sinatra/base'

class App < Sinatra::Base
  # Sinatra 4 rejects unknown Host headers; an empty list permits any host.
  set :host_authorization, permitted_hosts: []

  get '/' do
    content_type 'text/plain'
    "Hello from TruffleRuby on Miren!\n"
  end
end
```

:::warning[Sinatra 4 blocks the deploy host]
Sinatra 4 enables `Rack::Protection::HostAuthorization`, which only allows requests whose
`Host` header is on an allowlist (localhost by default). Behind Miren's router your app is
reached at its route hostname, so without configuration it returns `Host not permitted`.
Set `host_authorization` to an empty `permitted_hosts` list (allow any), or list your
actual hostnames. This applies to any Sinatra 4 app, MRI included.
:::

```ruby
# config.ru
require './app'
run App
```

Miren injects `PORT` and routes to it; you bind it via the Puma command in the Procfile
below (`0.0.0.0` on `$PORT`).

## The Dockerfile

Create `Dockerfile.miren` in your project root, built on the official GraalVM community
image, and install gems with Bundler:

```dockerfile
FROM ghcr.io/graalvm/truffleruby-community:latest
WORKDIR /app
COPY Gemfile ./
RUN bundle install
COPY . /app
EXPOSE 8080
```

### .dockerignore

```text
.git
```

## Set up the app

Run Puma bound to the injected port:

```procfile
web: bundle exec puma -b tcp://0.0.0.0:$PORT
```

TruffleRuby warms up slowly (GraalVM startup plus gem loading), so raise `port_timeout`
past the 15-second default, and keep one instance always running instead of autoscaling
to zero. Set both in `.miren/app.toml`:

```toml
name = "truffleruby-bench"

[services.web]
# GraalVM Ruby boots slowly; give it more than the 15s default to bind.
port_timeout = "120s"

[services.web.concurrency]
mode = "fixed"
num_instances = 1
```

<CliCommand context="client">
```miren
miren deploy
```
</CliCommand>

:::warning[GraalVM boot vs. the port timeout]
By default Miren waits 15 seconds for a service to bind its port, then reports
`nothing is listening after the port timeout`. TruffleRuby's startup plus Bundler and
Puma routinely exceeds that. Raise `port_timeout` (e.g. `"120s"`) so the health check
waits long enough, and use fixed scaling so a warm instance stays up.
:::

## Environment variables

Set variables with `miren env set` — `-e` for plain values, `-s` for secrets (masked in
output and logs). Read them with `ENV['KEY']`:

<CliCommand context="client">
```miren
miren env set -e RACK_ENV=production
miren env set -s DATABASE_URL
```
</CliCommand>

See [App Configuration — Environment Variables](/app-configuration#environment-variables).

## Agent quick reference

- **Detection:** MRI Ruby is auto-detected from a `Gemfile`; use `Dockerfile.miren` to pin TruffleRuby
- **Base image:** `ghcr.io/graalvm/truffleruby-community:latest`; `bundle install` at build time
- **Service is required:** `Procfile` `web: bundle exec puma -b tcp://0.0.0.0:$PORT` — the image `CMD` is not used
- **Slow boot:** raise `port_timeout` (e.g. `"120s"`) past the 15s default, and pin `num_instances = 1` to stay warm
- **Port:** Puma `-b tcp://0.0.0.0:$PORT`
- **Env vars:** `miren env set -e/-s`; read with `ENV['KEY']`

## Next steps

- [Ruby on Miren](/guides/ruby) — auto-detected MRI Ruby
- [JRuby on Miren](/guides/jruby) — Ruby on the JVM
- [Application Scaling](/scaling) — fixed vs. autoscaling
