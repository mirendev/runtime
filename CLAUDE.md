# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Development Commands

### Building
- `make bin/runtime` - Build the runtime binary using hack/build.sh (includes version info)
- `make bin/runtime-debug` - Build with debug symbols for debugging
- `make release` - Build release version using hack/build-release.sh

### Testing
- `make test` - Run all tests using Dagger (equivalent to `dagger call -q test --dir=.`)
- `hack/it <gopkg>` - Run all tests in a package using Dagger
- `hack/run <gopkg> <testname>` - Run a single focused test using Dagger

### Development Environment
- `make dev` - Start development environment with Dagger
- `make dev-tmux` - Start development environment with tmux splits
- The dev environment automatically:
  - Sets up containerd, buildkit, and gvisor (runsc)
  - Builds the runtime binary and creates `/bin/r` symlink
  - Generates auth config in `~/.config/runtime/clientconfig.yaml`
  - Cleans the runtime namespace
  - Starts the runtime server and provides a shell

### Other Commands
- `make image` - Build and import Docker image as `miren:latest`
- `make release-data` - Create release package tar.gz
- `make clean` - Remove built binaries

## Architecture Overview

This is the **Miren Runtime** - a container orchestration system built on containerd and gvisor with a custom entity system for managing applications and infrastructure.

### Core Components

**Entity System** (`pkg/entity/`, `api/entityserver/`):
- Central entity store using PostgreSQL backend
- Entity types defined in `api/*/schema.yml` files and generated Go structs
- Supports entity watching, indexing, and relationship management
- Controllers reconcile desired vs actual state

**Application Management** (`app/`, `servers/app/`):
- Apps have versions with configurations (env vars, commands, concurrency)
- Database stores app metadata, filesystem stores runtime configs
- Default app controller handles app lifecycle

**Sandbox Management** (`controllers/sandbox/`):
- Sandboxes are isolated execution environments using gvisor
- Each sandbox runs in a separate containerd container with runsc runtime
- Network isolation with custom CNI setup

**RPC System** (`pkg/rpc/`):
- Custom RPC framework with code generation from YAML schemas
- Used for inter-service communication
- Supports both client-server and pub-sub patterns

**CLI** (`cli/commands/`):
- Extensive CLI for app management, debugging, and operations
- Commands include: app management, sandbox control, entity inspection, disk operations

### Key Directories

- `api/` - Generated and manual API definitions (protobuf-style schemas)
- `controllers/` - Kubernetes-style controllers for reconciliation
- `components/` - Core runtime components (scheduler, runner, etc.)
- `servers/` - RPC servers for various services
- `pkg/` - Shared libraries and utilities
- `lsvd/` - Custom log-structured virtual disk implementation
- `hack/` - Build scripts and development utilities

### Development Environment Setup

The system uses Dagger for containerized development with all dependencies (containerd, buildkit, gvisor, PostgreSQL, etcd, ClickHouse, MinIO) provided as services. The development container includes proper cgroup setup for gvisor compatibility.

### Testing Notes

- Tests must run without any parallelism (`-p 1`) due to shared containerd/buildkit instances
- Integration tests in `e2e/` directory
- Test data in various `testdata/` directories
- Some tests require specific container runtime setup (runsc/gvisor)

### Code Generation

- Entity schemas → Go structs: `entity/cmd/schemagen`
- RPC interfaces → implementations: `pkg/rpc/cmd/rpcgen`
- Generated files have `.gen.go` suffix
