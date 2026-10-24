#!/bin/sh
# Retry downloads only; test failures must never be retried into a green build.
set -eu
attempt=1
while :; do
  if "$@"; then exit 0; else result=$?; fi
  [ "$attempt" -lt 3 ] || exit "$result"
  attempt=$((attempt + 1))
  echo "Download/setup failed; attempt $attempt of 3" >&2
  sleep 2
done
