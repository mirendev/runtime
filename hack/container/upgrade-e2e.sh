#!/usr/bin/env bash
#
# End-to-end test of `miren server restart` and `miren upgrade` on a
# container install, the docker/ image run the way `miren server container
# install` runs it (--init --privileged --restart always, data volume on
# /var/lib/miren). Builds the image from this checkout with a build tagged
# v9.0.0 as the image binary, serves two fixture releases from a local asset
# server, and drives:
#
#   1. miren cluster restart              (new instance, same version; the
#                                          container exits and docker brings
#                                          it back)
#   2. miren cluster upgrade -V v9.0.1    (real upgrade, new binary in the
#                                          volume, image binary untouched)
#   3. miren cluster upgrade -V v9.0.2
#      where v9.0.2 is a script that reports a version and exits, so the
#      new build crash loops before any executor can run in it and
#      container-boot has to roll back the binary and the etcd snapshot
#   4. miren cluster upgrade -V v9.0.3
#      where v9.0.3 reports a version and then hangs, so nothing inside the
#      server ever runs and the watchdog container-boot started beside it
#      has to roll back on the ready deadline
#   5. a restart whose CLI is killed mid-flight, which must still finish
#
# The CLI runs on the host through the cluster commands, as it does for real
# container installs: a `docker exec` session dies with the container at the
# restart it asked for, and the server commands are for a systemd host.
#
# Requires docker on a Linux host and the iso dev environment (binaries are
# built with hack/dev-exec so no host Go toolchain is needed; the build is
# static, so the same binary serves as the host CLI). The image build
# downloads the main release bundle from the asset service. Takes several
# minutes; not wired into per-PR CI on purpose.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && cd .. && pwd)"
WORK="${WORK:-$(mktemp -d)}"
ASSET_PORT="${ASSET_PORT:-8788}"
# Fixed host ports, as container install publishes: an ephemeral mapping
# (-p 127.0.0.1::8443) is reallocated on every container restart, which is
# exactly what this test does over and over.
API_PORT="${API_PORT:-18443}"
HEALTH_PORT="${HEALTH_PORT:-18443}"
CONTAINER=miren-container-e2e
VOLUME=miren-container-e2e-data
IMAGE=miren-container-e2e:v9.0.0

log() { echo "[$(date '+%H:%M:%S')] $*"; }
in_container() { docker exec "$CONTAINER" "$@"; }
# The host CLI: the v9.0.0 build with a client config pointing at the
# container's published API port.
m() { MIREN_CONFIG="$WORK/clientconfig.yaml" MIREN_CLUSTER=local "$WORK/bin/miren-v9.0.0" "$@"; }
health() { curl -sk "https://127.0.0.1:$HEALTH_PORT/.well-known/miren/health" 2>/dev/null | jq -r "$1"; }
restart_count() { docker inspect --format '{{.RestartCount}}' "$CONTAINER"; }
ledger() { in_container cat "/var/lib/miren/server/lifecycle/$1.json"; }
wait_ready() {
  for _ in $(seq 1 100); do
    [ "$(health .server.ready 2>/dev/null)" = "true" ] && return 0
    sleep 3
  done
  echo "server never became ready" >&2; docker logs --tail 30 "$CONTAINER" >&2; return 1
}
# Reads the ledger file rather than the RPC so it works while the server is
# down or crash looping; docker exec fails during the restart itself, and
# that just means trying again.
wait_terminal() {
  local phase=""
  for _ in $(seq 1 100); do
    phase=$(ledger "$1" 2>/dev/null | jq -r .phase) || phase=""
    case "$phase" in succeeded|failed|rolled_back) echo "$phase"; return 0;; esac
    sleep 3
  done
  echo "operation $1 never finished (last phase: $phase)" >&2; return 1
}
last_op_id() { m server operations list --format json | jq -r '.[-1].id'; }

log "building fixture binaries in $WORK"
mkdir -p "$WORK/bin" "$WORK/assets/v9.0.1" "$WORK/assets/v9.0.2" "$WORK/assets/v9.0.3"
for v in v9.0.0 v9.0.1; do
  n=${v##*.}
  (cd "$ROOT_DIR" && ./hack/dev-exec env GIT_VERSION=$v GIT_COMMIT=$(printf '%040d' $n) \
    BUILD_DATE=2026-01-01T0$n:00:00Z bash hack/build.sh >"$WORK/build-$v.log" 2>&1)
  cp "$ROOT_DIR/bin/miren" "$WORK/bin/miren-$v"
done

log "packaging fixture releases"
T=$(mktemp -d); cp "$WORK/bin/miren-v9.0.1" "$T/miren"
tar -C "$T" -czf "$WORK/assets/v9.0.1/miren-base-linux-amd64.tar.gz" miren; rm -rf "$T"
# The broken build answers `version` (the installer checks it) and dies on
# anything else. Inside the container that is the whole new build crashing
# before its executor exists: only container-boot, in the image binary, is
# around to notice.
T=$(mktemp -d); cat >"$T/miren" <<'BROKEN'
#!/bin/sh
case "$1" in
  version) echo '{"version":"v9.0.2","commit":"0000000000000000000000000000000000000002","build_date":"2026-01-01T02:00:00Z"}';;
  *) echo "v9.0.2 is broken on purpose" >&2; exit 1;;
