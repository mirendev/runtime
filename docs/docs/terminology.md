---
sidebar_position: 4
---

# Terminology

Common terms used in Miren.

## App

An application deployed to Miren. Each app has a name, configuration, and one or more deployments.

## Cluster

A Miren server instance that runs your applications. A cluster can be standalone or connected to Miren Cloud for team management and multi-environment workflows.

## Miren Cloud

A central control plane that connects and manages your Miren clusters. Provides team management, access control, automatic data backup, and multi-environment workflows. See [Working with Miren Cloud](/working-with-miren-cloud).

## Miren Runtime

The core container orchestration system that powers Miren. Built on containerd, it handles building, deploying, and running your applications in isolated sandboxes.

## Miren Runtime Client

The `miren` CLI tool used to interact with your cluster. Manages apps, deployments, routes, and cluster configuration.

## Miren Server

The background service that runs on your cluster and manages applications, sandboxes, and networking. Installed as a systemd service via `miren server install`.

## Deployment

A specific version of your app that has been built and deployed. Each deployment creates a new container image and can be rolled back if needed.

## Disk

Persistent storage attached to your application. Miren disks survive restarts and redeployments, making them suitable for databases and stateful workloads.

## Route

Maps a hostname to an application. Routes determine how HTTP traffic reaches your apps. Your first app gets a default route automatically.

## Sandbox

An isolated execution environment where your app runs. Sandboxes use gvisor for security isolation and have their own network namespace.

## Service

A named process within an app. An app can have multiple services, each with its own command, image, port, and scaling configuration. Common services include `web` (HTTP server), `worker` (background jobs), and database services like `postgres`. See [Services](/services).

## Client Config

The configuration file (`~/.config/miren/clientconfig.yaml`) that stores your CLI settings, including cluster connections and the active cluster. Managed automatically by commands like `miren server install` and `miren cluster add`.
