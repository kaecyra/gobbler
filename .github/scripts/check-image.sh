#!/usr/bin/env bash
# Fails when the production image carries a Node runtime or any path named
# e2e. ADR-00013 obligation 1: e2e/ is test tooling and never ships, and the
# production image has no Node (ADR-00017: no sidecar, nothing else in it).
#
# usage: check-image.sh <image>
set -euo pipefail

image="${1:?usage: check-image.sh <image>}"

# Runs inside the image with its own shell, bypassing the ENTRYPOINT, as root
# so find can read every directory (the image runs as a non-root user).
inspect() {
  docker run --rm --user 0 --entrypoint sh "$image" -c "$1"
}

fail=0

node_found="$(inspect 'for c in node nodejs npm npx corepack; do command -v "$c" || true; done')" || {
  echo "image check: could not run a shell in $image" >&2
  exit 1
}
if [ -n "$node_found" ]; then
  echo "image check FAILED: $image contains a Node runtime:" >&2
  echo "$node_found" | sed 's/^/  /' >&2
  fail=1
fi

e2e_found="$(inspect 'find / -path /proc -prune -o -path /sys -prune -o -name e2e -print')" || {
  echo "image check: could not search the filesystem of $image" >&2
  exit 1
}
if [ -n "$e2e_found" ]; then
  echo "image check FAILED: $image contains e2e paths (ADR-00013):" >&2
  echo "$e2e_found" | sed 's/^/  /' >&2
  fail=1
fi

[ "$fail" -eq 0 ] || exit 1
echo "image check: $image has no Node runtime and no e2e path"
