---
title: Terminology
description: Definitions of common terms used in Miren — apps, clusters, sandboxes, services, routes, addons, and more.
keywords: [terminology, glossary, definitions, concepts]
---

# Terminology

Common terms used in Miren.

## Addon

A managed backing service that Miren provisions and operates for your app. Addons handle infrastructure setup and inject connection credentials as environment variables automatically. Available addons include PostgreSQL, MySQL, Valkey, RabbitMQ, and Memcache. See [Addons](/addons).

## App

An application deployed to Miren. Each app has a name, configuration, and one or more deployments.

## app.toml

The configuration file for a Miren application, located at `.miren/app.toml`. Defines the app's services, environment variables, build settings, scaling behavior, disks, and addons. See [app.toml Reference](/app-toml).

## Build

The process of creating a container image from your application source code. Miren detects your language and framework, runs build steps (including any [onbuild](#onbuild) commands), and produces an image that runs in a [sandbox](#sandbox). Builds are powered by BuildKit.

## Client Config

The configuration file (`~/.config/miren/clientconfig.yaml`) that stores your CLI settings, including cluster connections and the active cluster. Managed automatically by commands like `miren server install` and `miren cluster add`.

## Cluster

A Miren server instance that runs your applications. A cluster can be standalone or connected to Miren Cloud for team management and multi-environment workflows.

## Cold Start

The additional latency on the first request to an app that has been [scaled to zero](#scale-to-zero) or needs a new [sandbox](#sandbox). Miren creates a sandbox, starts the process, and waits for it to bind its port before forwarding the request. Subsequent requests are served immediately.

## Concurrency

The scaling configuration for a [service](#service). Miren supports two modes: **auto** (scales instances up and down based on traffic) and **fixed** (runs a constant number of instances). Auto mode is configured with `requests_per_instance` and `scale_down_delay`; fixed mode uses `num_instances`. See [Application Scaling](/scaling).

## Default Route

The [route](#route) automatically assigned to an app when it is first deployed. Points to the cluster's hostname (e.g., `myapp.cluster-abc123.miren.systems`), giving the app a URL without any DNS configuration.

## Deployment

A specific version of your app that has been built and deployed. Each deployment creates a new container image and can be rolled back if needed.

## Disk

Persistent storage attached to your application. Miren disks survive restarts and redeployments, making them suitable for databases and stateful workloads. See [Persistent Storage](/disks).

## Ephemeral Version

A labeled, time-boxed preview build of your app that runs alongside the active [version](#version) on its own subdomain. Ephemeral versions don't affect production traffic and are deleted automatically when their [TTL](#ttl) expires. Used for pull request previews. See [Pull Request Environments](/pr-environments).

## Identity Provider

An OIDC-compliant authentication service (e.g., Google, GitHub, Okta) registered with your cluster. Identity providers are used for [route protection](#route-protection) (SSO in front of your app) and [OIDC bindings](#oidc-binding) (CI/CD deployment without stored secrets). Configured with `miren auth provider add`.

## Instance

A single running copy of a [service](#service). Each instance runs in its own [sandbox](#sandbox). The number of instances is controlled by the service's [concurrency](#concurrency) configuration and managed by the [sandbox pool](#sandbox-pool).

## Label

A DNS-compliant name assigned to an [ephemeral version](#ephemeral-version) (e.g., `pr-123`). The label becomes the subdomain prefix for accessing the preview: `<label>.<your-app-host>`.

## Miren Cloud

A central control plane that connects and manages your Miren clusters. Provides team management, access control, and multi-environment workflows. See [Miren Cloud](/miren-cloud/overview).

## Miren Runtime

The core container orchestration system that powers Miren. Built on containerd, it handles building, deploying, and running your applications in isolated sandboxes.

## Miren Runtime Client

The `miren` CLI tool used to interact with your cluster. Manages apps, deployments, routes, and cluster configuration.

## Miren Server

The background service that runs on your cluster and manages applications, sandboxes, and networking. Installed as a systemd service via `miren server install`.

## Node Port

A port exposed directly on the host machine for non-HTTP services like game servers, IRC, or custom TCP/UDP protocols. Unlike HTTP traffic (which is routed automatically through Miren's ingress layer), node ports give external clients a direct `host:port` to connect to. Configured with `port_type = "tcp"` in [app.toml](#apptoml). See [Traffic Routing](/traffic-routing#non-http-services-tcpudp).

## OIDC Binding

A configuration that links an [app](#app) to an [identity provider](#identity-provider) with a subject pattern, enabling CI/CD systems to deploy using short-lived OIDC tokens instead of stored secrets. For example, a binding can authorize GitHub Actions workflows from a specific repository to deploy a specific app. See [CI/CD Deployment](/ci-deploy).

## Onbuild

Commands defined in the `[build]` section of [app.toml](#apptoml) that run inside the build container after the main build steps. Used for tasks like asset compilation (`npm run build`) or pruning dev dependencies. Equivalent to Dockerfile `ONBUILD` instructions but configured declaratively.

## Procfile

A simple file format for defining [services](#service) in your app. Each line maps a service name to a command (e.g., `web: node server.js`). For more advanced configuration (scaling, disks, images), use [app.toml](#apptoml) instead.

## Route

Maps a hostname to an application. Routes determine how HTTP traffic reaches your apps. Your first app gets a [default route](#default-route) automatically. See [Traffic Routing](/traffic-routing).

## Route Protection

Authentication at the routing layer using an [identity provider](#identity-provider). Unauthenticated requests are redirected to an OIDC provider for login. After authentication, JWT claims are injected as HTTP headers (e.g., `X-User-Email`) before the request reaches your app — no in-app auth code required. See [Protecting Routes](/route-protect).

## Sandbox

An isolated execution environment where your app runs. Sandboxes use gvisor for security isolation and have their own network namespace.

## Sandbox Pool

The controller that manages the set of [instances](#instance) for a [service](#service) within a [version](#version). The pool tracks desired, current, and ready instance counts and reconciles them based on the service's [concurrency](#concurrency) configuration. Visible via `miren sandbox-pool list`.

## Scale to Zero

Miren's ability to scale a service down to zero [instances](#instance) when there is no traffic. The next incoming request triggers a [cold start](#cold-start) to create a new sandbox. Enabled by default for auto-scaled services. See [Application Scaling](/scaling#scale-to-zero).

## Service

A named process within an app. An app can have multiple services, each with its own command, image, port, and scaling configuration. Common services include `web` (HTTP server), `worker` (background jobs), and database services like `postgres`. See [Services](/services).

## TLS Certificate

An SSL/TLS certificate that Miren provisions automatically from [Let's Encrypt](https://letsencrypt.org/) when you set a [route](#route). Certificates are obtained via ACME HTTP-01 or DNS-01 challenges, cached on disk, and renewed automatically. See [TLS Certificates](/tls).

## TTL

Time to live — the expiration timer for an [ephemeral version](#ephemeral-version). After the TTL elapses, the ephemeral version is automatically deleted. Defaults to 24 hours and can be overridden with `--ttl` on deploy.

## Version

A unique identifier for a specific build of your app (e.g., `myapp-vCVkjR6u7744AsMebwMjGU`). Each [deployment](#deployment) creates a new version, which tracks the container image, git commit, and configuration. Previous versions can be rolled back to without rebuilding.

## WAF

Web Application Firewall — per-route request filtering using the [OWASP Core Rule Set](https://coreruleset.org/). Inspects incoming HTTP requests for common attacks like SQL injection, XSS, and path traversal. Configured per [route](#route) with a paranoia level (1–4) controlling strictness. See [WAF](/waf).

## Wildcard Route

A [route](#route) that matches all subdomains of a domain using a `*` prefix (e.g., `*.myapp.example.com`). Useful for multi-tenant apps or custom subdomain routing. Exact routes take priority over wildcards when both match. See [Traffic Routing](/traffic-routing#wildcard-routes).
