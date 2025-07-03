#!/bin/bash
# Common setup functions for test.sh and dev.sh

# Setup cgroups for runsc
setup_cgroups() {
    # Solve the issue of runsc not being able to manipulate subtree_control
    # by moving everything here into a new cgroup so the root can be changed.
    mkdir /sys/fs/cgroup/inner

    cat /sys/fs/cgroup/cgroup.procs | while read -r pid; do
        echo "$pid" >/sys/fs/cgroup/inner/cgroup.procs 2>/dev/null || true
    done

    sed -e 's/ / +/g' -e 's/^/+/' </sys/fs/cgroup/cgroup.controllers >/sys/fs/cgroup/cgroup.subtree_control
}

# Setup basic directories and environment
setup_environment() {
    mkdir -p /data /run
    export OTEL_SDK_DISABLED=true
}

# Generate containerd config with optional metrics
# Usage: generate_containerd_config [metrics_address]
generate_containerd_config() {
    local metrics_address="$1"

    cat <<EOF >/etc/containerd/config.toml
version = 2
[plugins."io.containerd.runtime.v1.linux"]
  shim_debug = true
[plugins."io.containerd.grpc.v1.cri".containerd.runtimes.runc]
  runtime_type = "io.containerd.runc.v2"
[plugins."io.containerd.grpc.v1.cri".containerd.runtimes.runsc]
  runtime_type = "io.containerd.runsc.v1"
[plugins."io.containerd.grpc.v1.cri".containerd.runtimes.miren]
  runtime_type = "io.containerd.runsc.v1"
EOF

    # Add metrics config if address provided
    if [ "$metrics_address" != "" ]; then
        cat <<EOF >>/etc/containerd/config.toml

[metrics]
  address = "$metrics_address"
EOF
    fi
}

# Setup runsc config
setup_runsc_config() {
    cat <<EOF >/etc/containerd/runsc.toml
log_path = "/var/log/runsc/%ID%/shim.log"
log_level = "debug"
binary_name = "/src/hack/runsc-ignore"
[runsc_config]
  debug = "true"
  debug-log = "/var/log/runsc/%ID%/gvisor.%COMMAND%.log"
EOF
}

# Setup kernel mounts
setup_kernel_mounts() {
    mount -t debugfs nodev /sys/kernel/debug
    mount -t tracefs nodev /sys/kernel/debug/tracing
    mount -t tracefs nodev /sys/kernel/tracing
}

# Start containerd
# Usage: start_containerd <socket_path> [log_destination]
start_containerd() {
    local socket_path="$1"
    local log_dest="${2:-/dev/null}"

    mkdir -p "$(dirname "$socket_path")"

    if [ "$log_dest" = "/dev/null" ]; then
        containerd --root /data --state /data/state --address "$socket_path" -l trace >/dev/null 2>&1 &
    else
        containerd --root /data --state /data/state --address "$socket_path" -l trace >"$log_dest" 2>&1 &
    fi
}

# Start buildkitd
# Usage: start_buildkitd [log_destination]
start_buildkitd() {
    local log_dest="${1:-/dev/stdout}"

    # Since our buildkit dir is cached across runs, there might be a stale lockfile
    # sitting around that should be safe to kill
    rm -f /data/buildkit/buildkitd.lock

    if [ "$log_dest" = "/dev/stdout" ]; then
        buildkitd --root /data/buildkit 2>&1 &
    else
        buildkitd --root /data/buildkit >"$log_dest" 2>&1 &
    fi
}

# Wait for service to be ready
# Usage: wait_for_service <service_name> <check_command>
wait_for_service() {
    local service_name="$1"
    local check_command="$2"
    local timeout=30
    local count=0

    echo "Waiting for $service_name..."
    while ! eval "$check_command" >/dev/null 2>&1; do
        sleep 1
        count=$((count + 1))
        if [ "$count" -ge "$timeout" ]; then
            echo "Timeout waiting for $service_name"
            exit 1
        fi
    done
    echo "$service_name is ready"
}

# Setup bash history and common exports
setup_bash_environment() {
    export HISTFILE=/data/.bash_history
    export HISTIGNORE=exit
}
