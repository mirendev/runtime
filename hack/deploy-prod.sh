#!/usr/bin/env bash
# Fail fast on unset vars and broken pipelines, not just non-zero exits
set -euo pipefail

# Configuration
HOST="miren.cloud"
# Use an absolute, unique temp file on the remote host to avoid tilde-expansion and collisions
REMOTE_TEMP_PATH="/tmp/miren.$(date +%s).$$"
INSTALL_PATH="/var/lib/miren/release/miren"
SERVICE_NAME="miren"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Function to print colored output
print_step() {
    echo -e "${GREEN}==>${NC} $1"
}

print_error() {
    echo -e "${RED}ERROR:${NC} $1" >&2
}

print_warning() {
    echo -e "${YELLOW}WARNING:${NC} $1"
}

# Check if we're in the right directory
if [ ! -f "Makefile" ] || [ ! -d "hack" ]; then
    print_error "Must be run from the root of the runtime repository"
    exit 1
fi

# Ensure ssh is available
if ! command -v ssh >/dev/null 2>&1; then
    print_error "ssh command not found"
    exit 1
fi

# Ensure scp is available
if ! command -v scp >/dev/null 2>&1; then
    print_error "scp command not found"
    exit 1
fi

# Parse command line arguments
SKIP_BUILD=false
FOLLOW_LOGS=false
FORCE=false

while [[ $# -gt 0 ]]; do
    case $1 in
    --skip-build)
        SKIP_BUILD=true
        shift
        ;;
    --follow-logs | -f)
        FOLLOW_LOGS=true
        shift
        ;;
    --force)
        FORCE=true
        shift
        ;;
    --help | -h)
        echo "Usage: $0 [OPTIONS]"
        echo ""
        echo "Options:"
        echo "  --skip-build     Skip building the distribution binary"
        echo "  --follow-logs    Follow service logs after deployment"
        echo "  -f               Alias for --follow-logs"
        echo "  --force          Force deployment even if not on clean main branch"
        echo "  --help, -h       Show this help message"
        exit 0
        ;;
    *)
        print_error "Unknown option: $1"
        echo "Use --help for usage information"
        exit 1
        ;;
    esac
done

# Git safety checks
if [ "$FORCE" = false ]; then
    print_step "Performing git safety checks..."

    # Check if we're on main branch
    CURRENT_BRANCH=$(git branch --show-current)
    if [ "$CURRENT_BRANCH" != "main" ]; then
        print_error "Not on main branch (currently on: $CURRENT_BRANCH)"
        print_error "Production deployments should only be done from the main branch"
        print_error "Use --force to override this check"
        exit 1
    fi

    # Check for uncommitted changes
    if ! git diff-index --quiet HEAD --; then
        print_error "Uncommitted changes detected"
        print_error "Production deployments should only be done from a clean checkout"
        print_error "Use --force to override this check"
        git status --short
        exit 1
    fi

    # Check for untracked files (excluding common ignored patterns)
    UNTRACKED=$(git ls-files --others --exclude-standard)
    if [ "$UNTRACKED" != "" ]; then
        print_error "Untracked files detected:"
        echo "$UNTRACKED"
        print_error "Production deployments should only be done from a clean checkout"
        print_error "Use --force to override this check"
        exit 1
    fi

    # Fetch latest changes and check if we're up to date with origin/main
    print_step "Fetching latest changes from origin..."
    if ! git fetch origin main; then
        print_error "Failed to fetch from origin"
        exit 1
    fi

    LOCAL_COMMIT=$(git rev-parse HEAD)
    REMOTE_COMMIT=$(git rev-parse origin/main)

    if [ "$LOCAL_COMMIT" != "$REMOTE_COMMIT" ]; then
        print_error "Local main is not up to date with origin/main"
        print_error "Local:  $LOCAL_COMMIT"
        print_error "Remote: $REMOTE_COMMIT"
        print_error "Please pull the latest changes or use --force to override"
        exit 1
    fi

    print_step "Git safety checks passed ✓"
