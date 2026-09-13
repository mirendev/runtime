#!/usr/bin/env bash
#
# Resolve the checkout's branch, commit, and exact tag for version stamping.
# Pre-set GIT_BRANCH / GIT_COMMIT / GIT_TAG win (CI exports them), then git,
# then jj for a non-colocated workspace where git sees no repository at all.
# jj has no current branch, so the bookmarks on the parent of the working
# copy stand in for it, and "HEAD" mirrors git's detached answer when there
# are none. Falls back to branch "dev" and an empty commit outside any VCS.
#
# Usage: source hack/vcs-info.sh        # exports GIT_BRANCH GIT_COMMIT GIT_TAG
#        hack/vcs-info.sh branch|commit|tag

vcs_in_git() { git rev-parse --git-dir >/dev/null 2>&1; }
vcs_in_jj() { command -v jj >/dev/null 2>&1 && jj workspace root >/dev/null 2>&1; }

vcs_branch() {
  if [ -n "${GIT_BRANCH:-}" ]; then
    echo "$GIT_BRANCH"
  elif vcs_in_git; then
    git rev-parse --abbrev-ref HEAD
  elif vcs_in_jj; then
    local b
    b=$(jj log -r @- -n 1 --no-graph --color=never -T 'local_bookmarks.map(|b| b.name()).join(" ")' 2>/dev/null || true)
    # A version string wants one name, so take the first bookmark; deploy
    # provenance (pkg/git) keeps all of them since it is recording, not naming.
    echo "${b%% *}" | sed 's/^$/HEAD/'
  else
    echo "dev"
  fi
}

vcs_commit() {
  if [ -n "${GIT_COMMIT:-}" ]; then
    echo "$GIT_COMMIT"
  elif vcs_in_git; then
    git rev-parse HEAD
  elif vcs_in_jj; then
    jj log -r @- -n 1 --no-graph --color=never -T 'commit_id' 2>/dev/null || true
  fi
}

vcs_tag() {
  if [ -n "${GIT_TAG:-}" ]; then
    echo "$GIT_TAG"
  elif vcs_in_git; then
    git describe --exact-match --tags HEAD 2>/dev/null || true
  elif vcs_in_jj; then
    local t
    t=$(jj log -r @- -n 1 --no-graph --color=never -T 'tags.map(|t| t.name()).join(" ")' 2>/dev/null || true)
    echo "${t%% *}"
  fi
}

case "${1:-}" in
  branch) vcs_branch ;;
  commit) vcs_commit ;;
  tag) vcs_tag ;;
  "")
    GIT_BRANCH=$(vcs_branch)
    GIT_COMMIT=$(vcs_commit)
    GIT_TAG=$(vcs_tag)
    export GIT_BRANCH GIT_COMMIT GIT_TAG
    ;;
  *) echo "usage: $0 [branch|commit|tag]" >&2; exit 2 ;;
esac
