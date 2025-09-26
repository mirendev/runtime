#!/bin/bash
set -e

# Function for logging with timestamps
log() {
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] $1"
}

# Setup function that installs miren and configures systemd
setup_miren() {
    # Wait for systemd to be ready
    log "Waiting for systemd to initialize..."
    for i in {1..60}; do
        if systemctl is-system-running 2>/dev/null | grep -qE "running|degraded"; then
            log "Systemd is ready"
            break
        fi
        sleep 1
    done

    # Install Miren (mirroring production setup)
    log "Installing Miren..."

    # Create the release directory structure
    MIREN_RELEASE_DIR="/var/lib/miren/release"
    log "Creating Miren release directory at $MIREN_RELEASE_DIR..."
    mkdir -p "$MIREN_RELEASE_DIR"

    # Install Miren to the release directory
    log "Downloading Miren installer..."
    export HOME="/root"
    export RELEASE="${RELEASE:-main}"  # Default to main branch (installer uses RELEASE or MIREN_VERSION)
    export BINDIR="$MIREN_RELEASE_DIR"

    if ! curl -fsSL --connect-timeout 10 --max-time 120 https://api.miren.cloud/install -o /tmp/miren-install.sh; then
        log "ERROR: Failed to download install script"
        exit 1
    fi

    log "Running Miren installer to $MIREN_RELEASE_DIR (branch: $RELEASE)..."
    if ! bash /tmp/miren-install.sh; then
        log "ERROR: Miren installation failed"
        exit 1
    fi

    # Verify miren binary exists
    if [ ! -f "$MIREN_RELEASE_DIR/miren" ]; then
        log "ERROR: Miren binary not found at $MIREN_RELEASE_DIR/miren"
        exit 1
    fi
    log "Miren binary installed at $MIREN_RELEASE_DIR/miren"

    # Create symlink for PATH access
    log "Creating symlink from /usr/local/bin/miren to $MIREN_RELEASE_DIR/miren..."
    ln -sf "$MIREN_RELEASE_DIR/miren" /usr/local/bin/miren

    # Verify version
    log "Miren version: $(miren version || echo 'Unable to get version')"

    # Create systemd service (matching production)
    log "Creating systemd service for miren..."
    cat >/etc/systemd/system/miren.service <<EOF
[Unit]
Description=Miren Service
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
Environment="NO_COLOR=1"
ExecStart=/var/lib/miren/release/miren server -vv --address=0.0.0.0:8443 --serve-tls
Restart=always
RestartSec=10
StandardOutput=journal
StandardError=journal
SyslogIdentifier=miren
User=root
WorkingDirectory=/var/lib/miren/release
KillMode=process
TimeoutStopSec=90s

[Install]
WantedBy=multi-user.target
EOF

    # Reload systemd and enable the service
    log "Enabling miren service..."
    systemctl daemon-reload
    systemctl enable miren.service

    # Don't start automatically - let user control when to start
    log "Miren service installed but not started"
    log "To start the service: systemctl start miren"
    log "To check status: systemctl status miren"

    # Keep container ready
    log "Container ready for testing"
}

# Check if we are PID 1
if [ $$ -eq 1 ]; then
    # We are PID 1, run setup in background then exec systemd
    log "Running as PID 1, setting up environment..."
    setup_miren &
    exec /sbin/init
else
    # Not PID 1, just run setup
    setup_miren
    # Keep container running
    tail -f /dev/null
fi