else
    print_warning "Skipping git safety checks (--force specified)"
fi

# Step 1: Build the distribution binary
if [ "$SKIP_BUILD" = false ]; then
    print_step "Building distribution binary..."
    if ! make dist; then
        print_error "Failed to build distribution binary"
        exit 1
    fi
else
    print_warning "Skipping build step"
fi

# Verify the binary exists
if [ ! -f "dist/miren-dist" ]; then
    print_error "Distribution binary not found at dist/miren-dist"
    print_error "Run 'make dist' to build it"
    exit 1
fi

# Step 2: Copy binary to production server
print_step "Copying binary to production server..."
if ! scp dist/miren-dist "$HOST:$REMOTE_TEMP_PATH"; then
    print_error "Failed to copy binary to server"
    exit 1
fi

# Step 3: Deploy on server
print_step "Deploying on server..."

# Create deployment script
DEPLOY_SCRIPT=$(
    cat <<'EOF'
set -euo pipefail

# Always clean up the remote temp file on exit (success or failure)
trap 'rm -f "${REMOTE_TEMP_PATH}"' EXIT

# Stop the service
echo "Stopping ${SERVICE_NAME} service..."
sudo systemctl stop "${SERVICE_NAME}" || true

# Backup current binary if it exists
if [ -f "${INSTALL_PATH}" ]; then
    echo "Backing up current binary..."
    sudo cp "${INSTALL_PATH}" "${INSTALL_PATH}.backup.$(date +%Y%m%d_%H%M%S)"
fi

# Install new binary
echo "Ensuring install directory exists..."
sudo install -d -o root -g root -m 0755 "$(dirname "${INSTALL_PATH}")"
echo "Installing new binary..."
sudo install -o root -g root -m 0755 "${REMOTE_TEMP_PATH}" "${INSTALL_PATH}"

# Start the service
echo "Starting ${SERVICE_NAME} service..."
sudo systemctl start "${SERVICE_NAME}"

# Check status
echo "Checking service status..."
if sudo systemctl is-active --quiet "${SERVICE_NAME}"; then
    echo "Service started successfully"
    sudo systemctl status "${SERVICE_NAME}" --no-pager
else
    echo "Service failed to start; attempting rollback..."
    sudo systemctl status "${SERVICE_NAME}" --no-pager || true
    # Find the latest timestamped backup, if any
    LATEST_BACKUP="$(ls -1t "${INSTALL_PATH}".backup.* 2>/dev/null | head -n1 || true)"
    if [ -n "${LATEST_BACKUP}" ]; then
        echo "Restoring backup: ${LATEST_BACKUP}"
        sudo install -o root -g root -m 0755 "${LATEST_BACKUP}" "${INSTALL_PATH}"
        echo "Restarting ${SERVICE_NAME} after rollback..."
        sudo systemctl restart "${SERVICE_NAME}" || true
        if sudo systemctl is-active --quiet "${SERVICE_NAME}"; then
            echo "Rollback successful - service is running with previous version"
            sudo systemctl status "${SERVICE_NAME}" --no-pager
        else
            echo "Rollback failed - service still not running"
        fi
    else
        echo "No backup found to rollback to."
    fi
    exit 1
fi
EOF
)

# Execute deployment script on server
if ! ssh "$HOST" "SERVICE_NAME=${SERVICE_NAME} INSTALL_PATH=${INSTALL_PATH} REMOTE_TEMP_PATH=${REMOTE_TEMP_PATH} bash -s" <<<"$DEPLOY_SCRIPT"; then
    print_error "Deployment failed"
    exit 1
fi

print_step "Deployment completed successfully!"

# Optional: Follow logs
if [ "$FOLLOW_LOGS" = true ]; then
    print_step "Following service logs (Ctrl+C to stop)..."
    ssh "$HOST" "sudo journalctl -u ${SERVICE_NAME} -f"
fi
