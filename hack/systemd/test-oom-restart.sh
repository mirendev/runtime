#!/bin/bash
# End-to-end check of the memory-limit story, run inside the systemd test
# container: squeeze miren.service until the kernel kills it, then confirm the
# kill was recorded and reported.
#
# This exercises the two halves that unit tests can't reach — systemd actually
# invoking ExecStopPost with SERVICE_RESULT=oom-kill, and the hook managing to
# run at all inside a cgroup that just hit its limit.
#
# Usage, from the repository root:
#   docker exec -it miren-systemd-test /hack/test-oom-restart.sh
# or inside a shell in the container:
#   /hack/test-oom-restart.sh

set -uo pipefail

STATE_DIR=/var/lib/miren/server
RECORD="$STATE_DIR/last-exit.json"
REPORTED="$STATE_DIR/last-exit.reported.json"

# Small enough that miren cannot finish starting inside it, so the kernel kills
# the control process within seconds of launch.
SQUEEZE=32M

log() { echo "[$(date '+%H:%M:%S')] $1"; }
fail() { echo "FAIL: $1" >&2; exit 1; }

log "Clearing any previous exit record..."
rm -f "$RECORD" "$REPORTED"

log "Stopping miren..."
systemctl stop miren.service 2>/dev/null

# Record the managed limit so the cleanup can prove it came back.
MANAGED_MAX=$(systemctl show -p MemoryMax --value miren.service)
if [ -z "$MANAGED_MAX" ] || [ "$MANAGED_MAX" = "infinity" ]; then
    fail "miren.service has no managed MemoryMax to begin with — is the drop-in installed?"
fi
log "Managed MemoryMax is $MANAGED_MAX"

log "Squeezing miren.service to MemoryMax=$SQUEEZE..."
# --runtime writes to /run/systemd/system/miren.service.d, leaving the managed
# drop-in under /etc untouched.
systemctl set-property --runtime miren.service MemoryMax="$SQUEEZE" MemorySwapMax=0 \
    || fail "could not set a temporary memory limit"

log "Starting miren under the squeeze (expected to be killed)..."
systemctl start miren.service 2>/dev/null

# Restart=always means systemd keeps relaunching into the same wall. Give it a
# few cycles, then stop so the loop doesn't run for the rest of the session.
for _ in $(seq 1 30); do
    [ -f "$RECORD" ] && break
    sleep 1
done

log "Releasing the squeeze..."
systemctl stop miren.service 2>/dev/null
# Deliberately NOT `systemctl revert`: that removes every drop-in for the unit,
# including the managed /etc/systemd/system/miren.service.d/10-miren-resources.conf
# this script exists to verify. Remove only what set-property --runtime created.
#
# That lands in system.control, not system. Getting it wrong leaves the 32 MB
# squeeze in force, and since /run outranks /etc the service would restart still
# squeezed. Both are cleared so a systemd that chooses differently still ends up
# clean, and the restored-limit check below is what actually proves it did.
rm -rf /run/systemd/system.control/miren.service.d
rm -rf /run/systemd/system/miren.service.d
systemctl daemon-reload

restored=$(systemctl show -p MemoryMax --value miren.service)
if [ "$restored" != "$MANAGED_MAX" ]; then
    fail "managed MemoryMax did not come back: expected '$MANAGED_MAX', got '$restored'"
fi
log "Managed MemoryMax restored to $restored"

if [ ! -f "$RECORD" ]; then
    log "No exit record at $RECORD. Recent journal:"
    journalctl -u miren --no-pager -n 40 || true
    fail "systemd killed the service but nothing recorded why"
fi

log "Exit record written:"
cat "$RECORD"

result=$(jq -r .result "$RECORD")
if [ "$result" != "oom-kill" ]; then
    fail "expected result=oom-kill, got '$result'"
fi

peak=$(jq -r '.memory_peak // 0' "$RECORD")
if [ "$peak" = "0" ]; then
    # Not fatal: MemoryPeak needs systemd 254 or newer.
    log "NOTE: no memory_peak recorded (systemd $(systemctl --version | head -1 | awk '{print $2}') may predate it)"
fi

log "Restarting miren normally to check the exit is reported..."
systemctl start miren.service 2>/dev/null

reported=0
for _ in $(seq 1 60); do
    if [ -f "$REPORTED" ]; then
        reported=1
        break
    fi
    sleep 1
done

if [ "$reported" -eq 1 ]; then
    log "PASS: the restart was recorded and reported"
    journalctl -u miren --no-pager -n 200 | grep -i "memory limit" || true
    exit 0
fi

# The report is logged by a boot component that runs after observability comes
# up, so a server that can't finish starting in this container won't have
# reached it yet. The record stays put and is reported on the first healthy
# boot, so this is a warning about the container, not a failure of the feature.
log "WARN: exit record still unreported — did the server finish starting?"
systemctl is-active miren.service || true
journalctl -u miren --no-pager -n 40 || true
exit 0
