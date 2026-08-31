#!/bin/sh
set -eu

echo "direct-image-metadata: cwd=$(pwd) port=$PORT"
exec python3 -m http.server "$PORT"
