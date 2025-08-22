# Miren Docker Support

This directory contains Docker configurations for running Miren on macOS and other non-Linux platforms.

## Quick Start

### Installation

Run the installer script:

```bash
curl -fsSL https://raw.githubusercontent.com/mirendev/cloud/main/services/installer/install.sh | bash
```

The installer will:
- Detect your operating system
- Check for Docker (on macOS)
- Install the appropriate miren version
- Set up Docker services and client configuration

### Manual Installation

If you prefer to install manually:

1. Build the Docker image:
   ```bash
   make docker-image
   ```

2. Run with docker-compose:
   ```bash
   cd docker
   docker compose up -d
   ```

3. Use miren:
   ```bash
   docker exec -it miren --help
   ```

## Architecture

The Docker setup includes:

- **Miren Container**: Contains miren server with containerd and container runtime
- **etcd**: Distributed key-value store for state management  
- **ClickHouse**: Analytics database for metrics

## Configuration

### Environment Variables

- `MIREN_DOCKER_IMAGE`: Override the Docker image (default: `miren/runtime:latest`)
- `MIREN_DATA_DIR`: Data directory on host (default: `$HOME/.miren/data`)
- `MIREN_CONFIG_DIR`: Config directory on host (default: `$HOME/.config/miren`)

### Volumes

The following directories are mounted from the host:
- Current working directory → `/workspace`
- `~/.miren/data` → `/data`
- `~/.config/miren` → `/root/.config/miren`

## Development

To build a development image:

```bash
docker build -f docker/Dockerfile.miren -t miren/runtime:dev .
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
docker ps -a | grep miren
```

Remove old container if needed:
```bash
docker rm -f miren
```

### Permission issues

The runtime container requires privileged mode to run containerd and manage containers. Ensure Docker Desktop has the necessary permissions.

### Network issues

Ensure no other services are using the required ports:
- 8080 (HTTP ingress)
- 8443 (Miren server QUIC)
- 2379 (etcd)
- 9000, 8123 (ClickHouse)