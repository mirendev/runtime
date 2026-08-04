---
title: "miren server container install"
sidebar_label: "server container install"
description: "Install miren server in a container"
---

# miren server container install

Install miren server in a container

## Usage

```bash
miren server container install [flags]
```

## Flags

- `--allow-rootless` — Install even on a rootless runtime (control plane only; app workloads won't run)
- `--cluster-name` — Cluster name for cloud registration
- `--force, -f` — Remove existing container if present
- `--host-network` — Use host networking (ignores port mappings)
- `--http-port` — HTTP port mapping (default: `80`)
- `--image, -i` — Container image to use (default: `oci.miren.cloud/miren:latest`)
- `--ingress-mode` — Ingress mode: tls-autoprovision (default), behind-proxy-http (Miren serves plain HTTP behind a TLS-terminating proxy like tailscale serve / nginx), or behind-proxy-https (Miren terminates TLS on :443 behind a TCP-passthrough proxy)
- `--labs, -l` — Miren Labs features to enable (e.g. sagas). Prefix with - to disable.
- `--name, -n` — Container name
- `--runtime` — Container runtime to use: docker or podman (auto-detected by default, preferring docker)
- `--url, -u` — Cloud URL for registration (default: `https://miren.cloud`)
- `--without-cloud` — Skip cloud registration setup

## Global Options

- `--options` — Path to file containing options
- `--server-address` — Server address to connect to (default: `127.0.0.1:8443`)
- `--verbose, -v` — Enable verbose output

## Examples

**Install with cloud registration:**

```bash
miren server container install
```

**Install without cloud (local only):**

```bash
miren server container install --without-cloud
```

**Install with a custom HTTP port:**

```bash
miren server container install --http-port 8080
```

**Install behind a TLS-terminating proxy (e.g. tailscale serve):**

```bash
miren server container install --ingress-mode behind-proxy-http
```

**Force a specific runtime:**

```bash
miren server container install --runtime podman
```

## See also

- [`miren server container`](/command/server-container)
