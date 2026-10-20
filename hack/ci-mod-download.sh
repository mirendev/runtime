#!/usr/bin/env bash
# Download every module both go.mod files need into the current module cache.
#
# CI runs this in every job that can save a module cache, before the job's real
# work. Many jobs share one cache key and the first to save wins, so a job that
# only builds a few packages would otherwise save a cache missing most modules,
# and every later run would restore it and fetch the rest from the proxy again.
# Filling it here means whichever job wins the save, the cache is complete.
#
# The retries cover what GOPROXY's |direct fallback can't: a module whose tag
# moved upstream after the proxy cached it (github.com/sacloud/packages-go
# v0.0.11 is one) only verifies from the proxy, so a reset stream on it has to
# go back to the proxy rather than to the origin. Modules that already
# downloaded stay in the cache, so a retry only fetches what is missing.

set -euo pipefail

cd "$(dirname "$0")/.."

for attempt in 1 2 3; do
  if go mod download && (cd x && go mod download); then
    exit 0
  fi
  echo "module download attempt $attempt failed" >&2
  sleep $((attempt * 5))
done
exit 1
