#!/usr/bin/env bash
set -euo pipefail

# Push a release tag after the release PR has been merged
# Usage: hack/release-tag.sh <version> [--branch <name>] [--force] [--yes] [--dry-run]
# Examples:
#   hack/release-tag.sh v0.3.0
#   hack/release-tag.sh v0.15.1 --branch release/0.15  # Maintenance release
#   hack/release-tag.sh v0.3.0 --force   # Skip safety checks
#   hack/release-tag.sh v0.3.0 --yes     # Skip confirmation prompt
#   hack/release-tag.sh v0.3.0 --dry-run # Run every check, stop before tagging

VERSION=""
# The branch the release is cut from. Patch releases for an older line come off
# a maintenance branch such as release/0.15 instead.
BRANCH="main"
FORCE=false
YES=false
DRY_RUN=false

usage() {
  echo "Usage: $0 <version> [--branch <name>] [--force] [--yes] [--dry-run]"
  echo ""
  echo "Examples:"
  echo "  $0 v0.3.0"
  echo "  $0 v0.15.1 --branch release/0.15  # Maintenance release"
  echo "  $0 v0.3.0 --force   # Skip safety checks"
  echo "  $0 v0.3.0 --yes     # Skip confirmation prompt"
  echo "  $0 v0.3.0 --dry-run # Run every check, stop before tagging"
}

# Parse arguments
while [ $# -gt 0 ]; do
  case "$1" in
    --branch|-b)
      if [ $# -lt 2 ] || [ -z "$2" ]; then
        echo "Error: $1 requires a branch name"
        exit 1
      fi
      BRANCH="$2"
      shift 2
      ;;
    --branch=*)
      BRANCH="${1#*=}"
      if [ -z "$BRANCH" ]; then
        echo "Error: --branch requires a branch name"
        exit 1
      fi
      shift
      ;;
    --force|-f)
      FORCE=true
      shift
      ;;
    --yes|-y)
      YES=true
      shift
      ;;
    --dry-run|-n)
      DRY_RUN=true
      shift
      ;;
    -*)
      echo "Error: Unknown flag: $1"
      exit 1
      ;;
    *)
      if [ -z "$VERSION" ]; then
        VERSION="$1"
      else
        echo "Error: Unexpected argument: $1"
        exit 1
      fi
      shift
      ;;
  esac
done

if [ -z "$VERSION" ]; then
  echo "Error: Version required"
  usage
  exit 1
fi

