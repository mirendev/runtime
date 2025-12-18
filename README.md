# Miren Runtime

[![Test](https://github.com/mirendev/runtime/actions/workflows/test.yml/badge.svg?branch=main)](https://github.com/mirendev/runtime/actions/workflows/test.yml?query=branch%3Amain)
[![Release](https://img.shields.io/github/v/tag/mirendev/runtime?sort=semver)](https://github.com/mirendev/runtime/releases/latest)
[![Changelog](https://img.shields.io/badge/changelog-miren.md-blue)](https://miren.md/changelog)
[![License](https://img.shields.io/github/license/mirendev/runtime)](LICENSE)

A container orchestration system built on containerd for running secure, isolated applications with etcd-backed state management.

## Overview

The Miren runtime provides a platform for deploying and managing containerized applications with strong isolation guarantees. It features an entity-based architecture with etcd as the distributed state store for managing applications, versions, sandboxes, and infrastructure components.

## Key Features

- **Secure Isolation**: Strong container isolation using containerd
- **Distributed State**: etcd backend for reliable, distributed state management
- **Multi-tenant**: Support for projects and isolated environments
- **HTTP Ingress**: Built-in routing for HTTP traffic to applications
- **Hot Reload**: Applications can be updated without downtime
- **CLI Tool**: Comprehensive command-line interface for all operations

## Architecture

### Core Components

- **Entity Store**: Central state management using etcd
- **Sandbox Controller**: Manages isolated execution environments
- **App Server**: Handles application lifecycle and deployments
- **Ingress Controller**: Routes HTTP traffic to applications
- **Build Server**: Handles application builds and image management

### Entity Types

- **Apps**: Application definitions with configuration
- **App Versions**: Specific versions of applications with container specs
- **Sandboxes**: Isolated execution environments running app versions
- **Routes**: HTTP routing rules for ingress
- **Projects**: Multi-tenant isolation boundaries

## Getting Started

### Prerequisites

- Go 1.25+ (required for building)
- iso (optional, for containerized development environment)

### Development Setup

```bash
# Clone the repository
git clone https://github.com/mirendev/runtime.git
cd runtime

# Start development environment with all dependencies
make dev

# Or with tmux for split terminals
make dev-tmux
```

The development environment automatically sets up:
- containerd for container runtime
- etcd for state storage

### Building

```bash
# Build the miren binary
make bin/miren

# Build with debug symbols
make bin/miren-debug

# Build release version
make release
```

### Running Tests

```bash
# Run all tests
make test

# Run tests in a specific package
hack/it <package>

# Run a specific test
hack/run <package> <test-name>
```

## CLI Usage

### Application Management

```bash
# Initialize a new application
miren init

# Deploy an application
miren deploy

# List all applications
miren apps

# Get application details
miren app <app-name>

# View application logs
miren logs <app-name>
```

### Sandbox Management

```bash
# List all sandboxes
miren sandbox list

# Filter sandboxes by status
miren sandbox list --status running

# Execute command in sandbox
miren sandbox exec <sandbox-id> -- <command>

# Get sandbox metrics
miren sandbox metrics <sandbox-id>
```

### Configuration Management

Cluster configurations are stored in `~/.config/miren/clientconfig.yaml`.

```bash
# Show current configuration
miren config info

# Switch active cluster
miren config set-active <cluster-name>

# Load additional configuration
miren config load <config-file>
```

## Application Configuration

Applications are configured using YAML files:

```yaml
name: myapp
container:
  - name: web
    image: myapp:latest
    command: ["/app/server"]
    env:
      PORT: "8080"
    resource:
      memory: "256Mi"
      cpu: "100m"
route:
  - hostname: "myapp.example.com"
    path: "/"
    port: 8080
```

## Development

### Building from Source

```bash
# Build the miren binary
make bin/miren
```

### Code Style

```bash
# Run linters on changed files
make lint-changed
```
