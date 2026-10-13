#!/usr/bin/env bash
# Exercises check-image.sh with a stub docker on PATH, so no image is needed.
# The stub runs the script's in-image shell command on the host against a temp
# tree, via CHECK_ROOT.
set -euo pipefail

script="$(cd "$(dirname "$0")" && pwd)/check-image.sh"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
fails=0

mkdir -p "$tmp/bin"
cat > "$tmp/bin/docker" <<'STUB'
#!/usr/bin/env bash
# docker run ... --entrypoint sh <image> -c <cmd>: run <cmd> against $FAKE_ROOT.
[ "$1" = run ] || { echo "stub docker: unsupported: $*" >&2; exit 2; }
CHECK_ROOT="$FAKE_ROOT" exec sh -c "${@: -1}"
STUB
chmod +x "$tmp/bin/docker"

new_case() {
  rm -rf "$tmp/root" "$tmp/Dockerfile"
  mkdir -p "$tmp/root/usr/local/bin" "$tmp/root/data"
  : > "$tmp/root/usr/local/bin/gobbler"
  printf 'FROM alpine\nCOPY --from=build /out/gobbler /usr/local/bin/gobbler\nCOPY cmd ./cmd\nCOPY internal ./internal\n' > "$tmp/Dockerfile"
}

# expect <name> <want-exit 0|1> <stderr-substring-or-empty>
expect() {
  local name="$1" want="$2" needle="$3" got=0 err
  err="$(cd "$tmp" && PATH="$tmp/bin:$PATH" FAKE_ROOT="$tmp/root" "$script" fake:img "$tmp/Dockerfile" 2>&1 >/dev/null)" || got=$?
  if [ "$got" -ne "$want" ]; then
    echo "FAIL $name: want exit $want, got $got"; fails=$((fails+1))
  elif [ -n "$needle" ] && [[ "$err" != *"$needle"* ]]; then
    echo "FAIL $name: stderr lacks '$needle': $err"; fails=$((fails+1))
  else
    echo "ok   $name"
  fi
}

new_case
expect "clean image passes" 0 ""

new_case; touch "$tmp/root/usr/local/bin/node"
expect "node on PATH fails" 1 "Node runtime"

new_case; mkdir -p "$tmp/root/opt/tools"; touch "$tmp/root/opt/tools/node"
expect "node off PATH fails" 1 "/opt/tools/node"

new_case; mkdir -p "$tmp/root/app/node_modules/x"
expect "node_modules fails" 1 "node_modules"

new_case; mkdir -p "$tmp/root/app/e2e"
expect "e2e path fails" 1 "e2e paths"

new_case; echo 'COPY e2e/ /app/' >> "$tmp/Dockerfile"
expect "Dockerfile COPY of e2e fails" 1 "copies 'e2e/'"

new_case; printf 'COPY --chown=1:1 go.mod \\\n  ./e2e/fixtures /app/\n' >> "$tmp/Dockerfile"
expect "multi-line COPY under e2e fails" 1 "e2e"

new_case; echo 'COPY --from=e2ebuild /e2e /app/e2e' >> "$tmp/Dockerfile"
expect "stage copy named e2e is not a context copy" 0 ""

new_case; echo 'COPY ["e2e/", "/app/e2e/"]' >> "$tmp/Dockerfile"
expect "JSON form COPY of e2e fails" 1 "copies 'e2e/'"

new_case; echo 'ADD --chown=1:1 ["./e2e/fixtures","go.mod","/app/"]' >> "$tmp/Dockerfile"
expect "JSON form with flag, e2e among several sources fails" 1 "copies './e2e/fixtures'"

new_case; echo 'COPY ["cmd", "internal", "/app/"]' >> "$tmp/Dockerfile"
expect "JSON form not copying e2e passes" 0 ""

new_case; echo 'COPY --from=build ["/e2e", "/app/e2e"]' >> "$tmp/Dockerfile"
expect "JSON form stage copy is skipped" 0 ""

[ "$fails" -eq 0 ] || { echo "$fails failed"; exit 1; }
