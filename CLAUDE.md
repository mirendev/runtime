# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Development Commands

### Building
- `make bin/miren` - Build the miren binary using hack/build.sh (includes version info)
- `make bin/miren-debug` - Build with debug symbols for debugging
- `make release` - Build release version using hack/build-release.sh

### Testing
- `make test` - Run all tests in dev container
- `make test-e2e` - Run e2e tests in dev container
- `go test ./...` - Run tests directly on host (hitting services on localhost)
- `./hack/dev-run.sh go test ./pkg/entity` - Run specific tests in dev container

### Development Environment
- `make dev` - Start dev environment and attach to shell
- `make dev-stop` - Stop dev environment and services
- `./hack/dev-run.sh <cmd>` - Execute any command in dev container context
- `docker exec -it miren-dev bash` - Attach another shell to running dev container

The dev environment provides:
- Long-running privileged container with containerd, buildkit, and gvisor (runsc)
- Source code mounted at `/src` (edits on host immediately visible)
- Miren binary built and available at `/bin/m`
- Services accessible: etcd:2379, clickhouse:9000, minio:9000
- Auth config generated at `~/.config/miren/clientconfig.yaml`
- Persistent caches for Go modules, build artifacts, and containerd data

**Debugging:**
- Native: Run `go test` on host for most tests (services on localhost)
- Remote: Use `./hack/dev-run.sh dlv test --headless --listen=:2345 --api-version=2 <pkg>` for containerd-dependent tests
- GoLand: Create "Go Remote" config pointing to localhost:2345 with path mapping to `/src`

### Other Commands
- `make image` - Build and import Docker image as `miren:latest`
- `make release-data` - Create release package tar.gz
- `make clean` - Remove built binaries

## Architecture Overview

This is the **Miren Runtime** - a container orchestration system built on containerd and gvisor with a custom entity system for managing applications and infrastructure.

### Core Components

**Entity System** (`pkg/entity/`, `api/entityserver/`):
- Central entity store using etcd backend
- Entity types defined in `api/*/schema.yml` files and generated Go structs
- Supports entity watching, indexing, and relationship management
- Controllers reconcile desired vs actual state

**Application Management** (`servers/app/`):
- Apps have versions with configurations (env vars, commands, concurrency)
- Entity store manages app metadata, filesystem stores Miren configs
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

The system uses Dagger for containerized development with all dependencies (containerd, buildkit, gvisor, etcd, ClickHouse, MinIO) provided as services. The development container includes proper cgroup setup for gvisor compatibility.

### Testing Notes

- Tests must run without any parallelism (`-p 1`) due to shared containerd/buildkit instances
- Integration tests in `e2e/` directory
- Test data in various `testdata/` directories
- Some tests require specific container runtime setup (runsc/gvisor)

### Code Generation

- Entity schemas → Go structs: `entity/cmd/schemagen`
- RPC interfaces → implementations: `pkg/rpc/cmd/rpcgen`
- Generated files have `.gen.go` suffix

### Code Style & Formatting

- **ALWAYS run `make lint` before committing** - This runs golangci-lint on the entire codebase
- Run `make lint-fix` to automatically fix issues where possible
- The codebase follows standard Go formatting conventions
- **Comment style**: Only add comments when they provide valuable context or explain "why" something is done
  - Avoid redundant comments that restate what the code does (e.g., `// Start server` above `server.Start()`)
  - Good comments explain complex logic, document assumptions, or clarify non-obvious behavior
  - Function/method comments should explain the purpose and any important side effects, not just restate the name
