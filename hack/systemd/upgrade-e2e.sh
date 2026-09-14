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
#      where v9.0.2 is a script that reports a version, writes to etcd the
#      way a build migrating state on boot would, and then cannot start, so
#      the executor must roll back to v9.0.1 and the pre-upgrade etcd data
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
# The broken build stands in for a new version that migrates etcd state on
# boot and then fails readiness. A graceful service stop takes embedded etcd
# down with the server, so like a real build it brings etcd up on the
# existing data directory itself (with the etcd binary the harness installs
# below), writes, stops it again, and exits non-zero.
ETCDCTL="etcdctl --endpoints https://localhost:12379 --cacert /var/lib/miren/etcd-certs/ca.crt --cert /var/lib/miren/etcd-certs/server.crt --key /var/lib/miren/etcd-certs/server.key"
T=$(mktemp -d); cat >"$T/miren" <<'BROKEN'
#!/bin/sh
case "$1" in
  version) echo '{"version":"v9.0.2","commit":"0000000000000000000000000000000000000002","build_date":"2026-01-01T02:00:00Z"}';;
  server)
    /usr/local/bin/etcd --data-dir /var/lib/miren/etcd --name miren-etcd \
      --listen-client-urls http://127.0.0.1:12379 --advertise-client-urls http://127.0.0.1:12379 \
      --listen-peer-urls http://127.0.0.1:12380 --log-level error >/dev/null 2>&1 &
    ETCD_PID=$!
    for _ in $(seq 1 50); do
      /usr/local/bin/etcdctl --endpoints http://127.0.0.1:12379 endpoint health >/dev/null 2>&1 && break
      sleep 0.2
    done
    if /usr/local/bin/etcdctl --endpoints http://127.0.0.1:12379 put /e2e/migrated by-v9.0.2 >/dev/null; then
      echo "migration marker written" >&2
    else
      echo "could not write migration marker" >&2
    fi
    kill "$ETCD_PID"; wait "$ETCD_PID"
    echo "v9.0.2 is broken on purpose" >&2; exit 1;;
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
# A server left behind by an earlier run would quietly serve its fixtures
# instead of these; python's bind failure is only in http.log otherwise.
if ss -ltn | grep -q ":$ASSET_PORT "; then
  echo "port $ASSET_PORT is already in use; stop the other server or set ASSET_PORT" >&2; exit 1
fi
# exec so $! is python itself; killing the subshell would orphan it.
(cd "$WORK/assets" && exec python3 -m http.server "$ASSET_PORT" --bind 0.0.0.0 >"$WORK/http.log" 2>&1) &
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

# etcd and etcdctl from the image the runtime runs etcd from: the harness
# reads etcd through the server's TLS port, and the broken fixture runs its
# own etcd on the data directory while the server is down.
ETCD_IMAGE=$(sed -n 's/.*Etcd = "\(.*\)"/\1/p' "$ROOT_DIR/pkg/imagerefs/imagerefs.go")
log "installing etcd and etcdctl from $ETCD_IMAGE"
ETCD_CID=$(docker create "$ETCD_IMAGE")
for bin in etcd etcdctl; do
  docker cp "$ETCD_CID:/usr/local/bin/$bin" "$WORK/bin/$bin"
  docker cp "$WORK/bin/$bin" "$CONTAINER:/usr/local/bin/$bin"
done
docker rm "$ETCD_CID" >/dev/null
etcdctl_in() { in_container "$ETCDCTL $*"; }

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
# Every upgrade snapshots etcd first, whether or not it ends up needing it.
BACKUP=$(in_container 'miren server operations list --format json' | jq -re '.[-1].backup_ref')
in_container "test -s '$BACKUP'"

log "3. miren server upgrade --version v9.0.2 (broken, must roll back binary and data)"
etcdctl_in put /e2e/before kept >/dev/null
if in_container "MIREN_ASSET_BASE_URL=$ASSET_URL miren server upgrade --version v9.0.2 --health-timeout 45"; then
  echo "upgrade to a broken build reported success" >&2; exit 1
fi
[ "$(health .server.version)" = "v9.0.1" ]
[ "$(health .server.ready)" = "true" ]
OP=$(in_container 'miren server operations list --format json' | jq -c '.[-1]')
echo "$OP" | jq -e '.phase == "rolled_back"' >/dev/null
echo "$OP" | jq -e '.data_restore.restored_at != null and .data_restore.backup_ref == .backup_ref' >/dev/null
in_container "test -s '$(echo "$OP" | jq -r .backup_ref)'"
# The data is what it was before the upgrade: the broken build's write, which
# the journal proves happened, is gone and the earlier key is back.
[ "$(in_container 'journalctl -u miren --no-pager -o cat | grep -c "migration marker written"')" -gt 0 ]
[ "$(etcdctl_in get /e2e/before --print-value-only)" = "kept" ]
[ -z "$(etcdctl_in get /e2e/migrated --print-value-only)" ]
in_container "test -d /var/lib/miren/etcd.replaced-$(echo "$OP" | jq -r .id)"
in_container "miren server operations show $(echo "$OP" | jq -r .id)" | grep -q '^Restore:   restored'

log "4. restart with the CLI killed mid-operation"
BEFORE=$(health .server.runtime_instance_id)
rc=0; in_container 'timeout 2 miren server restart' || rc=$?
[ "$rc" = 124 ] || { echo "expected timeout to kill the CLI (exit 124), got $rc" >&2; exit 1; }
for _ in $(seq 1 40); do
  in_container 'miren server operations list --format json' | jq -e '.[-1].phase == "succeeded"' >/dev/null 2>&1 && break
  sleep 3
done
in_container 'miren server operations list --format json' | jq -e '.[-1].phase == "succeeded"' >/dev/null
[ "$(health .server.runtime_instance_id)" != "$BEFORE" ]

log "all upgrade scenarios passed"
in_container 'miren server operations list'
