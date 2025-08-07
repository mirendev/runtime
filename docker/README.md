# Miren Runtime Docker Support

This directory contains Docker configurations for running Miren Runtime on macOS and other non-Linux platforms.

## Quick Start

### Installation

Run the installer script from the repository root:

```bash
curl -fsSL https://raw.githubusercontent.com/mirendev/runtime/main/install.sh | bash
```

The installer will:
- Detect your operating system
- Check for Docker (on macOS)
- Install the appropriate runtime version
- Set up the runtime wrapper script

### Manual Installation

If you prefer to install manually:

1. Build the Docker image:
   ```bash
   make docker-image
   ```

2. Run with docker-compose:
   ```bash
   cd docker
   docker-compose up -d
   ```

3. Use the runtime:
   ```bash
   docker exec -it miren-runtime runtime --help
   ```

## Architecture

The Docker setup includes:

- **Runtime Container**: Contains all runtime executables (containerd, buildkit, gvisor/runsc)
- **etcd**: Distributed key-value store for state management
- **ClickHouse**: Analytics database for metrics
- **MinIO**: S3-compatible object storage

## Configuration

### Environment Variables

- `MIREN_DOCKER_IMAGE`: Override the Docker image (default: `miren/runtime:latest`)
- `MIREN_DATA_DIR`: Data directory on host (default: `$HOME/.miren/data`)
- `MIREN_CONFIG_DIR`: Config directory on host (default: `$HOME/.config/runtime`)

### Volumes

The following directories are mounted from the host:
- Current working directory → `/workspace`
- `~/.miren/data` → `/data`
- `~/.config/runtime` → `/root/.config/runtime`

## Development

To build a development image:

```bash
docker build -f docker/Dockerfile.runtime -t miren/runtime:dev .
```

To run with local changes:

```bash
docker run -it --rm \
  --privileged \
  --pid host \
  -v $(pwd):/workspace \
  -v ~/.miren/data:/data \
  miren/runtime:dev \
  bash
```

## Troubleshooting

### Container won't start

Check if the container is already running:
```bash
docker ps -a | grep miren-runtime
```

Remove old container if needed:
```bash
docker rm -f miren-runtime
```

### Permission issues

The runtime container requires privileged mode to run containerd and manage containers. Ensure Docker Desktop has the necessary permissions.

### Network issues

The runtime uses host networking mode. Ensure no other services are using ports:
- 8080 (HTTP ingress)
- 2379 (etcd)
- 9000-9002 (MinIO/ClickHouse)