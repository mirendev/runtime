#!/usr/bin/env bash
# Fail fast on unset vars and broken pipelines, not just non-zero exits
set -euo pipefail

# Configuration
ZONE="us-central1-a"
PROJECT="miren-deployment"
INSTANCE="runtime-sandbox"
REMOTE_TEMP_PATH="~/runtime"
INSTALL_PATH="/usr/local/bin/runtime"
SERVICE_NAME="miren-runtime"

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

# Ensure gcloud CLI is present
if ! command -v gcloud >/dev/null 2>&1; then
    print_error "gcloud CLI not found – install & run 'gcloud auth login' first"
    exit 1
fi

# Parse command line arguments
SKIP_BUILD=false
FOLLOW_LOGS=false

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
    --help | -h)
        echo "Usage: $0 [OPTIONS]"
        echo ""
        echo "Options:"
        echo "  --skip-build     Skip building the distribution binary"
        echo "  --follow-logs    Follow service logs after deployment"
        echo "  -f               Alias for --follow-logs"
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
if [ ! -f "dist/runtime-dist" ]; then
    print_error "Distribution binary not found at dist/runtime-dist"
    print_error "Run 'make dist' to build it"
    exit 1
fi

# Step 2: Copy binary to sandbox server
print_step "Copying binary to sandbox server..."
if ! gcloud compute scp dist/runtime-dist ${INSTANCE}:${REMOTE_TEMP_PATH} \
    --zone="$ZONE" \
    --tunnel-through-iap \
    --project="$PROJECT"; then
    print_error "Failed to copy binary to server"
    exit 1
fi

# Step 3: Deploy on server
print_step "Deploying on server..."

# Create deployment script
DEPLOY_SCRIPT=$(
    cat <<'EOF'
set -e

# Stop the service
echo "Stopping ${SERVICE_NAME} service..."
sudo systemctl stop "${SERVICE_NAME}" || true

# Backup current binary if it exists
if [ -f "${INSTALL_PATH}" ]; then
    echo "Backing up current binary..."
    sudo cp "${INSTALL_PATH}" "${INSTALL_PATH}.backup.$(date +%Y%m%d_%H%M%S)"
fi

# Install new binary
echo "Installing new binary..."
sudo cp "${REMOTE_TEMP_PATH}" "${INSTALL_PATH}"
sudo chmod +x "${INSTALL_PATH}"

# Start the service
echo "Starting ${SERVICE_NAME} service..."
sudo systemctl start "${SERVICE_NAME}"

# Check status
echo "Checking service status..."
sudo systemctl is-active --quiet "${SERVICE_NAME}"
if [ $? -eq 0 ]; then
    echo "Service started successfully"
    sudo systemctl status "${SERVICE_NAME}" --no-pager
else
    echo "Service failed to start"
    sudo systemctl status "${SERVICE_NAME}" --no-pager
    exit 1
fi

# Clean up temporary file
rm -f "${REMOTE_TEMP_PATH}"
EOF
)

# Execute deployment script on server
if ! gcloud compute ssh "$INSTANCE" \
    --zone="$ZONE" \
    --tunnel-through-iap \
    --project="$PROJECT" \
    --command="SERVICE_NAME=${SERVICE_NAME} INSTALL_PATH=${INSTALL_PATH} REMOTE_TEMP_PATH=${REMOTE_TEMP_PATH} bash -s" <<<"$DEPLOY_SCRIPT"; then
    print_error "Deployment failed"
    exit 1
fi

print_step "Deployment completed successfully!"

# Optional: Follow logs
if [ "$FOLLOW_LOGS" = true ]; then
    print_step "Following service logs (Ctrl+C to stop)..."
    gcloud compute ssh "$INSTANCE" \
        --zone="$ZONE" \
        --tunnel-through-iap \
        --project="$PROJECT" \
        --command="sudo journalctl -u ${SERVICE_NAME} -f"
fi
