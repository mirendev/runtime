#!/bin/bash
# Script for services container

echo "=== Services Container ==="
echo ""
echo "Services available:"
echo "  - etcd:       etcd:2379"
echo "  - minio:      minio:9000"  
echo "  - clickhouse: clickhouse:9000 (native), clickhouse:8123 (HTTP)"
echo ""
echo "To check service IPs:"
echo "  getent hosts etcd minio clickhouse"
echo ""
echo "This container provides only the services."
echo "Run the runtime locally on your host for debugging."
echo ""

# Start bash shell
exec bash