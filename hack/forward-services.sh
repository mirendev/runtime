#!/bin/bash
# Forward services from debug container to host for local debugging

# Check for required dependencies
check_dependency() {
    if ! command -v "$1" &> /dev/null; then
        echo "✗ Error: $1 is not installed"
        echo "  $2"
        return 1
    fi
}

# Check all required tools
MISSING_DEPS=0
check_dependency "docker" "Please install Docker: https://docs.docker.com/get-docker/" || MISSING_DEPS=1
check_dependency "socat" "Please install socat: apt install socat (Ubuntu/Debian) or brew install socat (macOS)" || MISSING_DEPS=1
check_dependency "pkill" "Please install procps package for pkill support" || MISSING_DEPS=1
check_dependency "make" "Please install make: apt install make (Ubuntu/Debian) or install Xcode Command Line Tools (macOS)" || MISSING_DEPS=1

if [ $MISSING_DEPS -eq 1 ]; then
    echo ""
    echo "Please install missing dependencies and try again."
    # Handle both sourced and executed modes
    if [[ "${BASH_SOURCE[0]}" != "${0}" ]]; then
        return 1  # Return from sourced script
    else
        exit 1    # Exit from executed script
    fi
fi

# Try to get IPs automatically from the container
if [ -z "$ETCD_IP" ] || [ -z "$CLICKHOUSE_IP" ]; then
    echo "Getting service IPs from container..."
    
    # Find the container PID running bash
    CONTAINER_PID=$(docker exec dagger-engine-v0.18.9 ps aux | grep -E "bash.*(run-services|dev.sh)" | grep -v grep | awk '{print $1}' | head -1)
    
    if [ -n "$CONTAINER_PID" ]; then
        # Get the IPs using nsenter
        SERVICE_IPS=$(docker exec dagger-engine-v0.18.9 nsenter -t $CONTAINER_PID -m -n sh -c 'getent hosts etcd clickhouse' 2>/dev/null)
        
        if [ -n "$SERVICE_IPS" ]; then
            ETCD_IP=$(echo "$SERVICE_IPS" | grep etcd | awk '{print $1}')
            CLICKHOUSE_IP=$(echo "$SERVICE_IPS" | grep clickhouse | awk '{print $1}')
            echo "✓ Found service IPs automatically"
        else
            echo "✗ Could not get service IPs automatically"
        fi
    else
        echo "✗ No debug container found running"
    fi
fi

# Fallback to defaults if auto-detection failed
ETCD_IP=${ETCD_IP:-10.87.3.128}
CLICKHOUSE_IP=${CLICKHOUSE_IP:-10.87.3.129}

echo "=== Forwarding Debug Services ==="
echo "Using IPs:"
echo "  etcd:       $ETCD_IP"
echo "  ClickHouse: $CLICKHOUSE_IP"
echo ""
echo "To update IPs, run:"
echo "  ETCD_IP=10.87.x.x CLICKHOUSE_IP=10.87.x.x $0"
echo ""

# Kill any existing forwards
pkill -f "socat.*9001" 2>/dev/null || true
pkill -f "socat.*9000" 2>/dev/null || true
pkill -f "socat.*2379" 2>/dev/null || true
pkill -f "socat.*8123" 2>/dev/null || true
sleep 1

# Start forwarding
echo "Starting port forwarding..."

# Track PIDs for cleanup
SOCAT_PIDS=()

# etcd
if socat TCP-LISTEN:2379,reuseaddr,fork EXEC:"docker exec -i dagger-engine-v0.18.9 nc $ETCD_IP 2379" & then
    SOCAT_PIDS+=($!)
    echo "✓ etcd forwarded: localhost:2379 → $ETCD_IP:2379"
else
    echo "✗ Failed to forward etcd port"
    # Kill already started processes
    for pid in ${SOCAT_PIDS[@]}; do kill $pid 2>/dev/null; done
    exit 1
fi

# ClickHouse native protocol
if socat TCP-LISTEN:9000,reuseaddr,fork EXEC:"docker exec -i dagger-engine-v0.18.9 nc $CLICKHOUSE_IP 9000" & then
    SOCAT_PIDS+=($!)
    echo "✓ ClickHouse native forwarded: localhost:9000 → $CLICKHOUSE_IP:9000"
else
    echo "✗ Failed to forward ClickHouse native port"
    # Kill already started processes
    for pid in ${SOCAT_PIDS[@]}; do kill $pid 2>/dev/null; done
    exit 1
fi

# ClickHouse HTTP
if socat TCP-LISTEN:8123,reuseaddr,fork EXEC:"docker exec -i dagger-engine-v0.18.9 nc $CLICKHOUSE_IP 8123" & then
    SOCAT_PIDS+=($!)
    echo "✓ ClickHouse HTTP forwarded: localhost:8123 → $CLICKHOUSE_IP:8123"
else
    echo "✗ Failed to forward ClickHouse HTTP port"
    # Kill already started processes
    for pid in ${SOCAT_PIDS[@]}; do kill $pid 2>/dev/null; done
    exit 1
fi

echo ""
echo "All services forwarded!"
echo ""

# Cleanup function for when script is sourced
cleanup_forwarding() {
    echo "Stopping port forwarding..."
    for pid in ${SOCAT_PIDS[@]}; do
        kill $pid 2>/dev/null && echo "  Stopped process $pid"
    done
    echo "Port forwarding stopped"
}

# Set trap for cleanup on exit (only when not sourced)
if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
    trap cleanup_forwarding EXIT
fi

# Check if script is being sourced
if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
    # Script is being run directly
    echo "Environment variables to export:"
    echo "  export S3_URL=http://localhost:9001"
    echo "  export ETCD_ENDPOINTS=localhost:2379"  
    echo "  export CLICKHOUSE_URL=http://localhost:8123"
    echo ""
    echo "To automatically export these variables, run:"
    echo "  source ./hack/forward-services.sh"
else
    # Script is being sourced
    export S3_URL=http://localhost:9001
    export ETCD_ENDPOINTS=localhost:2379
    export CLICKHOUSE_URL=http://localhost:8123
    
    echo "✓ Environment variables exported:"
    echo "  S3_URL=$S3_URL"
    echo "  ETCD_ENDPOINTS=$ETCD_ENDPOINTS"
    echo "  CLICKHOUSE_URL=$CLICKHOUSE_URL"
    
    # Build miren if needed and create 'm' alias
    SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
    PROJECT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
    MIREN_BIN="$PROJECT_DIR/bin/miren"
    
    if [[ ! -f "$MIREN_BIN" ]]; then
        echo "Building miren binary..."
        (cd "$PROJECT_DIR" && make bin/miren)
    fi
    
    if [[ -f "$MIREN_BIN" ]]; then
        alias m="$MIREN_BIN"
        echo "✓ Created alias 'm' for miren command"
    else
        echo "✗ Failed to build miren binary"
    fi
fi

echo ""
if [[ "${BASH_SOURCE[0]}" != "${0}" ]] && [[ -f "$MIREN_BIN" ]]; then
    echo "You can now run: m server -vv --etcd=localhost:2379 --clickhouse-addr=localhost:9000"
else
    echo "You can now run: ./bin/miren server -vv --etcd=localhost:2379 --clickhouse-addr=localhost:9000"
fi
