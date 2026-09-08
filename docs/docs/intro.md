---
slug: /
title: Miren
description: Miren is a container platform for small teams — deploy apps to a Linux server with builds, scaling, routing, TLS, and backups built in.
keywords: [miren, container platform, deploy, self-hosted, paas]
---

:::tip[Working with an AI coding agent?]
Point it at [llms.txt](https://miren.md/llms.txt) or [llms-full.txt](https://miren.md/llms-full.txt) for LLM-friendly docs, or install [Miren agent skills](https://github.com/mirendev/miren-skills) so it can deploy and manage your apps directly.
:::

# Miren

Miren is a container platform for small teams. You install it on a Linux server, point your CLI at it, and deploy with `miren deploy`. It handles builds, scaling, routing, TLS, and backups so you don't have to stitch together a platform from parts.

## How it works

Miren has two sides. The **server** runs on your Linux machine and manages containers, networking, and storage. The **client** is the `miren` CLI on your laptop (or CI runner), which talks to the server over a secure connection.

You deploy apps by running `miren deploy` from your project directory. Miren can run a configured container image directly, or detect your language (Python, Node, Bun, Go, Ruby, Rust, or a Dockerfile) and build one from your source. Your first app gets a route automatically. Scaling is automatic by default, adjusting instance counts based on traffic.

If your server is on a home network or behind a firewall, [Miren Anywhere](/miren-cloud/miren-anywhere) still puts your apps on the internet, routing their traffic through Miren Cloud so you don't need port forwarding or a public IP of your own.

Configuration lives in `.miren/app.toml` in your project. Environment variables, secrets, services, scaling behavior, and persistent disks are all managed through the CLI and this config file.

## Get started

**[Getting Started](/getting-started)** walks you through installation, server setup, and deploying your first app in a few minutes.

## Learn more

- [App Configuration](/app-configuration) - `.miren/app.toml` and how to configure your app
- [Deployment](/deployment) - Deploy workflows, rollbacks, and CI integration
- [Pull Request Environments](/pr-environments) - Time-boxed preview deploys, one per PR
- [Services](/services) - Run multiple processes (web, workers, databases) in one app
- [Scaling](/scaling) - Autoscaling and fixed instance modes
- [Traffic Routing](/traffic-routing) - Custom domains, TLS, and path-based routing
- [Disks](/disks) - Persistent storage with backup and restore
- [Logs](/logs) - Application, build, and system logs
- [CLI Reference](/commands) - Every command and flag
