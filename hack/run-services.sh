#!/bin/bash
# Script for services container

echo "=== Services Container ==="
echo ""
echo "Services available:"
echo "  - etcd:       etcd:2379"
echo ""
echo "To check service IPs:"
echo "  getent hosts etcd"
echo ""
echo "This container provides only the services."
echo "Run the runtime locally on your host for debugging."
echo ""

# Start bash shell
exec bash
