#!/usr/bin/env bash
set -euo pipefail

# Release script for creating version tags following RFD-40 conventions
# Usage: hack/release.sh <version>
# Examples:
#   hack/release.sh v0.0.0-test.1    # Test release
#   hack/release.sh v0.1.0           # Preview release
#   hack/release.sh v1.0.0           # GA release

VERSION="${1:-}"

if [ -z "$VERSION" ]; then
  echo "Error: Version required"
  echo "Usage: $0 <version>"
  echo ""
  echo "Examples:"
  echo "  $0 v0.0.0-test.1    # Test release"
  echo "  $0 v0.1.0           # Preview release"
  echo "  $0 v1.0.0           # GA release"
  exit 1
fi

# Validate version format
if ! [[ "$VERSION" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$ ]]; then
  echo "Error: Invalid semver format: $VERSION"
  echo "Must match: v<major>.<minor>.<patch>[-<prerelease>]"
  exit 1
fi

# Check we're on main branch
CURRENT_BRANCH=$(git rev-parse --abbrev-ref HEAD)
if [ "$CURRENT_BRANCH" != "main" ]; then
  echo "Error: Must be on main branch (currently on: $CURRENT_BRANCH)"
  echo "Run: git checkout main"
  exit 1
fi

# Check working directory is clean
if [ -n "$(git status --porcelain)" ]; then
  echo "Error: Working directory has uncommitted changes"
  git status --short
  exit 1
fi

# Check we're up to date with origin
echo "Fetching from origin..."
git fetch origin main

LOCAL=$(git rev-parse main)
REMOTE=$(git rev-parse origin/main)

if [ "$LOCAL" != "$REMOTE" ]; then
  echo "Error: Local main is not up to date with origin/main"
  echo "Run: git pull origin main"
  exit 1
fi

# Check if tag already exists
if git rev-parse "$VERSION" >/dev/null 2>&1; then
  echo "Error: Tag $VERSION already exists"
  exit 1
fi

# Determine release type
RELEASE_TYPE=""
if [[ "$VERSION" =~ ^v0\.0\.0-test\. ]]; then
  RELEASE_TYPE="Test release"
elif [[ "$VERSION" =~ ^v[0-9]+\.[0-9]+\.[0-9]+- ]]; then
  RELEASE_TYPE="Prerelease"
elif [[ "$VERSION" =~ ^v0\. ]]; then
  RELEASE_TYPE="Preview release"
else
  RELEASE_TYPE="GA release"
fi

# Show what we're about to do
echo ""
echo "======================================"
echo "Creating $RELEASE_TYPE: $VERSION"
echo "======================================"
echo "Branch: $CURRENT_BRANCH"
echo "Commit: $(git rev-parse --short HEAD)"
echo ""

# Ask for confirmation
read -p "Continue? (y/N) " -n 1 -r
echo
if [[ ! $REPLY =~ ^[Yy]$ ]]; then
  echo "Aborted"
  exit 1
fi

# Create annotated tag
TAG_MESSAGE="Release $VERSION

$RELEASE_TYPE created from main branch"

git tag -a "$VERSION" -m "$TAG_MESSAGE"

echo ""
echo "✓ Created annotated tag: $VERSION"
echo ""

# Push the tag
echo "Pushing tag to origin..."
git push origin "$VERSION"

echo ""
echo "======================================"
echo "✓ Release tag pushed successfully"
echo "======================================"
echo ""
echo "What happens next:"
echo "1. Test workflow runs: https://github.com/mirendev/runtime/actions"
echo "2. Once tests pass, release workflow builds and uploads"
if [[ "$VERSION" =~ ^v[0-9]+\.[0-9]+\.[0-9]+- ]]; then
  echo "3. Artifacts uploaded to: $VERSION (prereleases don't update 'latest')"
else
  echo "3. Artifacts uploaded to: $VERSION and latest"
fi
echo "4. Slack notification sent with deployment details"
echo ""
echo "Monitor progress:"
echo "  https://github.com/mirendev/runtime/actions"
echo ""
