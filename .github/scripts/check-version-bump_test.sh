#!/usr/bin/env bash
# Exercises check-version-bump.sh against real git states in a temp repo.
set -euo pipefail

script="$(cd "$(dirname "$0")" && pwd)/check-version-bump.sh"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
fails=0

git_() { git -C "$tmp" -c user.name=t -c user.email=t@example.com "$@"; }

reset_repo() {
  rm -rf "$tmp/.git" "$tmp"/* 2>/dev/null || true
  git init -q -b main "$tmp"
  mkdir -p "$tmp/docs" "$tmp/internal"
  echo 0.1.0 > "$tmp/VERSION"
  echo a > "$tmp/internal/a.go"
  echo a > "$tmp/docs/a.md"
  git_ add -A && git_ commit -qm base
  git_ update-ref refs/remotes/origin/main HEAD
  git_ checkout -qb feature
}

# expect <name> <want-exit 0|1>
expect() {
  local name="$1" want="$2" got=0
  (cd "$tmp" && "$script" origin/main >/dev/null 2>&1) || got=$?
  if [ "$got" -ne "$want" ]; then
    echo "FAIL $name: want exit $want, got $got"; fails=$((fails+1))
  else
    echo "ok   $name"
  fi
}

reset_repo
expect "no changes passes" 0

reset_repo; echo b > "$tmp/docs/b.md"; mkdir -p "$tmp/.memory"; echo m > "$tmp/.memory/m"; echo r >> "$tmp/README.md"
git_ add -A && git_ commit -qm docs
expect "docs, memory and markdown only pass without a bump" 0

reset_repo; echo b >> "$tmp/internal/a.go"; git_ commit -qam src
expect "source change without a bump fails" 1

reset_repo; echo b >> "$tmp/internal/a.go"; echo 0.2.0 > "$tmp/VERSION"; git_ commit -qam src
expect "source change with a bump passes" 0

reset_repo; echo b >> "$tmp/internal/a.go"; echo 0.0.9 > "$tmp/VERSION"; git_ commit -qam src
expect "source change with a lower VERSION fails" 1

reset_repo; echo b >> "$tmp/internal/a.go"; echo 0.1.1 > "$tmp/VERSION"; echo b >> "$tmp/docs/a.md"; git_ commit -qam both
expect "source and docs with a bump passes" 0

reset_repo; echo 0.1.1 > "$tmp/VERSION"; git_ commit -qam ver
expect "VERSION-only change passes" 0

reset_repo; echo b >> "$tmp/internal/a.go"; git_ commit -qam src
got=0; (cd "$tmp" && "$script" origin/nope >/dev/null 2>&1) || got=$?
if [ "$got" -eq 1 ]; then echo "ok   missing base ref fails loudly"; else echo "FAIL missing base ref: got $got"; fails=$((fails+1)); fi

exit "$fails"
