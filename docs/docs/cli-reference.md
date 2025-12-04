---
sidebar_position: 3
---

# CLI Reference

The Miren CLI (`miren`) provides commands for managing applications and deployments.

## Core Commands

### Deployment

- `miren deploy` - Deploy the current project
- `miren init` - Initialize a new application

### Server Install

- `miren server install` - Usually run via sudo, setup the server with systemd
- `miren server uninstall` - Run as root, remove the global server install
- `miren server docker install` - Setup a server in the local docker
- `miren server docker uninstall` - Remove the server running within docker

### Application Management

- `miren app` - Get information about an application
- `miren app list` (or `miren apps`) - List all applications
- `miren app delete` - Delete an application and all its resources
- `miren app history` - Show deployment history for an application
- `miren app status` - Show current status of an application

### Logs & Monitoring

- `miren logs` - Get logs for an application
- `miren route` - List all HTTP routes

### Environment & Configuration

- `miren env` - Environment variable management commands
- `miren config` - Configuration file management

### Cluster & Authentication

- `miren cluster` - List configured clusters
- `miren cluster list` - List all configured clusters
- `miren cluster add` - Add a new cluster configuration
- `miren cluster remove` - Remove a cluster from the configuration
- `miren cluster switch` - Switch to a different cluster
- `miren login` - Authenticate with miren.cloud
- `miren server register` - Register this cluster with miren.cloud
- `miren whoami` - Display information about the current authenticated user

### Advanced Commands

- `miren sandbox list` - List all sandboxes
- `miren sandbox exec` - Execute a command in a sandbox
- `miren sandbox stop` - Stop a sandbox
- `miren sandbox delete` - Delete a dead sandbox
- `miren sandbox metrics` - Get metrics from a sandbox
- `miren debug entity list` - List entities
- `miren debug entity get` - Get an entity
- `miren debug entity delete` - Delete an entity
- `miren debug entity put` - Put an entity

### Utility Commands

- `miren version` - Print the version
- `miren upgrade` - Upgrade miren CLI to latest version
- `miren server` - Start the miren server

## Global Flags

These flags are available for most commands:

- `-v, --verbose` - Enable verbose output
- `--server-address` - Server address to connect to
- `--config` - Path to the config file
- `-C, --cluster` - Cluster name
- `-a, --app` - Application name
- `-d, --dir` - Directory to run from

## Configuration

The CLI reads configuration from `~/.config/miren/clientconfig.yaml`:

```yaml
server: http://localhost:8080
auth:
  token: <your-auth-token>
```

## Authentication

### Login to Miren Cloud

```bash
miren login
```

### Install a Server on Linux

```bash
miren server install -n my-cluster
```

### Install a Server inside Docker

```bash
miren server docker install -n my-cluster
```
## Output Formats

Many commands support different output formats:

```bash
# Table format (default)
miren app list

# JSON format
miren app list --format json
```

## Next Steps

- [Getting Started](/getting-started) - Learn by deploying
- [App Commands](/cli/app) - Manage your applications
