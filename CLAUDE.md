# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Development Commands

**IMPORTANT: This project uses iso for containerized builds and testing.**

### Building
- `make bin/miren` - Build the miren binary using hack/build.sh (includes version info)
- `make bin/miren-debug` - Build with debug symbols for debugging
- `make release` - Build release version using hack/build-release.sh

### Testing

- `make test` - Run all tests using iso (runs hack/test.sh in isolated container)
- `make test-serial` - Run all tests serially with `-p 1` (for debugging test interference)
- `make test-shell` - Run tests with interactive shell (set USESHELL=1)
- `make test-blackbox` - Run blackbox CLI tests (requires `make dev` running)
- `hack/it <gopkg>` - Run all tests in a package using iso
- `hack/run <gopkg> <testname>` - Run a single focused test using iso

### Development Environment

The dev environment uses **standalone mode** where miren manages its own containerd and buildkit internally, matching how it runs in production.

**Initial setup (once per worktree):**
- `make dev` - Start persistent dev environment, launch server, and open a shell (recommended)
- `make dev-start` - Start environment only (no server, no shell)

The dev environment automatically:
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
  - `./hack/dev-exec make bin/miren` - Rebuild binary inside dev container (then `make dev-server-restart`)

**Important**: The miren binary must be built **inside** the dev container (not on the host) so it has the correct architecture. Use `./hack/dev-exec make bin/miren` instead of `make bin/miren`.

**Exception**: `go generate` and doc generation (`hack/gen-command-docs`) need host tools like `jq` that aren't in the dev container. For these, build the binary on the host with `make bin/miren` and run `go generate` on the host as well.

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
./hack/dev-exec make bin/miren # Rebuild inside container
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

**Connecting to a local Miren Cloud instance:**

The dev environment can connect to a local copy of Miren Cloud for testing the full authentication and registration flow.

Prerequisites:
- Miren Cloud running locally (typically on `http://localhost:3001`)
- The `.iso/config.yml` includes `extra_hosts` configuration for host access:
  ```yaml
  extra_hosts:
    - "miren.host:host-gateway"
  ```
  This allows containers to reach the host machine via `miren.host` instead of `localhost`.

Workflow:
```bash
# 1. Start the dev environment
make dev

# 2. Login to your local cloud (inside dev shell or via dev-exec)
m login --url http://miren.host:3001
# Follow the URL to authenticate in your browser

# 3. Register the cluster with cloud
m register -u http://miren.host:3001 -n my-dev-cluster
# Approve the registration in the browser when prompted

# 4. Restart server to activate registration
make dev-server-restart

# 5. Verify the setup
m cluster list
m app list
```

**Important notes:**
- Use `miren.host:3001` (not `localhost:3001`) from inside the dev container
- The server must be restarted after registration to load the new cluster configuration
- Registration creates `/var/lib/miren/server/registration.json` with cluster credentials
- Login creates `~/.config/miren/clientconfig.yaml` with user credentials

**With Dagger (for CI compatibility):**
- `make dev-dagger` - Start development environment with Dagger
- `make services-dagger` - Run services container for debugging

### Documentation
- `cd docs && bun install && bun run build` - Build the documentation site (Docusaurus, uses bun NOT npm)
- `cd docs && bun run start` - Start local dev server on port 3333
- Docs source lives in `docs/docs/` as Markdown files; sidebar is configured in `docs/sidebars.ts`
- **Published docs URL is `https://miren.md/`** (NOT `miren.dev/docs`). Use this when linking to docs from CLI output, error messages, or anywhere user-facing.

### Other Commands
- `make image` - Export Docker image
- `make release-data` - Create release package tar.gz
- `make clean` - Remove built binaries

### ISO Environment
The project uses **iso** for containerized development with all dependencies provided:
- `.iso/Dockerfile` - Defines the build environment (Go 1.25, containerd, buildkit, etc.)
- `.iso/services.yml` - Defines external service containers (MinIO for object storage)
- All default `make` targets and `hack/` scripts run inside the isolated container
- Services are automatically started and ready before commands run
- In standalone mode, miren manages etcd internally

## Architecture Overview

This is the **Miren Runtime** - a container orchestration system built on containerd with a custom entity system for managing applications and infrastructure.

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
- Sandboxes are isolated execution environments
- Each sandbox runs in a separate containerd container
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
- `hack/` - Build scripts and development utilities

### Development Environment Setup

The system uses **iso** (isolated Docker environment) for containerized development with all dependencies (containerd, buildkit, etcd) provided as services.

To get started with iso:
1. Ensure `iso` is installed and available in your PATH
2. Run `make dev` or `make test` - iso will automatically start services and run commands

### Testing Notes

- Tests must run without any parallelism (`-p 1`) due to shared containerd/buildkit instances
- Blackbox CLI tests in `blackbox/` directory (run via `make test-blackbox`)
- Test data in various `testdata/` directories

