#!/bin/sh

# This script is executed in the runtime environment of the application

if ! which docker > /dev/null; then
    echo "Docker is not installed. Please install Docker and try again."
    exit 1
fi

if test -n "$GLOBAL"; then
    DIR=/var/lib/runtime
    BINDIR=/usr/local/bin
    echo "Setting up global environment in $DIR (bindir: $BINDIR)"
else
    DIR=~/runtime
    BINDIR=~/bin
    echo "Setting up local environment in $DIR (bindir: $BINDIR)"
fi

if test -f "$DIR/compose.yaml"; then
    echo "Runtime is already installed in $DIR. Exiting."
    exit 1
fi

mkdir -p "$DIR"

cd "$DIR"

cat >> compose.yaml <<EOF
name: runtime
services:
  runtime:
    image: ghcr.io/mirendev/runtime:latest
    ports:
      - 443:443
      - 80:80
      - 8443:8443/udp
    privileged: true
    labels:
      computer.runtime.cluster: "local"
    volumes:
      - containerd:/var/lib/containerd
      - runtime:/var/lib/runtime
    restart: always
    depends_on:
      clickhouse:
        condition: service_healthy
      postgres:
        condition: service_healthy

  clickhouse:
    image: clickhouse/clickhouse-server:latest
    environment:
      - CLICKHOUSE_DEFAULT_ACCESS_MANAGEMENT=1
      - CLICKHOUSE_PASSWORD=default
    volumes:
      - clickhouse:/var/lib/clickhouse
    restart: always
    healthcheck:
      # Can just use 'db' as the host since we're inside the container network
      test: ["CMD", "clickhouse-client", "--host", "clickhouse", "--query", "SELECT 1"]
      interval: 1s
      timeout: 5s
      retries: 5

  postgres:
    image: postgres:17
    environment:
      - POSTGRES_DB=runtime_prod
      - POSTGRES_PASSWORD=runtime
      - POSTGRES_USER=runtime
    volumes:
      - postgres:/var/lib/postgresql/data
    restart: always
    healthcheck:
      test: ["CMD-SHELL", "PGPASSWORD=runtime pg_isready -U runtime -d runtime_prod"]
      interval: 1s
      timeout: 5s
      retries: 5

volumes:
  clickhouse:
    # ok
  postgres:
    # ok
  containerd:
    # ok
  runtime:
    # ok
EOF

echo "Installation booting..."

docker compose up -d

echo ""
echo "Downloading CLI..."

if test "$(uname)" = "Linux"; then
    if test "$(uname -m)" = "x86_64"; then
        curl -sL "https://releases.miren.dev/runtime/mvp/runtime-linux-amd64.zip" -o runtime.zip
    elif test "$(uname -m)" = "aarch64"; then
        curl -sL "https://releases.miren.dev/runtime/mvp/runtime-linux-arm64.zip" -o runtime.zip
    else
        echo "Unsupported architecture"
        exit 1
    fi
elif test "$(uname)" = "Darwin"; then
    if test "$(uname -m)" = "arm64"; then
        curl -sL "https://releases.miren.dev/runtime/mvp/runtime-darwin-arm64.zip" -o runtime.zip
    elif test "$(uname -m)" = "x86_64"; then
        curl -sL "https://releases.miren.dev/runtime/mvp/runtime-darwin-amd64.zip" -o runtime.zip
    else
        echo "Unsupported architecture"
        exit 1
    fi
else
    echo "Unsupported OS"
    exit 1
fi

echo "Installing runtime CLI into $BINDIR..."
mkdir -p "$BINDIR"

unzip -q -o runtime.zip -d "$BINDIR"

echo "Configuring CLI to talk to local runtime..."
"${BINDIR}/runtime" setup

echo ""
echo "To uninstall, run 'docker compose down -v' in $DIR"
echo ""

if ! which runtime > /dev/null; then
    echo "WARNING: $BINDIR is not in your PATH. Please add it to your shell profile."
    echo "Ready! Run '$BINDIR/runtime' to get started."
else
    echo "Ready! Run 'runtime' to get started."
fi
