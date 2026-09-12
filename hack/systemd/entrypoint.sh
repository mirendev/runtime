#!/bin/bash
set -e

# Function for logging with timestamps
log() {
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] $1"
}

# SETUP_MARKER tells run-systemd-test.sh how setup finished. Without it the run
# script can only see that a unit file exists — which is true even when every
# check below failed, because the unit is written before anything verifies it.
SETUP_MARKER=/run/miren-setup-status

# verify_service_setup checks that the unit systemd actually loaded carries the
# directives we expect. The harness used to only check that a unit existed,
# which is why a stale unit went unnoticed for so long.
verify_service_setup() {
    local failed=0

    log "Verifying the loaded unit..."

    # Resource limits arrive in a drop-in rather than the unit itself, so that
    # they can also be refreshed on upgrade. Check the resolved values, which is
    # what systemd will actually enforce.
    local mem_max mem_high swap_max stop_post
    mem_max=$(systemctl show -p MemoryMax --value miren.service)
    mem_high=$(systemctl show -p MemoryHigh --value miren.service)
    swap_max=$(systemctl show -p MemorySwapMax --value miren.service)
    stop_post=$(systemctl show -p ExecStopPost --value miren.service)

    log "  MemoryMax=$mem_max MemoryHigh=$mem_high MemorySwapMax=$swap_max"

    if [ "$mem_max" = "infinity" ] || [ -z "$mem_max" ]; then
        log "  FAIL: no MemoryMax set — a runaway control process could take the host down"
        failed=1
    fi
    if [ "$mem_high" = "infinity" ] || [ -z "$mem_high" ]; then
        log "  FAIL: no MemoryHigh set — the cgroup would be killed without reclaiming first"
        failed=1
    fi

    # Checking only for "infinity" would accept a nonsense finite value like
    # MemoryMax=1, which is arguably worse than no limit at all. These assert the
    # invariants the policy has to satisfy rather than re-deriving the policy
    # itself, since a second copy of the formula in shell is exactly the kind of
    # drift that left this harness testing a stale unit for months.
    local mem_total
    mem_total=$(awk '/^MemTotal:/ {print $2 * 1024}' /proc/meminfo)
    if [ -n "$mem_total" ] && [ "$mem_max" != "infinity" ] && [ -n "$mem_max" ]; then
        if [ "$mem_max" -lt $((1024 * 1024 * 1024)) ]; then
            log "  FAIL: MemoryMax is $mem_max, under 1 GB — the control plane cannot run in that"
            failed=1
        fi
        if [ "$mem_max" -gt "$mem_total" ]; then
            log "  FAIL: MemoryMax is $mem_max, above MemTotal $mem_total — the cap can never be reached"
            failed=1
        fi
    fi
    if [ "$mem_high" != "infinity" ] && [ -n "$mem_high" ] && [ "$mem_max" != "infinity" ] && [ -n "$mem_max" ]; then
        if [ "$mem_high" -ge "$mem_max" ]; then
            log "  FAIL: MemoryHigh ($mem_high) is not below MemoryMax ($mem_max) — no reclaim before the kill"
            failed=1
        fi
    fi
    if [ "$swap_max" != "0" ]; then
        log "  FAIL: MemorySwapMax is '$swap_max', expected 0 — swap thrash is what kills a host"
        failed=1
    fi
    if ! echo "$stop_post" | grep -q "record-exit"; then
        log "  FAIL: no record-exit hook — an out-of-memory restart would go unreported"
        failed=1
    fi
    # The "-" prefix in the unit shows up here as ignore_errors=yes. Without it,
    # a hook that cannot run fails the unit's stop.
    if ! echo "$stop_post" | grep -q "ignore_errors=yes"; then
        log "  FAIL: record-exit hook is not marked ignore-errors — a failed hook would fail the stop"
        failed=1
    fi

    # ExecReload is what makes `systemctl reload miren` a soft restart that
    # preserves disk mounts. The old hand-copied unit lost it.
    if ! systemctl show -p ExecReload --value miren.service | grep -q USR1; then
        log "  FAIL: unit has no ExecReload"
        failed=1
    fi

    if [ "$failed" -ne 0 ]; then
        log "Unit verification FAILED"
        systemctl cat miren.service || true
        return 1
    fi

    log "  OK: unit and resource limits verified"
    return 0
}

# finish records the outcome where the run script can find it, then exits with
# that status.
finish() {
    local status="$1" message="$2"
    echo "$status" >"$SETUP_MARKER"
    log "$message"
    [ "$status" = "ok" ] || exit 1
}

# Setup function that installs miren and configures systemd
setup_miren() {
    rm -f "$SETUP_MARKER"

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

    export HOME="/root"
    export RELEASE="${RELEASE:-main}"  # Default to main branch (installer uses RELEASE or MIREN_VERSION)
    export BINDIR="$MIREN_RELEASE_DIR"

    # A binary bind-mounted at /miren-local takes precedence, so a change that
    # hasn't been pushed anywhere can still be tested. Otherwise fall back to the
    # published installer for $RELEASE.
    if [ -x /miren-local/miren ]; then
        log "Installing locally built Miren from /miren-local/miren..."
        cp /miren-local/miren "$MIREN_RELEASE_DIR/miren"
        chmod +x "$MIREN_RELEASE_DIR/miren"
    else
        log "Downloading Miren installer..."
        if ! curl -fsSL --connect-timeout 10 --max-time 120 https://api.miren.cloud/install -o /tmp/miren-install.sh; then
            finish "fail" "ERROR: Failed to download install script"
        fi

        log "Running Miren installer to $MIREN_RELEASE_DIR (branch: $RELEASE)..."
        if ! bash /tmp/miren-install.sh; then
            finish "fail" "ERROR: Miren installation failed"
        fi
    fi

    # Verify miren binary exists
    if [ ! -f "$MIREN_RELEASE_DIR/miren" ]; then
        finish "fail" "ERROR: Miren binary not found at $MIREN_RELEASE_DIR/miren"
    fi
    log "Miren binary installed at $MIREN_RELEASE_DIR/miren"

    # Create symlink for PATH access
    log "Creating symlink from /usr/local/bin/miren to $MIREN_RELEASE_DIR/miren..."
    ln -sf "$MIREN_RELEASE_DIR/miren" /usr/local/bin/miren

    # Verify version
    log "Miren version: $(miren version || echo 'Unable to get version')"

    # Install the unit through the real code path rather than a copy of it.
    # This file used to carry its own heredoc of the unit, which silently went
    # stale: it was missing ExecReload for months, so the harness tested a unit
    # nobody shipped.
    log "Installing systemd service via 'miren server install'..."
    if ! miren server install --force --no-start --without-cloud --skip-system-check \
        --branch "$RELEASE" --address=0.0.0.0:8443; then
        finish "fail" "ERROR: 'miren server install' failed"
    fi

    if ! verify_service_setup; then
        finish "fail" "ERROR: unit verification failed"
    fi

    log "Miren service installed but not started"
    log "To start the service: systemctl start miren"
    log "To check status: systemctl status miren"

    finish "ok" "Container ready for testing"
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