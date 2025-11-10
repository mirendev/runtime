#!/usr/bin/env bash
set -euo pipefail

# Upload speed test for measuring baseline network performance
# Usage: ./test-upload-speed.sh <host> [size_mb]

if [ $# -lt 1 ]; then
    echo "Usage: $0 <host> [size_mb]"
    echo "Example: $0 my-remote-server.example.com 100"
    exit 1
fi

HOST="$1"
SIZE_MB="${2:-100}"
REMOTE_USER="${REMOTE_USER:-phinze}"

echo "Testing upload speed to $HOST (user: $REMOTE_USER)"
echo "File size: ${SIZE_MB}MB"
echo "---"

# Create test file
TEST_FILE="/tmp/upload-test-$(date +%s).bin"
echo "Creating ${SIZE_MB}MB test file..."
dd if=/dev/urandom of="$TEST_FILE" bs=1M count="$SIZE_MB" 2>&1 | grep -v records

FILE_SIZE=$(stat -c%s "$TEST_FILE" 2>/dev/null || stat -f%z "$TEST_FILE" 2>/dev/null)
echo "Test file size: $(numfmt --to=iec-i --suffix=B $FILE_SIZE 2>/dev/null || echo "$FILE_SIZE bytes")"
echo ""

# Test 1: Direct SCP upload (baseline)
echo "Test 1: Direct SCP upload (no compression)"
START=$(date +%s.%N)
scp -o Compression=no "$TEST_FILE" "${REMOTE_USER}@${HOST}:/tmp/test-upload.bin" 2>&1 | tail -1 || true
END=$(date +%s.%N)
DURATION=$(awk "BEGIN {printf \"%.2f\", $END - $START}")
SPEED=$(awk "BEGIN {printf \"%.2f\", $FILE_SIZE / $DURATION / 1024 / 1024}")
echo "Duration: ${DURATION}s"
echo "Speed: ${SPEED} MB/s"
echo ""

# Test 2: Tar stream through SSH pipe (similar to BuildFromTar)
echo "Test 2: Tar stream through SSH pipe"
START=$(date +%s.%N)
tar -cf - -C "$(dirname $TEST_FILE)" "$(basename $TEST_FILE)" | \
  ssh "${REMOTE_USER}@${HOST}" "cat > /tmp/test-stream.tar"
END=$(date +%s.%N)
DURATION=$(awk "BEGIN {printf \"%.2f\", $END - $START}")
TAR_SIZE=$(ssh "${REMOTE_USER}@${HOST}" "stat -c%s /tmp/test-stream.tar")
SPEED=$(awk "BEGIN {printf \"%.2f\", $TAR_SIZE / $DURATION / 1024 / 1024}")
echo "Duration: ${DURATION}s"
echo "Tar size: $(numfmt --to=iec-i --suffix=B $TAR_SIZE 2>/dev/null || echo "$TAR_SIZE bytes")"
echo "Speed: ${SPEED} MB/s"
echo "Overhead: $(awk "BEGIN {printf \"%.1f%%\", ($TAR_SIZE - $FILE_SIZE) * 100.0 / $FILE_SIZE}")"
echo ""

# Test 3: Piped gzip through SSH (compressed streaming)
echo "Test 3: Gzipped stream through SSH pipe"
START=$(date +%s.%N)
gzip -c "$TEST_FILE" | \
  ssh "${REMOTE_USER}@${HOST}" "cat > /tmp/test-gzip.gz"
END=$(date +%s.%N)
DURATION=$(awk "BEGIN {printf \"%.2f\", $END - $START}")
GZ_SIZE=$(ssh "${REMOTE_USER}@${HOST}" "stat -c%s /tmp/test-gzip.gz")
SPEED=$(awk "BEGIN {printf \"%.2f\", $GZ_SIZE / $DURATION / 1024 / 1024}")
RATIO=$(awk "BEGIN {printf \"%.1f%%\", $GZ_SIZE * 100.0 / $FILE_SIZE}")
echo "Duration: ${DURATION}s"
echo "Compressed size: $(numfmt --to=iec-i --suffix=B $GZ_SIZE 2>/dev/null || echo "$GZ_SIZE bytes") (${RATIO} of original)"
echo "Speed: ${SPEED} MB/s (of compressed data)"
echo "Effective speed: $(awk "BEGIN {printf \"%.2f\", $FILE_SIZE / $DURATION / 1024 / 1024}") MB/s (of original data)"
echo ""

# Cleanup
echo "Cleaning up..."
rm -f "$TEST_FILE"
ssh "${REMOTE_USER}@${HOST}" "rm -f /tmp/test-upload.bin /tmp/test-stream.tar /tmp/test-gzip.gz"

echo "---"
echo "Summary:"
echo "  Direct SCP: ${SPEED} MB/s baseline"
echo "  Tar streaming: Most similar to BuildFromTar's approach"
echo "  Gzipped: Shows potential if compression is added"
echo ""
echo "Compare these speeds with your BuildFromTar performance to identify overhead"
