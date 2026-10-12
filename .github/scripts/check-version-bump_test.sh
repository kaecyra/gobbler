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

# Main moves past the branch's bump while the branch is still cut from the old base.
reset_repo; echo b >> "$tmp/internal/a.go"; echo 0.2.0 > "$tmp/VERSION"; git_ commit -qam src
git_ checkout -q main; echo 0.2.0 > "$tmp/VERSION"; echo c >> "$tmp/internal/a.go"; git_ commit -qam "other unit"
git_ update-ref refs/remotes/origin/main HEAD; git_ checkout -q feature
expect "main moved past the branch's bump fails" 1

reset_repo; echo 0.1.1 > "$tmp/VERSION"; git_ commit -qam ver
expect "VERSION-only change passes" 0

reset_repo; git_ mv internal/a.go docs/moved.go; git_ commit -qm "move source into docs"
expect "moving source into an exempt path without a bump fails" 1

reset_repo; git_ rm -q internal/a.go; git_ commit -qm "delete source"
expect "deleting source without a bump fails" 1

reset_repo; echo b >> "$tmp/internal/a.go"; git_ commit -qam src; echo 0.2.0 > "$tmp/VERSION"
expect "an uncommitted VERSION bump does not count" 1

reset_repo; echo b >> "$tmp/internal/a.go"; echo 0.2 > "$tmp/VERSION"; git_ commit -qam src
expect "malformed VERSION fails" 1

reset_repo; echo b >> "$tmp/internal/a.go"; echo 0.2.0-rc1 > "$tmp/VERSION"; git_ commit -qam src
expect "VERSION with an rc suffix fails" 1

reset_repo; echo b >> "$tmp/internal/a.go"; echo 0.10.0 > "$tmp/VERSION"; git_ commit -qam src
expect "numeric comparison: 0.10.0 is above 0.1.0" 0

reset_repo; echo 0.9.0 > "$tmp/VERSION"; git_ commit -qam ver; git_ update-ref refs/remotes/origin/main HEAD
echo b >> "$tmp/internal/a.go"; echo 0.10.0 > "$tmp/VERSION"; git_ commit -qam src
expect "numeric comparison: 0.10.0 is above 0.9.0" 0

reset_repo; echo .env >> "$tmp/.gitignore"; git_ add -A; git_ commit -qm ignore
expect ".gitignore only passes without a bump" 0

reset_repo; echo .env >> "$tmp/internal/.gitignore"; git_ add -A; git_ commit -qm ignore
expect "nested .gitignore only passes without a bump" 0

reset_repo; echo .env >> "$tmp/.gitignore"; echo b >> "$tmp/internal/a.go"; git_ add -A; git_ commit -qm both
expect ".gitignore plus source without a bump fails" 1

reset_repo; echo b >> "$tmp/internal/a.go"; git_ commit -qam src
got=0; (cd "$tmp" && "$script" origin/nope >/dev/null 2>&1) || got=$?
if [ "$got" -eq 1 ]; then echo "ok   missing base ref fails loudly"; else echo "FAIL missing base ref: got $got"; fails=$((fails+1)); fi

exit "$fails"
