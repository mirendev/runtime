---
title: "miren debug test load"
sidebar_label: "debug test load"
description: "Loadtest a URL"
---

# miren debug test load

Loadtest a URL

## Usage

```bash
miren debug test load <url> [flags]
```

## Arguments

- `url` — URL to load test

## Flags

- `--accept, -A` — Accept header to use
- `--auth, -a` — Basic auth header to use
- `--concurrency, -c` — Number of concurrent requests to make (default: `50`)
- `--content-type, -T` — Content-Type header to use (default: `text/html`)
- `--cpus` — Number of CPUs to use
- `--data, -d` — HTTP request body
- `--data-file, -D` — File to use as request body
- `--disable-compression` — Disable compression
- `--disable-keepalives` — Disable keep-alives
- `--disable-redirects` — Disable redirects
- `--duration, -z` — Duration of the test (default: `0s`)
- `--h2` — Use HTTP/2
- `--header, -H` — HTTP header to use
- `--host` — Host header to use
- `--method, -m` — HTTP method to use (default: `GET`)
- `--output, -o` — Output type, the only supported value is 'csv'
- `--proxy, -x` — Proxy URL to use
- `--requests, -n` — Number of requests to make (default: `200`)
- `--timeout, -t` — Timeout for each request in seconds (default: `20`)
- `--user-agent, -U` — User-Agent header to use

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## See also

- [`miren debug test`](./debug-test.md)
