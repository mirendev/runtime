#!/bin/bash
# Script for services container

echo "=== Services Container ==="
echo ""
echo "Services available:"
echo "  - etcd:       etcd:2379"
echo "  - clickhouse: clickhouse:9000 (native), clickhouse:8123 (HTTP)"
echo ""
echo "To check service IPs:"
echo "  getent hosts etcd clickhouse"
echo ""
echo "This container provides only the services."
echo "Run the runtime locally on your host for debugging."
echo ""

# Start bash shell
exec bash
