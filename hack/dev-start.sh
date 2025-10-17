#!/usr/bin/env bash
set -e

cd "$(dirname "$0")/.."

# Ensure services are running
echo "Starting services..."
docker compose up -d

# Build dev image if needed
echo "Building dev image..."
docker build -t miren-dev:latest -f hack/Dockerfile.dev .

# Start dev container if not already running
if ! docker ps -q -f name=miren-dev | grep -q .; then
    echo "Starting dev container..."
    docker run -d \
        --name miren-dev \
        --privileged \
        --network miren-dev \
        -v "$PWD:/src" \
        -v miren-go-mod:/go/pkg/mod \
        -v miren-go-build:/go/build-cache \
        -v miren-containerd:/data \
        -p 2345:2345 \
        -e USE_TMUX="${USE_TMUX:-}" \
        miren-dev:latest \
        /src/hack/dev-daemon.sh

    # Wait a moment for initialization
    echo "Waiting for dev environment to initialize..."
    sleep 3
fi

# If "shell" argument provided, attach to shell
if [ "$1" = "shell" ]; then
    echo "Attaching to dev container..."
    docker exec -it miren-dev bash
fi
