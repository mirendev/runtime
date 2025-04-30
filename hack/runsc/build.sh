# First, build for AMD64
docker build \
  --platform linux/amd64 \
  -t ghcr.io/mirendev/runsc:latest-amd64 \
  -f Dockerfile.amd64 .

# Then build for ARM64
docker build \
  --platform linux/arm64 \
  -t ghcr.io/mirendev/runsc:latest-arm64 \
  -f Dockerfile.arm64 .

# Push both images
docker push ghcr.io/mirendev/runsc:latest-amd64
docker push ghcr.io/mirendev/runsc:latest-arm64

# Create and push the manifest list
docker manifest create ghcr.io/mirendev/runsc:latest \
  --amend ghcr.io/mirendev/runsc:latest-amd64 \
  --amend ghcr.io/mirendev/runsc:latest-arm64

# Push the manifest
docker manifest push ghcr.io/mirendev/runsc:latest

# Optional: Inspect the manifest
docker manifest inspect ghcr.io/mirendev/runsc:latest
