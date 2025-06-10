#!/bin/bash
# Cleanup script for development environment - stops port forwarding and services

echo "=== Cleaning up Development Environment ==="

# Kill any existing port forwarding processes
echo "Stopping port forwarding..."
pkill -f "socat.*9001" 2>/dev/null && echo "  ✓ Stopped MinIO forwarding" || echo "  - No MinIO forwarding found"
pkill -f "socat.*9000" 2>/dev/null && echo "  ✓ Stopped ClickHouse native forwarding" || echo "  - No ClickHouse native forwarding found"
pkill -f "socat.*2379" 2>/dev/null && echo "  ✓ Stopped etcd forwarding" || echo "  - No etcd forwarding found"
pkill -f "socat.*8123" 2>/dev/null && echo "  ✓ Stopped ClickHouse HTTP forwarding" || echo "  - No ClickHouse HTTP forwarding found"

# Optional: Stop Dagger services
read -p "Stop Dagger services? (y/N) " -n 1 -r
echo
if [[ $REPLY =~ ^[Yy]$ ]]; then
    echo "Stopping Dagger services..."
    # Find and kill the container running services
    CONTAINER_PID=$(docker exec dagger-engine-v0.18.9 ps aux 2>/dev/null | grep -E "bash.*(run-services|debug)" | grep -v grep | awk '{print $1}' | head -1)
    if [ -n "$CONTAINER_PID" ]; then
        docker exec dagger-engine-v0.18.9 kill $CONTAINER_PID 2>/dev/null && echo "  ✓ Stopped Dagger services" || echo "  ✗ Failed to stop services"
    else
        echo "  - No running services found"
    fi
fi

# Clean up any temporary files
echo "Cleaning temporary files..."
rm -f /tmp/server.log 2>/dev/null && echo "  ✓ Removed server log" || echo "  - No server log found"
rm -rf /tmp/dagger-* 2>/dev/null && echo "  ✓ Removed Dagger temp files" || echo "  - No Dagger temp files found"

# Show status
echo ""
echo "=== Status Check ==="

# Check if services are still running
SOCAT_COUNT=$(pgrep -c socat 2>/dev/null || echo 0)
if [ $SOCAT_COUNT -gt 0 ]; then
    echo "⚠️  Warning: $SOCAT_COUNT socat processes still running"
    echo "   Run 'pkill socat' to stop all forwarding"
else
    echo "✓ No port forwarding active"
fi

# Check Dagger
if docker ps | grep -q dagger-engine; then
    echo "ℹ️  Dagger engine is still running"
else
    echo "✓ Dagger engine is not running"
fi

echo ""
echo "=== Cleanup Complete ==="
echo ""
echo "To start services again:"
echo "  1. Run 'make services' to start Dagger services"
echo "  2. Run 'source ./hack/forward-services.sh' to forward ports"