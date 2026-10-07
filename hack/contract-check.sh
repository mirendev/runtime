#!/usr/bin/env bash
# Check that the generated cloud export contract doesn't remove or retype
# anything cloud already knows. The baseline is the copy cloud embeds on its
# main branch, read through the GitHub API, so the check moves forward on its
# own once cloud ships a matching change. Cloud's repo is private: locally this
# uses your gh login, and in CI it needs GH_TOKEN with read access to it.

set -euo pipefail

cd "$(dirname "$0")/.."

CONTRACT="api/core/core_v1alpha/cloud-export.gen.json"
CLOUD_REPO="mirendev/cloud"
CLOUD_CONTRACT="services/entitysync/cloud-export.json"

# Fork PRs don't receive secrets, so they skip. Release runs this workflow on
# main with secrets inherited, which catches a fork's break after merge rather
# than letting it ride silently into a release. Any other run without a token
# is a misconfiguration, and passing it would quietly switch the check off.
if [ "${GITHUB_ACTIONS:-}" = "true" ] && [ -z "${GH_TOKEN:-}" ]; then
	if [ "${FORK_PR:-}" = "true" ]; then
		echo "::notice title=Cloud export contract::Skipped: fork PRs can't read $CLOUD_REPO."
		exit 0
	fi
	echo "::error title=Cloud export contract::No GH_TOKEN to read $CLOUD_REPO; is the CLOUD_REPO_TOKEN secret set?"
	exit 1
fi

known="$(mktemp)"
trap 'rm -f "$known"' EXIT

if ! gh api "repos/$CLOUD_REPO/contents/$CLOUD_CONTRACT" \
	-H "Accept: application/vnd.github.raw" >"$known"; then
	echo "could not read $CLOUD_CONTRACT from $CLOUD_REPO (is gh logged in with access to it?)" >&2
	exit 1
fi

go run ./hack/cmd/contractcheck "$known" "$CONTRACT"