# Validate version format
if ! [[ "$VERSION" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "Error: Invalid version format: $VERSION"
  echo "Must match: v<major>.<minor>.<patch>"
  exit 1
fi

# Resolve the repository's git dir. In a jj workspace the current directory has
# no .git of its own (jj drives a colocated git repo elsewhere), so plain git
# commands fail with "not a git repository". Detect that case and point git at
# the backing repo via `jj git root`. A normal checkout, including a colocated
# primary jj repo, keeps git's default discovery untouched.
if git rev-parse --absolute-git-dir >/dev/null 2>&1; then
  : # plain git checkout
elif command -v jj >/dev/null 2>&1 && JJ_GIT_DIR=$(jj git root --ignore-working-copy 2>/dev/null); then
  export GIT_DIR="$JJ_GIT_DIR"
else
  echo "Error: not a git repository or jj workspace"
  exit 1
fi

# Check if the tag already exists (always check this, even with --force). Origin
# is what matters, since that's where the tag is headed; a local clone may not
# have fetched it.
if git rev-parse "$VERSION" >/dev/null 2>&1; then
  echo "Error: Tag $VERSION already exists locally"
  exit 1
fi
if [ -n "$(git ls-remote --tags origin "refs/tags/$VERSION")" ]; then
  echo "Error: Tag $VERSION already exists on origin"
  exit 1
fi

# GitHub enforces review and passing checks when a pull request merges into a
# protected branch, so anything that reached one has already been vetted.
# Maintenance branches carry no protection rules, so the same two guarantees
# have to be established here instead: the commit we are about to tag must be
# the merge of a pull request that was approved and whose checks passed.
verify_target_merge() {
  local branch="$1" target="$2"

  if ! command -v gh >/dev/null 2>&1; then
    echo "Error: gh (GitHub CLI) is required to verify what landed on $branch"
    echo "Install it, or use --force to skip this check"
    exit 1
  fi

  # Resolve owner/name from the remote rather than letting gh discover it: in a
  # jj workspace the current directory has no .git for gh to find.
  local repo
  repo=$(git remote get-url origin |
    sed -E 's#^git@github\.com:##; s#^https://github\.com/##; s#\.git$##')

  local protected
  protected=$(gh api "repos/$repo/branches/$branch" --jq '.protected' 2>/dev/null || true)
  if [ "$protected" = "true" ]; then
    echo "Branch $branch is protected; GitHub enforced review and checks at merge"
    return
  fi
  if [ -z "$protected" ]; then
    echo "Warning: could not read branch protection for $branch; treating it as unprotected"
  fi

  echo "Branch $branch is unprotected; verifying review and checks for $(git rev-parse --short "$target")..."

  local pr
  pr=$(gh api "repos/$repo/commits/$target/pulls" --jq '.[0].number' 2>/dev/null || true)
  if [ -z "$pr" ] || [ "$pr" = "null" ]; then
    echo "Error: $target is not the merge of any pull request"
    echo "On an unprotected branch that means nothing reviewed it"
    echo "Or use --force to skip this check"
    exit 1
  fi

  # One call for both guarantees. statusCheckRollup mixes two shapes: checks
  # report a conclusion, older-style statuses report a state.
  local summary merge_oid review check_count failed
  summary=$(gh pr view "$pr" --repo "$repo" \
    --json reviewDecision,mergeCommit,statusCheckRollup \
    --jq '[
      (.mergeCommit.oid // ""),
      (.reviewDecision // ""),
      ([.statusCheckRollup[]?] | length),
      ([.statusCheckRollup[]?
        | select(((.conclusion // .state) // "") as $c
                 | $c != "SUCCESS" and $c != "SKIPPED" and $c != "NEUTRAL")
        | (.name // .context)] | join(", "))
    ] | @tsv')
  IFS=$'\t' read -r merge_oid review check_count failed <<<"$summary"

  if [ "$merge_oid" != "$target" ]; then
    echo "Error: PR #$pr claims $target but merged as ${merge_oid:-nothing}"
    exit 1
  fi
  if [ "$review" != "APPROVED" ]; then
    echo "Error: PR #$pr was not approved (review status: ${review:-none})"
    echo "$branch has no required-review rule, so approval is checked here"
    echo "Or use --force to skip this check"
    exit 1
  fi
  if [ "$check_count" = "0" ]; then
    echo "Error: no CI checks ran on PR #$pr"
    echo "$branch has no required-checks rule, so this is checked here"
    echo "Or use --force to skip this check"
    exit 1
  fi
  if [ -n "$failed" ]; then
    echo "Error: PR #$pr has checks that did not pass: $failed"
    echo "Or use --force to skip this check"
    exit 1
  fi

  echo "PR #$pr: approved, $check_count checks passed"
}

# What gets tagged is the tip of the release branch on origin, in both a plain
# checkout and a jj workspace. Nothing about the local checkout is consulted:
# HEAD can sit on another branch, and the tree can be dirty, because the tag is
# placed on the fetched commit rather than on whatever is checked out. That also
# means this script can tag a maintenance branch while running from a checkout
# of main, which is how it gets fixed and used in the same release.
#
# FETCH_HEAD rather than origin/$BRANCH: a colocated repo driven by jj is not
# guaranteed to carry remote-tracking refs that match what was just fetched.
echo "Fetching origin/$BRANCH..."
git fetch origin "$BRANCH"
TARGET=$(git rev-parse FETCH_HEAD)

if [ "$FORCE" = true ]; then
  echo "Warning: Skipping safety checks (--force); origin/$BRANCH targeting still runs"
else
  # Read the changelog into a variable rather than piping it. `grep -q` exits
  # on its first match, which SIGPIPEs `git show` into exit 141, and pipefail
  # turns that into a failed check even when the version is present.
  REMOTE_CHANGELOG=$(git show "$TARGET":docs/docs/changelog.md 2>/dev/null || true)

  if ! grep -q "^## $VERSION" <<<"$REMOTE_CHANGELOG"; then
    echo "Error: origin/$BRANCH changelog does not contain '## $VERSION'"
    echo "Has the release PR been merged?"
    echo "Or use --force to skip this check"
    exit 1
  fi

  verify_target_merge "$BRANCH" "$TARGET"
fi

# Show what we're about to do
echo ""
echo "======================================"
echo "Tagging release: $VERSION"
echo "======================================"
echo "Branch: $BRANCH"
echo "Commit: $(git rev-parse --short "$TARGET")"
echo ""

if [ "$DRY_RUN" = true ]; then
  echo "Dry run: would tag $TARGET as $VERSION and push it to origin"
  exit 0
fi

# Ask for confirmation
if [ "$YES" = true ]; then
  echo "Continuing (--yes)"
else
  read -p "Create and push tag $VERSION? (y/N) " -n 1 -r
  echo
  if [[ ! $REPLY =~ ^[Yy]$ ]]; then
    echo "Aborted"
    exit 1
  fi
fi

# Create and push tag
echo ""
echo "Creating tag $VERSION..."
git tag -a "$VERSION" "$TARGET" -m "Release $VERSION"

echo "Pushing tag to origin..."
git push origin "$VERSION"

echo ""
echo "======================================"
echo "✓ Tag $VERSION pushed"
echo "======================================"
echo ""
echo "The release workflow should now be running:"
echo "  https://github.com/mirendev/runtime/actions/workflows/release.yml"
echo ""
