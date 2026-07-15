---
title: PHP on Miren
description: Deploy PHP and Laravel apps on Miren with a Dockerfile.miren using FrankenPHP.
keywords: [php, laravel, symfony, frankenphp, composer, dockerfile, deploy]
---

import CliCommand from '@site/src/components/CliCommand';

# PHP on Miren

PHP isn't auto-detected, so you deploy it with a `Dockerfile.miren`. This guide uses
[FrankenPHP](https://frankenphp.dev) — a modern PHP application server that runs your
app from a single image, no separate nginx + php-fpm wiring. It works for plain PHP and
for frameworks like Laravel and Symfony.

:::tip[Let your agent do this]
Ask your AI coding agent to "set up this Laravel app on Miren" after installing the
[Miren agent skills](/agent-skills). It adds the `Dockerfile.miren`, points the server
at `0.0.0.0:$PORT`, wires up environment variables, and deploys — using this page as its
reference.
:::

## Do you need a Dockerfile?

Yes. Miren doesn't auto-detect PHP, so add a `Dockerfile.miren` to your project root.
Miren builds from it instead of guessing the stack — see
[Using Dockerfile.miren](/guides#using-dockerfilemiren).

:::tip[Want native support?]
Miren auto-detects and builds common stacks (Python, Node, Bun, Go, Ruby, Rust)
without a Dockerfile. This language isn't one of them yet — if you'd like first-class
support, [request it](https://linear.miren.garden/suggest).
:::

## The Dockerfile

Create `Dockerfile.miren` in your project root. FrankenPHP serves your `public/`
directory:

```dockerfile
FROM dunglas/frankenphp:1-php8.3

WORKDIR /app
COPY . /app

EXPOSE 8080
```

For a Laravel or Composer app, install dependencies during the build:

```dockerfile
FROM dunglas/frankenphp:1-php8.3

# Composer from its official pinned image — no piping a remote installer into php
COPY --from=composer:2 /usr/bin/composer /usr/bin/composer
RUN install-php-extensions pdo_pgsql pdo_mysql zip

WORKDIR /app
# Copy the manifests first so the install layer is deterministic and cache-friendly;
# committing composer.lock pins exact dependency versions.
COPY composer.json composer.lock ./
RUN composer install --no-dev --optimize-autoloader --no-scripts --no-interaction

COPY . /app
RUN composer dump-autoload --optimize --no-interaction

EXPOSE 8080
```

(`install-php-extensions` ships with the FrankenPHP image.)

### .dockerignore

```text
.git
vendor
```

## Set up the app

Even with a `Dockerfile.miren`, Miren needs at least one **service** defined — it
doesn't use the image's `CMD` as the start command. FrankenPHP's `php-server` command
takes the listen address and document root; point it at Miren's injected `$PORT` and
`0.0.0.0` in a `Procfile`:

```procfile
web: frankenphp php-server --listen 0.0.0.0:$PORT --root /app/public
```

Then create `.miren/app.toml` naming your app and deploy from your project root:

```toml
name = "php-bench"
```

<CliCommand context="client">
```miren
miren deploy
```
</CliCommand>

:::note[Deploying without a service fails]
If no service is defined, the build succeeds but the deploy stops with
`no services defined: please define at least one service in a Procfile or
.miren/app.toml`.
:::

## Environment variables

Set variables with `miren env set` — `-e` for plain values, `-s` for secrets (masked in
output and logs). Read them with `getenv()` or your framework's config (Laravel's
`env()`):

<CliCommand context="client">
```miren
miren env set -e APP_ENV=production
miren env set -s APP_KEY
miren env set -s DATABASE_URL
```
</CliCommand>

Generate a Laravel `APP_KEY` locally with `php artisan key:generate --show`. You can
also declare variables in `.miren/app.toml`:

```toml
[[env]]
key = "APP_ENV"
value = "production"
```

Need a managed Postgres database? Add a [`miren-postgresql` addon](/addons) and Miren
injects `DATABASE_URL` for you. See
[App Configuration — Environment Variables](/app-configuration#environment-variables).

## Agent quick reference

- **Detection:** none — requires `Dockerfile.miren`
- **Base image:** `dunglas/frankenphp:1-php8.3`; use `install-php-extensions` for pdo_pgsql, etc.
- **Composer:** `composer install --no-dev --optimize-autoloader` during the build
- **Service is required:** `Procfile` `web: frankenphp php-server --listen 0.0.0.0:$PORT --root /app/public` — the image `CMD` is not used
- **Port:** FrankenPHP `--listen 0.0.0.0:$PORT`
- **Env vars:** `miren env set -e/-s`; read with `getenv()` / Laravel `env()`
- **Database:** optional `[addons.miren-postgresql]` injects `DATABASE_URL`

## Next steps

- [Using Dockerfile.miren](/guides#using-dockerfilemiren) — how custom builds work
- [Addons](/addons) — managed Postgres and other backing services
- [App Configuration](/app-configuration) — customize `.miren/app.toml`
- [Deployment](/deployment) — how deploys build and activate
