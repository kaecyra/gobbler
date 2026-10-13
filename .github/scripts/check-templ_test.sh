#!/usr/bin/env bash
# Exercises check-templ.sh in a scratch module that borrows this repository's
# Makefile, go.mod and go.sum, so the real pinned templ and goimports run.
set -euo pipefail

root="$(cd "$(dirname "$0")/../.." && pwd)"
script="$root/.github/scripts/check-templ.sh"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
fails=0

fresh() {
  rm -rf "$tmp"/* 2>/dev/null || true
  mkdir -p "$tmp/internal/web/demo"
  cp "$root/Makefile" "$root/go.mod" "$root/go.sum" "$tmp/"
  # Keep the module path so goimports -local resolves; the scratch has no Go
  # packages of its own beyond the generated file.
  cat > "$tmp/internal/web/demo/demo.templ" <<'EOF'
package demo

templ Hello(name string) {
	<p>Hello { name }</p>
}
EOF
  (cd "$tmp" && make --no-print-directory generate-templ >/dev/null)
}

# expect <name> <want-exit>
expect() {
  local name="$1" want="$2" got=0
  (cd "$tmp" && bash "$script" >/dev/null 2>&1) || got=$?
  if [ "$got" -ne "$want" ]; then
    echo "FAIL $name: want exit $want, got $got"; fails=$((fails+1))
  else
    echo "ok   $name"
  fi
}

# sum prints a checksum of every generated file, to assert the tree is left as found.
sum() { (cd "$tmp" && find . -name '*_templ.go' -print0 | sort -z | xargs -0 cat | cksum); }

fresh
expect "fresh output passes" 0

fresh
sed 's/Hello/Goodbye/' "$tmp/internal/web/demo/demo.templ" > "$tmp/x" && mv "$tmp/x" "$tmp/internal/web/demo/demo.templ"
before="$(sum)"
expect "edited .templ without regenerating fails" 1
if [ "$before" != "$(sum)" ]; then echo "FAIL failed check left the tree modified"; fails=$((fails+1)); else echo "ok   failed check leaves the tree as found"; fi

fresh
rm "$tmp/internal/web/demo/demo_templ.go"
expect "missing generated file fails" 1
[ ! -e "$tmp/internal/web/demo/demo_templ.go" ] && echo "ok   missing file not left behind" || { echo "FAIL check created the missing file"; fails=$((fails+1)); }

# unchanged <name> <before-checksum>
unchanged() {
  if [ "$2" != "$(sum)" ]; then echo "FAIL $1: tree modified"; fails=$((fails+1)); else echo "ok   $1"; fi
}

fresh
echo '// hand edit' >> "$tmp/internal/web/demo/demo_templ.go"
before="$(sum)"
expect "hand-edited generated file fails" 1
unchanged "hand-edit case restores the hand-edited file" "$before"

fresh
echo 'package demo' > "$tmp/internal/web/demo/gone_templ.go"
before="$(sum)"
expect "orphaned generated file fails" 1
unchanged "orphan case leaves the orphan in place" "$before"

fresh
printf 'package demo\n\ntempl Broken( {\n' > "$tmp/internal/web/demo/broken.templ"
before="$(sum)"
expect "generator failure fails" 1
# Exit 1 alone is not enough: a half-finished generate also looks stale. Assert the reason.
out="$(cd "$tmp" && bash "$script" 2>&1 || true)"
if grep -q "generation failed" <<<"$out"; then echo "ok   generator failure is reported as such"; else echo "FAIL generator failure is not reported as such"; fails=$((fails+1)); fi
unchanged "generator failure leaves the tree as found" "$before"

# templ skips dot and underscore directories, so output there is not its to judge.
fresh
mkdir -p "$tmp/.cc-scratch" "$tmp/_skip"
echo 'package x // not ours' > "$tmp/.cc-scratch/x_templ.go"
echo 'package x // not ours' > "$tmp/_skip/y_templ.go"
expect "generated-looking files in dot and underscore directories are ignored" 0

[ "$fails" -eq 0 ] || { echo "$fails check-templ cases failed"; exit 1; }
