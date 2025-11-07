# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Development Commands

**IMPORTANT: This project supports two containerized build systems:**
- **iso**: For local development (default `make` targets) - **Recommended**
- **Dagger**: For CI/CD (targets with `-dagger` suffix)

This is a two-phase migration: Phase 1 uses iso for development and Dagger for CI. Phase 2 will migrate CI to iso.

### Building
- `make bin/miren` - Build the miren binary using hack/build.sh (includes version info)
- `make bin/miren-debug` - Build with debug symbols for debugging
- `make release` - Build release version using hack/build-release.sh

### Testing

**With iso (local development - recommended):**
- `make test` - Run all tests using iso (runs hack/test.sh in isolated container)
- `make test-shell` - Run tests with interactive shell (set USESHELL=1)
- `make test-e2e` - Run end-to-end tests
- `hack/it <gopkg>` - Run all tests in a package using iso
- `hack/run <gopkg> <testname>` - Run a single focused test using iso

**With Dagger (for CI compatibility):**
- `make test-dagger` - Run all tests using Dagger
- `make test-dagger-interactive` - Run tests interactively with Dagger
- `make test-shell-dagger` - Run tests with shell using Dagger
- `make test-e2e-dagger` - Run end-to-end tests with Dagger

### Development Environment

**With iso (local development - recommended):**

The dev environment uses **standalone mode** where miren manages its own containerd and buildkit internally, matching how it runs in production.

**Initial setup (once per worktree):**
- `make dev` - Start persistent dev environment, launch server, and open a shell (recommended)
- `make dev-start` - Start environment only (no server, no shell)

The dev environment automatically:
- Sets up gvisor (runsc) and kernel mounts
- Builds the miren binary and creates `/bin/m` symlink
- Generates auth config in `~/.config/miren/clientconfig.yaml`
- Prepares release directory with required binaries

When you run `make dev`, the server starts automatically in the background, so commands like `m app list` work immediately.

**Server lifecycle management:**

The miren server runs independently from your shell session:
- `make dev-server-start` - Start miren server (standalone mode)
- `make dev-server-stop` - Stop miren server
- `make dev-server-restart` - Restart server (useful after rebuilding)
- `make dev-server-status` - Check if server is running
- `make dev-server-logs` - Watch server logs

**Working in the persistent dev environment:**

The dev environment uses persistent containers, which means:
- The container and all services stay running between commands
- Each worktree gets its own isolated dev environment
- You can run commands from different terminals or LLM sessions
- The miren server runs independently and survives shell exits

Running commands:
- `make dev-shell` - Open an interactive shell
- `./hack/dev-exec <command>` - Run any command in the dev container
- Examples:
  - `./hack/dev-exec go test ./pkg/entity/...` - Run tests
  - `./hack/dev-exec m app list` - Use miren CLI
  - `make bin/miren` - Rebuild binary (then `make dev-server-restart`)

**Managing the dev environment:**
- `make dev-stop` - Stop and remove the persistent dev container
- `make dev-restart` - Restart the dev environment (stop + start)
- `make dev-status` - Check the status of the dev environment

**Typical workflow:**
```bash
# Initial setup (once per worktree)
make dev                      # Starts environment, server, and gives you a shell

# Now you're in a shell with server running - try it:
m app list                    # Works immediately!

# Development iteration
vim path/to/code.go           # Edit code
make bin/miren                # Rebuild
make dev-server-restart       # Bounce server with new code

# Debugging
make dev-server-logs          # Watch logs
make dev-server-status        # Check if running

# Multiple shells
make dev-shell                # Open another shell (from host)
./hack/dev-exec m app list    # One-off commands

# Cleanup
make dev-stop                 # Tear down environment
```

**With Dagger (for CI compatibility):**
- `make dev-dagger` - Start development environment with Dagger
- `make services-dagger` - Run services container for debugging

### Other Commands
- `make image` - Export Docker image (iso)
- `make image-dagger` - Build and import Docker image as `miren:latest` (Dagger)
- `make release-data` - Create release package tar.gz (iso)
- `make release-data-dagger` - Create release package tar.gz (Dagger)
- `make clean` - Remove built binaries

### ISO Environment
The project uses **iso** for containerized development with all dependencies provided:
- `.iso/Dockerfile` - Defines the build environment (Go 1.24, containerd, buildkit, gvisor, etc.)
- `.iso/services.yml` - Defines external service containers (MinIO for object storage)
- All default `make` targets and `hack/` scripts run inside the isolated container
- Services are automatically started and ready before commands run
- In standalone mode, miren manages etcd internally

### Dagger Environment (CI/CD)
The project also maintains **Dagger** for CI/CD:
- `dagger/main.go` - Dagger module definition
- `dagger.json` - Dagger configuration
- Use targets with `-dagger` suffix to run commands with Dagger

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

The system supports two containerized development environments:

**ISO (Recommended for local development):**
The system uses **iso** (isolated Docker environment) for containerized development with all dependencies (containerd, buildkit, gvisor, etcd) provided as services. The development container includes proper cgroup setup for gvisor compatibility.

To get started with iso:
1. Ensure `iso` is installed and available in your PATH
2. Run `make dev` or `make test` - iso will automatically start services and run commands

**Dagger (For CI/CD and compatibility):**
The system also uses **Dagger** for CI/CD with the same dependencies. Dagger configuration is in the `dagger/` directory.

To use Dagger:
1. Ensure `dagger` is installed and available in your PATH
2. Run `make test-dagger` or `make dev-dagger` - Dagger will automatically build and run containers

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
