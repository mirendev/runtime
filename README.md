# Miren Runtime

A container orchestration system built on containerd and gvisor for running secure, isolated applications with etcd-backed state management.

## Overview

Miren Runtime provides a platform for deploying and managing containerized applications with strong isolation guarantees using gvisor. It features an entity-based architecture with etcd as the distributed state store for managing applications, versions, sandboxes, and infrastructure components.

## Key Features

- **Secure Isolation**: Uses gvisor (runsc) for strong container isolation
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

- Go 1.24+ (required for building)
- Dagger (optional, for containerized development environment)

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
- containerd with gvisor runtime
- etcd for state storage
- buildkit for container builds
- MinIO for object storage
- ClickHouse for metrics

### Building

```bash
# Build the runtime binary
make bin/runtime

# Build with debug symbols
make bin/runtime-debug

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
runtime init

# Deploy an application
runtime deploy

# List all applications
runtime apps

# Get application details
runtime app <app-name>

# View application logs
runtime logs <app-name>
```

### Sandbox Management

```bash
# List all sandboxes
runtime sandbox list

# Filter sandboxes by status
runtime sandbox list --status running

# Execute command in sandbox
runtime sandbox exec <sandbox-id> -- <command>

# Get sandbox metrics
runtime sandbox metrics <sandbox-id>
```

### Configuration Management

Cluster configurations are stored in `~/.config/runtime/clientconfig.yaml`.

```bash
# Show current configuration
runtime config info

# Switch active cluster
runtime config set-active <cluster-name>

# Load additional configuration
runtime config load <config-file>
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
# Build the runtime binary
make bin/runtime
```

### Code Style

```bash
# Run linters on changed files
make lint-changed
```