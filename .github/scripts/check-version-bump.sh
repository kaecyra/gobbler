#!/usr/bin/env bash
# Fails when a branch changes source relative to the trunk without raising
# VERSION (.claude/rules/git-workflow.md, "Versioning"). Docs, ADRs, .memory/,
# Markdown and agent-rule files never need a bump.
#
# usage: check-version-bump.sh [base-ref]   (default: origin/main)
set -euo pipefail

base_ref="${1:-${VERSION_BASE:-origin/main}}"

if ! git rev-parse --verify --quiet "$base_ref^{commit}" >/dev/null; then
  echo "version check: base ref $base_ref not found; fetch it (git fetch origin main) or pass another base" >&2
  exit 1
fi
base="$(git merge-base "$base_ref" HEAD)" || {
  echo "version check: no merge base between $base_ref and HEAD (shallow clone? fetch full history)" >&2
  exit 1
}

# Files that never need a bump.
is_exempt() {
  case "$1" in
    docs/*|.memory/*|.claude/*|*.md) return 0 ;;
    *) return 1 ;;
  esac
}

source_changes=()
while IFS= read -r f; do
  [ -z "$f" ] && continue
  if [ "$f" != "VERSION" ] && ! is_exempt "$f"; then
    source_changes+=("$f")
  fi
done < <(git diff --name-only "$base" HEAD)

if [ "${#source_changes[@]}" -eq 0 ]; then
  echo "version check: no source changes against $base_ref; no bump needed"
  exit 0
fi

old="$(git show "$base:VERSION" 2>/dev/null | tr -d '[:space:]' || true)"
new="$(tr -d '[:space:]' < VERSION)"
if [ -z "$old" ]; then
  echo "version check: VERSION does not exist at $base_ref; nothing to bump against"
  exit 0
fi
if [ "$old" = "$new" ]; then
  echo "version check: source changed but VERSION is still $old (bump it once per shipped unit). Changed:" >&2
  printf '  %s\n' "${source_changes[@]}" >&2
  exit 1
fi
highest="$(printf '%s\n%s\n' "$old" "$new" | sort -V | tail -n 1)"
if [ "$highest" != "$new" ]; then
  echo "version check: VERSION went backwards ($old -> $new)" >&2
  exit 1
fi
echo "version check: VERSION $old -> $new"
