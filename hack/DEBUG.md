# Debugging Guide

This guide explains how to debug the Miren Runtime locally with your IDE while using containerized services.

## Overview

The runtime can be debugged locally while connecting to services (etcd, MinIO, ClickHouse) running in a Dagger container. This setup allows you to:
- Use your local IDE debugger (e.g., GoLand, VS Code)
- Make code changes without rebuilding containers
- Debug with breakpoints and step through code
- Keep services isolated in containers

## Quick Start

1. **Start the runtime's dependencies in a container:**
   ```bash
   make services
   ```
   This starts a Dagger container with:
   - etcd (distributed key-value store for entity storage)
   - MinIO (S3-compatible object storage)
   - ClickHouse (analytics database)
   
   These services run inside the container but will be made accessible to your local runtime.

2. **Set up local environment:**
   ```bash
   source ./hack/forward-services.sh
   ```
   This script:
   - Auto-detects service IPs from the container
   - Sets up port forwarding using socat
   - Exports required environment variables
   - Builds the runtime binary if needed
   - Creates the `r` alias for the runtime command

3. **Run the runtime locally:**
   ```bash
   r server -vv --etcd=localhost:2379 --clickhouse-addr=localhost:9000
   ```
   Your local runtime now connects to the containerized services.

## Detailed Setup

### Prerequisites

- Docker with Dagger engine running
- Go development environment
- `socat` installed (`apt install socat` or `brew install socat`)
- `/etc/hosts` entry: `127.0.0.1 etcd` (required for etcd DNS resolution)

### Service Endpoints

After running the port forwarding script, services are available at:
- **MinIO**: `localhost:9001` (S3 API)
- **etcd**: `localhost:2379`
- **ClickHouse**: 
  - Native protocol: `localhost:9000`
  - HTTP interface: `localhost:8123`

### Environment Variables

The following environment variables are automatically exported when you source the script:
- `S3_URL=http://localhost:9001`
- `ETCD_ENDPOINTS=localhost:2379`
- `CLICKHOUSE_URL=http://localhost:8123`

### IDE Configuration

#### GoLand

1. Create a new "Go Application" run configuration
2. Set the following:
   - **Package path**: `miren.dev/runtime/cmd/miren`
   - **Program arguments**: `dev -vv --data-path=/var/lib/runtime/data --etcd=localhost:2379 --clickhouse-addr=localhost:9000`
   - **Environment variables**:
     - `S3_URL=minio://admin123:admin123@localhost:9001/buckets`
     - `ETCD_ENDPOINTS=http://localhost:2379`
     - `CLICKHOUSE_URL=clickhouse://localhost:9000`
   - **Run with sudo**: Yes (required for sandbox operations)

#### VS Code

Add to `.vscode/launch.json`:
```json
{
    "version": "0.2.0",
    "configurations": [
        {
            "name": "Debug Runtime Dev",
            "type": "go",
            "request": "launch",
            "mode": "debug",
            "program": "${workspaceFolder}/cmd/miren",
            "args": [
                "server",
                "-vv",
                "--data-path=/var/lib/runtime/data",
                "--etcd=localhost:2379",
                "--clickhouse-addr=localhost:9000"
            ],
            "env": {
                "S3_URL": "minio://admin123:admin123@localhost:9001/buckets",
                "ETCD_ENDPOINTS": "http://localhost:2379",
                "CLICKHOUSE_URL": "clickhouse://localhost:9000"
            },
            "console": "integratedTerminal",
            "asRoot": true
        }
    ]
}
```

## Troubleshooting

### Port Forwarding Issues

If automatic IP detection fails, you can manually specify service IPs:
```bash
MINIO_IP=10.87.x.x ETCD_IP=10.87.x.x CLICKHOUSE_IP=10.87.x.x ./hack/forward-services.sh
```

To find the correct IPs, check the container logs or run:
```bash
docker exec dagger-engine-v0.18.9 ps aux | grep -E "bash.*(run-services|dev.sh)"
```

### DNS Resolution Errors

If you see "lookup etcd: Temporary failure in name resolution":
1. Add to `/etc/hosts`: `127.0.0.1 etcd`
2. Restart your IDE or debugging session

### Permission Errors

The runtime requires sudo for sandbox operations. Ensure:
- Your IDE run configuration has "Run with sudo" enabled
- You have sudo permissions without a password prompt for debugging

## Architecture Notes

### Service Separation

The debug setup runs services in containers while the runtime runs locally. This allows:
- Fast iteration without container rebuilds
- Direct debugging with IDE tools
- Isolated service dependencies

### Port Forwarding

The `forward-services.sh` script uses `socat` to forward local ports to container services through the Docker daemon. This avoids complex networking setup while maintaining service isolation.