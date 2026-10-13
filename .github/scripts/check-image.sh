#!/usr/bin/env bash
# Fails when the production image carries a Node runtime anywhere on its
# filesystem, or any e2e content. ADR-00013 obligation 1: the Dockerfile copies
# nothing from e2e/ and the image contains no Node runtime. The image is one
# static Go binary plus tesseract (ADR-00002).
#
# usage: check-image.sh <image> [dockerfile]   (dockerfile default: Dockerfile)
set -euo pipefail

image="${1:?usage: check-image.sh <image> [dockerfile]}"
dockerfile="${2:-Dockerfile}"

# Runs inside the image with its own shell, bypassing the ENTRYPOINT, as root
# so find can read every directory (the image runs as a non-root user).
inspect() {
  docker run --rm --user 0 --entrypoint sh "$image" -c "$1"
}

fail=0

# Node, wherever it sits: executables, package trees and the shared library.
# CHECK_ROOT exists so the test can point the same commands at a temp tree.
node_found="$(inspect 'root="${CHECK_ROOT:-/}"
find "$root" -path "${root%/}/proc" -prune -o -path "${root%/}/sys" -prune -o \
  \( \( \( -type f -o -type l \) \( -name node -o -name nodejs -o -name npm -o -name npx -o -name corepack -o -name "libnode*" \) \) \
     -o \( -type d -name node_modules \) \) -print')" || {
  echo "image check: could not search the filesystem of $image" >&2
  exit 1
}
if [ -n "$node_found" ]; then
  echo "image check FAILED: $image contains a Node runtime:" >&2
  echo "$node_found" | sed 's/^/  /' >&2
  fail=1
fi

e2e_found="$(inspect 'root="${CHECK_ROOT:-/}"
find "$root" -path "${root%/}/proc" -prune -o -path "${root%/}/sys" -prune -o -name e2e -print')" || {
  echo "image check: could not search the filesystem of $image" >&2
  exit 1
}
if [ -n "$e2e_found" ]; then
  echo "image check FAILED: $image contains e2e paths (ADR-00013):" >&2
  echo "$e2e_found" | sed 's/^/  /' >&2
  fail=1
fi

# Static check: no COPY/ADD source from the build context is e2e or under it.
# Stage copies (--from=) read another stage, not the context, and are skipped.
# Both the shell form and the JSON form (COPY ["src", "dest"]) are read.
if [ ! -f "$dockerfile" ]; then
  echo "image check: Dockerfile $dockerfile not found" >&2
  exit 1
fi
copies="$(awk '
  { line = $0 }
  cont { buf = buf " " line } !cont { buf = line }
  { if (buf ~ /\\[ \t]*$/) { sub(/\\[ \t]*$/, "", buf); cont = 1; next } cont = 0 }
  toupper(buf) ~ /^[ \t]*(COPY|ADD)[ \t]/ {
    n = split(buf, t, /[ \t]+/); skip = 0; srcs = ""; cnt = 0
    for (i = 1; i <= n; i++) {
      if (t[i] == "" || toupper(t[i]) == "COPY" || toupper(t[i]) == "ADD") continue
      if (t[i] ~ /^--from=/) { skip = 1; continue }
      if (t[i] ~ /^--/) continue
      if (t[i] ~ /^\[/) {
        # JSON (exec) form: the rest of the line is one array of quoted strings.
        rest = buf; sub(/^[ \t]*[A-Za-z]+[ \t]+/, "", rest)
        while (rest ~ /^--[^ \t]*[ \t]+/) sub(/^--[^ \t]*[ \t]+/, "", rest)
        while (match(rest, /"[^"]*"/)) {
          arg[++cnt] = substr(rest, RSTART + 1, RLENGTH - 2)
          rest = substr(rest, RSTART + RLENGTH)
        }
        break
      }
      arg[++cnt] = t[i]
    }
    if (!skip) for (i = 1; i < cnt; i++) print arg[i]
  }' "$dockerfile")"
while IFS= read -r src; do
  [ -z "$src" ] && continue
  norm="${src#./}"
  norm="${norm#\"}"; norm="${norm%\"}"
  case "$norm" in
    e2e|e2e/*)
      echo "image check FAILED: $dockerfile copies '$src' from e2e/ into the image (ADR-00013)" >&2
      fail=1 ;;
  esac
done <<< "$copies"

[ "$fail" -eq 0 ] || exit 1
echo "image check: $image has no Node runtime and no e2e content"
