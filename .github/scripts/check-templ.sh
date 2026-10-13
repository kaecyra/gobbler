#!/usr/bin/env bash
# Fails if the committed *_templ.go files are stale against their .templ
# sources. Run from the repository root.
#
# templ has no diff mode, so this regenerates in place (make generate-templ,
# the same recipe make generate uses), compares against a snapshot taken
# first, and restores the snapshot on every exit path, so the tree is left as
# it was found whether the check passes, fails or the generator itself breaks.
set -euo pipefail

snap="$(mktemp -d)"
# The file set comes from the Makefile so it matches what templ generates.
list() { make --no-print-directory -s templ-files | sort; }

restore() {
  list | while read -r f; do rm -f "$f"; done
  (cd "$snap" && tar cf - .) | tar xf -
  rm -rf "$snap"
}
trap restore EXIT

list | tar cf - -T - | tar xf - -C "$snap"

make --no-print-directory generate-templ >/dev/null || { echo "templ generation failed; cannot check staleness"; exit 1; }

# templ also deletes output whose .templ is gone, so an orphan shows up here as a
# changed file too.
stale=0
before="$(cd "$snap" && find . -type f)"
for f in $( (echo "$before"; list) | sort -u); do
  if ! cmp -s "$snap/$f" "$f"; then
    echo "stale templ output: ${f#./}"
    stale=1
  fi
done

if [ "$stale" -ne 0 ]; then
  echo "templ output is stale; run make generate and commit the result"
  exit 1
fi
