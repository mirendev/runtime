---
title: Python on Miren
description: Deploy Python apps — FastAPI, Django, Flask — on Miren with automatic build detection.
keywords: [python, fastapi, django, flask, gunicorn, uvicorn, uv, poetry, pipenv, deploy]
---

import CliCommand from '@site/src/components/CliCommand';

# Python on Miren

Miren auto-detects Python apps and builds a container image for you — no Dockerfile
required. It recognizes pip, Pipenv, Poetry, and uv, and configures a start command
for common web frameworks.

:::tip[Let your agent do this]
Ask your AI coding agent to "set up this Python app on Miren" after installing the
[Miren agent skills](/agent-skills). It detects your framework and package manager,
proposes a start command, wires up environment variables, and deploys — using this
page as its reference.
:::

## Do you need a Dockerfile?

No. Miren detects Python from a `requirements.txt`, `Pipfile`, `pyproject.toml`, or
`uv.lock` and builds the image automatically. The default Python version is **3.11**;
override it in [`.miren/app.toml`](/app-configuration) if you need another.

Provide a `Dockerfile.miren` only if your build needs custom system packages or steps
that don't fit detection — see [Using Dockerfile.miren](/guides#using-dockerfilemiren).

## Set up the app

From your project root, initialize and deploy:

<CliCommand context="client">
```miren
miren init
miren deploy
```
</CliCommand>

`miren init` scaffolds `.miren/app.toml` and scans your project for the environment
variables it needs. `miren deploy` uploads your code, builds the image, and activates
the new version. Preview what Miren detects — stack, package manager, and start
command — without building:

<CliCommand context="client">
```miren
miren deploy --analyze
```
</CliCommand>

### Package managers

Miren supports four Python dependency management systems and picks the install command
from the files in your repo. The table is a **priority order**: if your project has
more than one of these files, the first match wins.

| File | Package manager | Install command |
|------|-----------------|-----------------|
| `Pipfile` | pipenv | `pipenv install --deploy` |
| `uv.lock` | uv | `uv sync --frozen` |
| `pyproject.toml` | poetry | `poetry install --no-root` |
| `requirements.txt` | pip | `pip install -r requirements.txt` |

So a project with both a `Pipfile` and a `requirements.txt` builds with pipenv, not pip.

### Start command

Miren picks the start command from the **server package** in your dependencies rather
than from the web framework itself. The first rule that matches wins. You can always
override it with a `Procfile` or the `command` field in `.miren/app.toml`.

| Rule | Condition | Start command |
|------|-----------|---------------|
| 1 | `gunicorn` present, and no `fastapi` | `gunicorn <wsgi-module> -b 0.0.0.0:$PORT` |
| 2 | `fastapi` present | `fastapi run <entrypoint> --host 0.0.0.0 --port $PORT` |
| 3 | `uvicorn` present | `uvicorn <asgi-module> --host 0.0.0.0 --port $PORT` |
| 4 | `flask` present, no `gunicorn` | `flask run --host=0.0.0.0 --port=$PORT` |
| 5 | `django` and `manage.py` present, no `gunicorn` | `python manage.py runserver 0.0.0.0:$PORT` |

So a Django app is served by gunicorn when gunicorn is in its dependencies — Django
itself isn't what decides. FastAPI takes precedence over gunicorn, because `fastapi run`
is the framework's recommended entrypoint.

Miren fills in the module path from your project layout:

- **WSGI:** a `wsgi.py` in a subdirectory gives `<package>.wsgi:application` (the Django
  layout); a `wsgi.py` at the root gives `wsgi:app`; otherwise it falls back to `app:app`.
- **ASGI:** the same search over `asgi.py` gives `<package>.asgi:application` or
  `asgi:app`, falling back to `main:app`.
- **FastAPI:** the entrypoint from `[tool.fastapi]` in `pyproject.toml`, else `main.py`
  or `app.py`.

:::warning[Add a production server]
Rules 4 and 5 fall back to the Flask and Django development servers. Those are meant for
local use, not production traffic. Add `gunicorn` (or `uvicorn` for an ASGI app) to your
dependencies, or set an explicit `web:` line in a `Procfile`.
:::

Your server must bind to `0.0.0.0` on `$PORT` — Miren injects `PORT` and routes
traffic to it. A `Procfile` makes this explicit:

```procfile
# gunicorn (Flask / Django / WSGI)
web: gunicorn app:app --bind 0.0.0.0:$PORT

# Celery background worker
worker: celery -A tasks worker --loglevel=info
```

Pick a single `web:` line for your app. For an ASGI app, use uvicorn instead:

```procfile
web: uvicorn main:app --host 0.0.0.0 --port $PORT
```

With uv, pipenv, or poetry, prefix the command with the package manager:

```procfile
# uv
web: uv run gunicorn app:app --bind 0.0.0.0:$PORT

# Pipenv
web: pipenv run gunicorn app:app --bind 0.0.0.0:$PORT

# Poetry
web: poetry run gunicorn app:app --bind 0.0.0.0:$PORT
```

FastAPI is auto-detected, so a `Procfile` is optional — but you can be explicit:

```procfile
web: fastapi run
```

See [Services](/services) for running a worker alongside your web process.

## Environment variables

Set variables with `miren env set`. Use `-e` for plain values and `-s` for secrets
(masked in output and logs):

<CliCommand context="client">
```miren
miren env set -e LOG_LEVEL=info
miren env set -s DATABASE_URL
miren env set -s SECRET_KEY
```
</CliCommand>

`miren env set -s SECRET_KEY` (no value) prompts with masked input. You can also
declare variables in `.miren/app.toml`:

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

- **Detection:** `requirements.txt`, `Pipfile`, `pyproject.toml`, or `uv.lock`
- **Default version:** Python 3.11 (override via `[build] version` in `.miren/app.toml`)
- **Install:** pipenv / uv / poetry / pip, chosen by manifest in that priority order (see table above)
- **Start command:** chosen by server package, first match wins — gunicorn (unless FastAPI) → `fastapi run` → uvicorn → `flask run` → `manage.py runserver`; bind `0.0.0.0:$PORT`, or set a `Procfile`
- **Production server:** without gunicorn/uvicorn, Flask and Django fall back to their dev servers
- **Env vars:** `miren env set -e KEY=VALUE`, `-s` for secrets, or `[[env]]` in `app.toml`
- **Dockerfile:** not needed; add `Dockerfile.miren` only for custom builds

## Next steps

- [App Configuration](/app-configuration) — customize `.miren/app.toml`
- [Services](/services) — web + workers
- [Deployment](/deployment) — how deploys build and activate
