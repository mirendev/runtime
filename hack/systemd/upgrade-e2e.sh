#!/usr/bin/env bash
#
# End-to-end test of `miren server restart` and `miren upgrade` on a real
# systemd host. Runs the container from run-systemd-test.sh, replaces the
# installed miren with a build of this checkout tagged v9.0.0, serves two
# fixture releases from a local asset server, and drives:
#
#   1. sudo miren server restart            (new instance, same version)
#   2. sudo miren upgrade --version v9.0.1  (real upgrade, new binary)
#   3. sudo miren server upgrade --version v9.0.2 --health-timeout 45
#      where v9.0.2 is a script that reports a version but cannot start,
#      so the executor must roll back to v9.0.1
#   4. a restart whose CLI is killed mid-flight, which must still finish
#
# Requires docker on a Linux host and the iso dev environment (binaries are
# built with hack/dev-exec so they link against a glibc the container has).
# Takes several minutes; not wired into per-PR CI on purpose.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && cd .. && pwd)"
WORK="${WORK:-$(mktemp -d)}"
ASSET_PORT="${ASSET_PORT:-8787}"
CONTAINER=miren-systemd-test

log() { echo "[$(date '+%H:%M:%S')] $*"; }
in_container() { docker exec "$CONTAINER" bash -c "$*"; }
health() { in_container 'curl -sk https://127.0.0.1/.well-known/miren/health 2>/dev/null' | jq -r "$1"; }
wait_ready() {
  for _ in $(seq 1 100); do
    [ "$(health .server.ready 2>/dev/null)" = "true" ] && return 0
    sleep 3
  done
  echo "server never became ready" >&2; return 1
}

log "building fixture binaries in $WORK"
mkdir -p "$WORK/bin" "$WORK/assets/v9.0.1" "$WORK/assets/v9.0.2"
for v in v9.0.0 v9.0.1; do
  n=${v##*.}
  (cd "$ROOT_DIR" && ./hack/dev-exec env GIT_VERSION=$v GIT_COMMIT=$(printf '%040d' $n) \
    BUILD_DATE=2026-01-01T0$n:00:00Z bash hack/build.sh >"$WORK/build-$v.log" 2>&1)
  cp "$ROOT_DIR/bin/miren" "$WORK/bin/miren-$v"
done

log "packaging fixture releases"
T=$(mktemp -d); cp "$WORK/bin/miren-v9.0.1" "$T/miren"
tar -C "$T" -czf "$WORK/assets/v9.0.1/miren-base-linux-amd64.tar.gz" miren; rm -rf "$T"
T=$(mktemp -d); cat >"$T/miren" <<'BROKEN'
#!/bin/sh
case "$1" in
  version) echo '{"version":"v9.0.2","commit":"0000000000000000000000000000000000000002","build_date":"2026-01-01T02:00:00Z"}';;
  *) echo "v9.0.2 is broken on purpose" >&2; exit 1;;
esac
BROKEN
chmod +x "$T/miren"
tar -C "$T" -czf "$WORK/assets/v9.0.2/miren-base-linux-amd64.tar.gz" miren; rm -rf "$T"
for v in v9.0.1 v9.0.2; do
  n=${v##*.}
  (cd "$WORK/assets/$v" && sha256sum miren-base-linux-amd64.tar.gz >miren-base-linux-amd64.tar.gz.sha256)
  echo "{\"version\":\"$v\",\"commit\":\"$(printf '%040d' $n)\",\"branch\":\"$v\",\"build_date\":\"2026-01-01T0$n:00:00Z\",\"artifacts\":[]}" \
    >"$WORK/assets/$v/version.json"
done

log "serving fixtures on :$ASSET_PORT"
(cd "$WORK/assets" && python3 -m http.server "$ASSET_PORT" --bind 0.0.0.0 >"$WORK/http.log" 2>&1) &
HTTP_PID=$!
trap 'kill $HTTP_PID 2>/dev/null || true' EXIT
GATEWAY=$(docker network inspect bridge --format '{{(index .IPAM.Config 0).Gateway}}')
ASSET_URL="http://$GATEWAY:$ASSET_PORT"

log "starting systemd container (installs main from the asset service)"
docker volume rm miren-data >/dev/null 2>&1 || true
"$SCRIPT_DIR/run-systemd-test.sh" >"$WORK/container.log" 2>&1
for _ in $(seq 1 100); do
  docker logs "$CONTAINER" 2>&1 | grep -q 'Container ready' && break
  docker logs "$CONTAINER" 2>&1 | grep -q 'ERROR' && { docker logs "$CONTAINER" | tail -5; exit 1; }
  sleep 3
done

# The install script only places the miren binary; a server also needs the
# containerd/runc bundle from the base package next to it.
log "installing the base package dependencies"
in_container 'cd /tmp && curl -fsSL -o base.tgz https://api.miren.cloud/assets/release/miren/main/miren-base-linux-amd64.tar.gz \
  && tar -xzf base.tgz --exclude=./miren -C /var/lib/miren/release && rm base.tgz'

log "installing v9.0.0 over the network install"
docker cp "$WORK/bin/miren-v9.0.0" "$CONTAINER:/var/lib/miren/release/miren.new"
in_container 'mv /var/lib/miren/release/miren.new /var/lib/miren/release/miren && systemctl restart miren'
wait_ready
[ "$(health .server.version)" = "v9.0.0" ]
[ "$(health .server.install_kind)" = "systemd" ]
BEFORE=$(health .server.runtime_instance_id)

log "1. miren server restart"
in_container 'miren server restart'
[ "$(health .server.runtime_instance_id)" != "$BEFORE" ]
[ "$(health .server.version)" = "v9.0.0" ]

log "2. miren upgrade --version v9.0.1"
in_container "MIREN_ASSET_BASE_URL=$ASSET_URL miren upgrade --version v9.0.1"
[ "$(health .server.version)" = "v9.0.1" ]
in_container 'test -f /var/lib/miren/release/miren.old'

log "3. miren server upgrade --version v9.0.2 (broken, must roll back)"
if in_container "MIREN_ASSET_BASE_URL=$ASSET_URL miren server upgrade --version v9.0.2 --health-timeout 45"; then
  echo "upgrade to a broken build reported success" >&2; exit 1
fi
[ "$(health .server.version)" = "v9.0.1" ]
[ "$(health .server.ready)" = "true" ]
in_container 'miren server lifecycle list --format json' | jq -e '.[-1].phase == "rolled_back"' >/dev/null

log "4. restart with the CLI killed mid-operation"
BEFORE=$(health .server.runtime_instance_id)
rc=0; in_container 'timeout 2 miren server restart' || rc=$?
[ "$rc" = 124 ] || { echo "expected timeout to kill the CLI (exit 124), got $rc" >&2; exit 1; }
for _ in $(seq 1 40); do
  in_container 'miren server lifecycle list --format json' | jq -e '.[-1].phase == "succeeded"' >/dev/null 2>&1 && break
  sleep 3
done
in_container 'miren server lifecycle list --format json' | jq -e '.[-1].phase == "succeeded"' >/dev/null
[ "$(health .server.runtime_instance_id)" != "$BEFORE" ]

log "all upgrade scenarios passed"
in_container 'miren server lifecycle list'
