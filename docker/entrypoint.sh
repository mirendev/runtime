#!/bin/bash
set -e

# Start containerd
echo "Starting containerd..."
containerd --config /etc/containerd/config.toml \
    --root /var/lib/miren/containerd \
    --state /var/lib/miren/containerd/state &
CONTAINERD_PID=$!

# Wait for containerd to be ready
sleep 2


# Function to cleanup on exit
cleanup() {
    echo "Shutting down services..."
    kill $CONTAINERD_PID 2>/dev/null || true
    wait $CONTAINERD_PID 2>/dev/null || true
}

# Trap exit signals
trap cleanup EXIT INT TERM

# Wait for external services to be ready
echo "Waiting for external services..."
while ! nc -z etcd 2379; do
    echo "Waiting for etcd..."
    sleep 1
done

echo "External services are ready!"

# Execute command or start miren server
if [ $# -eq 0 ]; then
    echo "Starting miren server..."
    miren server -vv --mode=distributed -a 0.0.0.0:8443 &
    SERVER_PID=$!

    # Wait for server to be ready
    echo "Waiting for miren server to start..."
    while ! nc -zu localhost 8443; do
        sleep 1
    done
    echo "Miren server is ready!"

    # Generate auth config
    echo "Generating miren client configuration..."
    mkdir -p /root/.config/miren
    echo "DEBUG: About to run auth generation"
    echo "DEBUG: Config file before: $(ls -la /root/.config/miren/clientconfig.yaml 2>/dev/null || echo "does not exist")"
    if ! miren auth generate -c /root/.config/miren/clientconfig.yaml -C local -t localhost:8443 -v; then
        echo "ERROR: Failed to generate client configuration"
        kill $SERVER_PID 2>/dev/null || true
        exit 1
    fi
    echo "DEBUG: Config file after: $(ls -la /root/.config/miren/clientconfig.yaml)"
    echo "DEBUG: Config file content size: $(wc -c < /root/.config/miren/clientconfig.yaml 2>/dev/null || echo 0)"
    # Copy config to accessible location for installation script
    cp /root/.config/miren/clientconfig.yaml /tmp/clientconfig.yaml 2>/dev/null || echo "Failed to copy config"
    echo "DEBUG: Copied config to /tmp/clientconfig.yaml ($(wc -c < /tmp/clientconfig.yaml 2>/dev/null || echo 0) bytes)"
    echo "Client configuration generated successfully"

    # Wait for server to finish
    wait $SERVER_PID
else
    # Execute provided command
    exec "$@"
fi