esac
BROKEN
chmod +x "$T/miren"
tar -C "$T" -czf "$WORK/assets/v9.0.2/miren-base-linux-amd64.tar.gz" miren; rm -rf "$T"
# The hung build is the shape neither the crash count nor the executor can
# see: it stays up and never gets anywhere.
T=$(mktemp -d); cat >"$T/miren" <<'HUNG'
#!/bin/sh
case "$1" in
  version) echo '{"version":"v9.0.3","commit":"0000000000000000000000000000000000000003","build_date":"2026-01-01T03:00:00Z"}';;
  *) echo "v9.0.3 hangs on purpose" >&2; exec sleep infinity;;
esac
HUNG
chmod +x "$T/miren"
tar -C "$T" -czf "$WORK/assets/v9.0.3/miren-base-linux-amd64.tar.gz" miren; rm -rf "$T"
for v in v9.0.1 v9.0.2 v9.0.3; do
  n=${v##*.}
  (cd "$WORK/assets/$v" && sha256sum miren-base-linux-amd64.tar.gz >miren-base-linux-amd64.tar.gz.sha256)
  echo "{\"version\":\"$v\",\"commit\":\"$(printf '%040d' $n)\",\"branch\":\"$v\",\"build_date\":\"2026-01-01T0$n:00:00Z\",\"artifacts\":[]}" \
    >"$WORK/assets/$v/version.json"
done

log "building the container image with v9.0.0 as the image binary"
# The Dockerfile's builder stage is swapped for a directory holding the
# prebuilt binary, so the image build is the runtime stage only.
mkdir -p "$WORK/builder/build/bin" "$WORK/ctx/docker"
cp "$WORK/bin/miren-v9.0.0" "$WORK/builder/build/bin/miren"
cp "$ROOT_DIR/docker/entrypoint.sh" "$WORK/ctx/docker/entrypoint.sh"
docker build --build-context "builder=$WORK/builder" -f "$ROOT_DIR/docker/Dockerfile" -t "$IMAGE" "$WORK/ctx" >"$WORK/image.log" 2>&1

log "serving fixtures on :$ASSET_PORT"
if ss -ltn | grep -q ":$ASSET_PORT "; then
  echo "port $ASSET_PORT is already in use; stop the other server or set ASSET_PORT" >&2; exit 1
fi
if ss -ltn | grep -q ":$HEALTH_PORT " || ss -uln | grep -q ":$API_PORT "; then
  echo "port $HEALTH_PORT/tcp or $API_PORT/udp is already in use; set HEALTH_PORT or API_PORT" >&2; exit 1
fi
(cd "$WORK/assets" && exec python3 -m http.server "$ASSET_PORT" --bind 0.0.0.0 >"$WORK/http.log" 2>&1) &
HTTP_PID=$!
cleanup() {
  kill $HTTP_PID 2>/dev/null || true
  if [ -z "${KEEP:-}" ]; then
    docker rm -f "$CONTAINER" >/dev/null 2>&1 || true
    docker volume rm "$VOLUME" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT
GATEWAY=$(docker network inspect bridge --format '{{(index .IPAM.Config 0).Gateway}}')
ASSET_URL="http://$GATEWAY:$ASSET_PORT"

log "starting the container the way container install does"
docker rm -f "$CONTAINER" >/dev/null 2>&1 || true
docker volume rm "$VOLUME" >/dev/null 2>&1 || true
# The executor runs inside the server here, so the asset override has to be
# in the container's environment, not the CLI's.
docker run -d --name "$CONTAINER" --init --privileged --restart always \
  -p "127.0.0.1:$HEALTH_PORT:443/tcp" -p "127.0.0.1:$API_PORT:8443/udp" -e "MIREN_ASSET_BASE_URL=$ASSET_URL" \
  -v "$VOLUME:/var/lib/miren" "$IMAGE" server -v >/dev/null
wait_ready
[ "$(health .server.version)" = "v9.0.0" ]
[ "$(health .server.install_kind)" = "container" ]
in_container test -f /var/lib/miren/release/.image-sha256

log "configuring the host CLI against 127.0.0.1:$API_PORT"
in_container miren auth generate -C local -t "127.0.0.1:$API_PORT" -c - >"$WORK/clientconfig.yaml"
m version --server --format json | jq -e '.server.version == "v9.0.0"' >/dev/null
BEFORE=$(health .server.runtime_instance_id)
RESTARTS=$(restart_count)

log "1. miren cluster restart"
m cluster restart --yes
wait_ready
[ "$(health .server.runtime_instance_id)" != "$BEFORE" ]
[ "$(health .server.version)" = "v9.0.0" ]
[ "$(restart_count)" -eq $((RESTARTS + 1)) ]
m server operations list --format json | jq -e '.[-1].phase == "succeeded"' >/dev/null

log "2. miren cluster upgrade --version v9.0.1"
m cluster upgrade --yes --version v9.0.1
wait_ready
[ "$(health .server.version)" = "v9.0.1" ]
in_container test -f /var/lib/miren/release/miren.old
# The image binary is the fallback and must never be touched by an upgrade.
in_container /usr/local/bin/miren version --format json | jq -e '.version == "v9.0.0"' >/dev/null
OP=$(m server operations list --format json | jq -c '.[-1]')
echo "$OP" | jq -e '.phase == "succeeded" and .new_version == "v9.0.1"' >/dev/null
in_container test -s "$(echo "$OP" | jq -r .backup_ref)"
RESTARTS=$(restart_count)

log "3. miren cluster upgrade --version v9.0.2 (crash loops; container-boot must roll back)"
if m cluster upgrade --yes --version v9.0.2 --health-timeout 45; then
  echo "upgrade to a broken build reported success" >&2; exit 1
fi
OP_ID=$(last_op_id)
[ "$(wait_terminal "$OP_ID")" = "rolled_back" ]
wait_ready
[ "$(health .server.version)" = "v9.0.1" ]
OP=$(ledger "$OP_ID")
echo "$OP" | jq -e '.rollback_from == "container-boot"' >/dev/null
echo "$OP" | jq -e '.boot_attempts == 4' >/dev/null
echo "$OP" | jq -e '.data_restore.restored_at != null and .data_restore.backup_ref == .backup_ref' >/dev/null
echo "$OP" | jq -e '.error | test("did not come up in 3 boots")' >/dev/null
in_container test -d "/var/lib/miren/etcd.replaced-$OP_ID"
in_container test ! -e /var/lib/miren/release/miren.old
# One restart onto the broken build, three more crash loops, and the fourth
# boot rolled back and ran v9.0.1.
[ "$(restart_count)" -ge $((RESTARTS + 4)) ]
m server operations show "$OP_ID" | grep -Eq '^ *Restore: +restored'

log "4. miren cluster upgrade --version v9.0.3 (hangs; the watchdog must roll back)"
RESTARTS=$(restart_count)
if m cluster upgrade --yes --version v9.0.3 --health-timeout 45; then
  echo "upgrade to a hung build reported success" >&2; exit 1
fi
OP_ID=$(last_op_id)
[ "$(wait_terminal "$OP_ID")" = "rolled_back" ]
wait_ready
[ "$(health .server.version)" = "v9.0.1" ]
OP=$(ledger "$OP_ID")
echo "$OP" | jq -e '.rollback_from == "container-boot"' >/dev/null
echo "$OP" | jq -e '.boot_attempts == 1' >/dev/null
echo "$OP" | jq -e '.error | test("did not report ready within")' >/dev/null
echo "$OP" | jq -e '.data_restore.restored_at != null' >/dev/null
in_container test -d "/var/lib/miren/etcd.replaced-$OP_ID"
# One restart onto the hung build, one more when the watchdog ended it.
[ "$(restart_count)" -ge $((RESTARTS + 2)) ]
[ "$(docker logs "$CONTAINER" 2>&1 | grep -c 'ending the hung server')" -gt 0 ]

log "5. restart with the CLI killed mid-operation"
BEFORE=$(health .server.runtime_instance_id)
rc=0; timeout 2 env MIREN_CONFIG="$WORK/clientconfig.yaml" MIREN_CLUSTER=local "$WORK/bin/miren-v9.0.0" cluster restart --yes || rc=$?
[ "$rc" = 124 ] || { echo "expected timeout to kill the CLI (exit 124), got $rc" >&2; exit 1; }
wait_ready
OP_ID=$(last_op_id)
[ "$(wait_terminal "$OP_ID")" = "succeeded" ]
[ "$(health .server.runtime_instance_id)" != "$BEFORE" ]

log "all container upgrade scenarios passed"
m server operations list
