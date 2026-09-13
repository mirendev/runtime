#!/usr/bin/env bash
set -e

echo "=== Setting up Miren test container with systemd ==="
echo

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Get the directory of this script
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

# A locally built binary takes precedence over the published release, so a change
# that hasn't been pushed anywhere can still be tested end to end.
LOCAL_MOUNT=()
if [ -x "$REPO_ROOT/bin/miren" ]; then
    echo "Using locally built binary: $REPO_ROOT/bin/miren"
    LOCAL_MOUNT=(-v "$REPO_ROOT/bin:/miren-local:ro")
else
    echo "No bin/miren found; will install RELEASE=${RELEASE:-main} from api.miren.cloud."
    echo "Build one first with 'make bin/miren' to test local changes."
fi

# Check Docker version and cgroup setup
echo "Checking Docker environment..."
docker version --format 'Docker {{.Server.Version}}' || echo "Docker not found"

# Check if we're on Linux with proper cgroup support
if [[ "$(uname)" != "Linux" ]]; then
    echo -e "${RED}Error: systemd in Docker requires a Linux host${NC}"
    echo "On macOS/Windows, please use a Linux VM or cloud instance"
    exit 1
fi

# Clean up any existing container
echo
echo "Cleaning up any existing container..."
docker rm -f miren-systemd-test 2>/dev/null || true

# Optionally clean up the volume (uncomment to reset data between runs)
# docker volume rm miren-data 2>/dev/null || true

# Build the container
echo
echo "Building test container..."
docker build -t miren-systemd-test -f "$SCRIPT_DIR/Dockerfile.systemd" "$SCRIPT_DIR"

# Run with appropriate settings for systemd
echo
echo "Starting container with systemd..."

# Run with the right options for systemd
# The entrypoint will automatically install miren and set up the service
docker run -d \
    --name miren-systemd-test \
    --privileged \
    --cgroupns=host \
    -v /sys/fs/cgroup:/sys/fs/cgroup:rw \
    -v miren-data:/var/lib/miren \
    --tmpfs /tmp \
    --tmpfs /run \
    --tmpfs /run/lock \
    --stop-signal SIGRTMIN+3 \
    -e RELEASE="${RELEASE:-main}" \
    "${LOCAL_MOUNT[@]}" \
    -v "$SCRIPT_DIR:/hack:ro" \
    miren-systemd-test

# Wait for container to be ready (entrypoint installs miren)
echo
echo "Waiting for container setup (installing miren, configuring systemd)..."
echo "You can watch the progress with: docker logs -f miren-systemd-test"
echo
# Wait on the marker the entrypoint writes, not on the unit file. The unit exists
# well before anything has checked it, so polling for it reports success even
# when every verification failed.
setup_status=""
for i in {1..90}; do
    setup_status=$(docker exec miren-systemd-test cat /run/miren-setup-status 2>/dev/null || true)
    if [ -n "$setup_status" ]; then
        break
    fi
    echo -n "."
    sleep 2
done
echo

if [ "$setup_status" != "ok" ]; then
    if [ -z "$setup_status" ]; then
        echo -e "${RED}Container setup did not finish within the timeout${NC}"
    else
        echo -e "${RED}Container setup failed verification${NC}"
    fi
    echo
    echo "Setup log:"
    docker logs miren-systemd-test 2>&1 | tail -40
    echo
    echo "The container is still running for inspection:"
    echo "  docker exec -it miren-systemd-test bash"
    exit 1
fi

echo -e "${GREEN}✓ Miren service is installed and verified${NC}"

# Check final status
echo
if docker exec miren-systemd-test systemctl status --no-pager 2>/dev/null | head -5; then
    echo
    echo -e "${GREEN}=== Test container is ready ===${NC}"
    echo
    echo "To enter the container:"
    echo "  docker exec -it miren-systemd-test bash"
    echo
    echo "Miren is automatically installed with systemd service configured!"
    echo "  Current branch: ${RELEASE:-main}"
    echo
    echo "Commands to test:"
    echo "  miren version                             # Check current version"  
    echo "  systemctl start miren                     # Start the server"
    echo "  systemctl status miren                    # Check service status"
    echo "  journalctl -u miren -f                    # View logs"
    echo
    echo "Test the memory limit end to end:"
    echo "  docker exec -it miren-systemd-test /hack/test-oom-restart.sh"
    echo
    echo "Inspect the resource limits systemd resolved:"
    echo "  docker exec miren-systemd-test systemctl show -p MemoryMax -p MemoryHigh -p MemorySwapMax miren"
    echo
    echo "Test upgrade commands:"
    echo "  miren upgrade --check                     # Check for updates"
    echo "  miren upgrade --version main --force      # Upgrade CLI to main"
    echo "  sudo miren server upgrade --version main  # Upgrade running server"
    echo "  sudo miren server upgrade rollback        # Rollback to previous"
    echo
    echo "To check systemd services:"
    echo "  docker exec miren-systemd-test systemctl status"
    echo
    echo "To stop and remove:"
    echo "  docker rm -f miren-systemd-test"
    echo "  docker volume rm miren-data  # To also remove persistent data"
else
    echo -e "${RED}Failed to start systemd in container${NC}"
    echo "Check logs with: docker logs miren-systemd-test"
    exit 1
fi