### Distributed Runner Development

For testing distributed runners locally, the project uses iso peers to run a coordinator and runner in separate containers on a shared network.

```bash
# Start the distributed environment (coordinator + runner1)
make dev-distributed

# Check status
make dev-distributed-status

# Open shells into peers
hack/dev-distributed shell coordinator
hack/dev-distributed shell runner1

# View logs
hack/dev-distributed logs          # coordinator
hack/dev-distributed runner-logs   # runner

# Rebuild after code changes
hack/dev-distributed rebuild

# Run the blackbox test suite against the distributed topology
make test-blackbox-distributed

# Tear down
make dev-distributed-down
```

The distributed environment uses `.iso/peers.yml` to define two peers (coordinator and runner1) and connects them to the existing services (etcd, VictoriaMetrics, VictoriaLogs). The `MIREN_LABS=distributedrunners` feature flag is set on both peers.

Blackbox tests that are specific to distributed topologies (runner list, metrics pipeline, log pipeline) live in `blackbox/distributed_runner_test.go` and self-skip when not running in peers mode.

### Saga Framework (`pkg/saga/`)

The saga framework determines action execution order via **topological sort on data dependencies** (input/output field matching). Actions at the same dependency level are sorted alphabetically. This means registration order does NOT determine execution order.

- **Prefer explicit data passing between actions** to establish ordering. Even if data is available elsewhere (e.g., from a framework dependency or context), pass it through action inputs/outputs so the saga framework infers the correct dependency graph.
- **Use `saga.Edge` only as a last resort** when there is genuinely no data to pass between two actions but ordering is still required. `Edge` fields participate in dependency resolution but carry no data at runtime.
- When adding saga tags, use explicit `saga:"key_name"` tags on both the producing output field and the consuming input/Edge field to make the dependency clear and avoid relying on default lowercased field name matching.

### Code Generation

- Entity schemas → Go structs: `entity/cmd/schemagen`
- RPC interfaces → implementations: `pkg/rpc/cmd/rpcgen`
- Generated files have `.gen.go` suffix

### CLI JSON Output Pattern (FormatOptions)

Commands that output lists or tables should support `--format json` using the `FormatOptions` pattern:

1. Embed `FormatOptions` in the opts struct
2. Check `opts.IsJSON()` before table rendering
3. Define a command-specific JSON struct with `json:"snake_case"` tags (don't serialize internal types directly)
4. Build a slice of the JSON structs and return `PrintJSON(items)`
5. Use raw/machine-readable values in JSON (e.g., RFC3339 timestamps, numeric percentages) rather than human-formatted strings

See `cli/commands/route_list.go` for a reference implementation.

### Code Style & Formatting

- **ALWAYS run `make lint` before committing** - This runs golangci-lint on the entire codebase
- Run `make lint-fix` to automatically fix issues where possible
- The codebase follows standard Go formatting conventions
- **Comment style**: Only add comments when they provide valuable context or explain "why" something is done
  - Avoid redundant comments that restate what the code does (e.g., `// Start server` above `server.Start()`)
  - Good comments explain complex logic, document assumptions, or clarify non-obvious behavior
  - Function/method comments should explain the purpose and any important side effects, not just restate the name

### Log Levels

Daemons (`miren server`, `miren runner start`) default to **Info**; one-shot CLI
commands default to **Warn**. `-v` moves one step louder from there, and
`SIGTTIN`/`SIGTTOU` retune a running process without a restart. Because Info is
what an operator actually reads on a live cluster, it has to stay worth reading.

- **Error** — the system failed at something it is responsible for. A user's app
  failing to bind its own declared port is not this; the platform handled that
  correctly.
- **Warn** — degraded, unexpected, or handled-but-suspicious. Denials belong
  here regardless of source.
- **Info** — the affirmative record of a healthy daemon: lifecycle events, state
  transitions, decisions taken, and startup confirmations. If a healthy service
  says nothing at Info, an operator cannot tell "working" from "never
  attempted."
- **Debug** — diagnostic detail for when something is already wrong.

Useful tests when picking one:

- **Would this line be identical the next thousand times it fires?** Then it is
  Debug. Info is for things that happened, not things that are.
- **Reconcile entry versus reconcile outcome.** "Processing disk update" on
  every tick is Debug; the handler saying it mounted something is Info.
- **Would an operator act on it?** If not, it is not Error or Warn. A retry loop
  that can never converge must not log at full rate forever — it drowns the tier
  it is in.
- **Never log a struct at Info.** `%+v` on an entity renders its whole field
  tree inline. Name the two or three fields that matter.
- **Audit records are exempt from `-v`** (`pkg/rpc/audit.go` pins an Info
  floor), so no verbosity default can reduce their volume. Audit volume has to
  be solved in the audit code — see `certAuthDeduper` for the pattern of
  collapsing repeats while keeping a count.
