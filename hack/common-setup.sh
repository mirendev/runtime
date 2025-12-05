#!/bin/bash
# Common setup functions for test.sh and dev.sh

# Setup cgroups for container runtimes
setup_cgroups() {
    if [ -d /sys/fs/cgroup/inner ]; then
        # Already set up
        return
    fi

    # Move processes to an inner cgroup so subtree_control can be modified.
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
EOF

    # Add metrics config if address provided
    if [ "$metrics_address" != "" ]; then
        cat <<EOF >>/etc/containerd/config.toml

[metrics]
  address = "$metrics_address"
EOF
    fi
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

# Setup host user for file ownership preservation
# Creates a user matching the host UID/GID if it doesn't exist
setup_host_user() {
    local uid="${ISO_UID}"
    local gid="${ISO_GID}"

    # If ISO_UID not set, detect from mounted directory ownership
    if [ -z "$uid" ]; then
        uid=$(stat -c "%u" /src 2>/dev/null || echo "1000")
        gid=$(stat -c "%g" /src 2>/dev/null || echo "1000")
    fi

    # Check if user with this UID already exists
    if ! getent passwd "$uid" >/dev/null 2>&1; then
        # Create group if it doesn't exist
        if ! getent group "$gid" >/dev/null 2>&1; then
            groupadd -g "$gid" dev
        fi
        # Create user
        useradd -u "$uid" -g "$gid" -m -s /bin/bash dev
    fi

    # Get the username for this UID
    local username=$(getent passwd "$uid" | cut -d: -f1)
    local homedir=$(getent passwd "$uid" | cut -d: -f6)

    # Create ~/bin with shims for containerd tools
    if [ -n "$homedir" ] && [ -d "$homedir" ]; then
        mkdir -p "$homedir/bin"

        # Create ctr shim
        cat > "$homedir/bin/ctr" <<'EOF'
#!/bin/bash
exec sudo -E /usr/local/bin/ctr "$@"
EOF

        # Create nerdctl shim
        cat > "$homedir/bin/nerdctl" <<'EOF'
#!/bin/bash
exec sudo -E /usr/local/bin/nerdctl "$@"
EOF

        chmod +x "$homedir/bin/ctr" "$homedir/bin/nerdctl"
        chown -R "$uid:$gid" "$homedir/bin"
    fi

    # Create .bashrc in user's home that sources the main one
    if [ -n "$homedir" ] && [ -d "$homedir" ]; then
        cat > "$homedir/.bashrc" <<'EOF'
# Add ~/bin to PATH for shims
export PATH="$HOME/bin:$PATH"

# Source the shared bashrc
if [ -f /root/.bashrc ]; then
    source /root/.bashrc
fi
EOF
        chown "$uid:$gid" "$homedir/.bashrc"
    fi

    # Configure sudo for passwordless access
    if command -v sudo >/dev/null 2>&1; then
        echo "$username ALL=(ALL) NOPASSWD: ALL" > "/etc/sudoers.d/$username"
        chmod 440 "/etc/sudoers.d/$username"
    fi

    # Export for use by callers
    export HOST_UID="$uid"
    export HOST_GID="$gid"
    export HOST_USER="$username"
}
