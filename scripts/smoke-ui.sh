#!/usr/bin/env bash
set -euo pipefail

GO="${GO:-go}"
BINARY="${BINARY:-bin/factile}"
HOST="${HOST:-127.0.0.1}"
PORT="${PORT:-4327}"
CURATOR_PORT="$((PORT + 1))"
FIXTURE_SOURCE="$(pwd)/testdata/ui-smoke"
TMP_ROOT="${TMPDIR:-/tmp}/factile-cli-ui-smoke-${PORT}"
FIXTURE_ROOT="$TMP_ROOT/workspace"

if [ ! -f "pkg/uibridge/static/index.html" ]; then
  printf 'embedded UI assets are missing; run make ui-assets first\n' >&2
  exit 1
fi

install -d "$(dirname "$BINARY")"
"$GO" build -o "$BINARY" ./cmd/factile
BINARY="$(cd "$(dirname "$BINARY")" && pwd)/$(basename "$BINARY")"

rm -rf "$TMP_ROOT"
mkdir -p "$FIXTURE_ROOT"
cp -R "$FIXTURE_SOURCE/." "$FIXTURE_ROOT/"

server_pid=""

cleanup() {
  if [ -n "$server_pid" ]; then
    kill "$server_pid" 2>/dev/null || true
    wait "$server_pid" 2>/dev/null || true
  fi
}
trap cleanup EXIT

start_server() {
  local port="$1"
  local mode="$2"
  local log_file="$TMP_ROOT/$mode.log"
  local args=(--workspace "$FIXTURE_ROOT" ui --no-open --port "$port")
  if [ "$mode" = "curator" ]; then
    args+=(--curator)
  fi
  PATH=/nonexistent "$BINARY" "${args[@]}" >"$log_file" 2>&1 &
  server_pid="$!"

  local ready=0
  for _ in $(seq 1 80); do
    if curl -fsS "http://${HOST}:${port}/api/local/v1/health" >"$TMP_ROOT/$mode-health.json" 2>/dev/null; then
      ready=1
      break
    fi
    sleep 0.25
  done
  if [ "$ready" -ne 1 ]; then
    cat "$log_file"
    exit 1
  fi
}

stop_server() {
  cleanup
  server_pid=""
}

start_server "$PORT" reader
ROOT_URL="http://${HOST}:${PORT}"

curl -fsS "$ROOT_URL/" >"$TMP_ROOT/root.html"
curl -fsS "$ROOT_URL/guides/onboarding?view=support" >"$TMP_ROOT/deep-route.html"
cmp "$TMP_ROOT/root.html" "$TMP_ROOT/deep-route.html"
grep -q '<div id="root">' "$TMP_ROOT/root.html"

script_asset="$(sed -n 's/.*src="\([^"]*\.js\)".*/\1/p' "$TMP_ROOT/root.html" | head -n 1)"
style_asset="$(sed -n 's/.*href="\([^"]*\.css\)".*/\1/p' "$TMP_ROOT/root.html" | head -n 1)"
test -n "$script_asset"
test -n "$style_asset"
curl -fsS "$ROOT_URL$script_asset" >"$TMP_ROOT/app.js"
curl -fsS "$ROOT_URL$style_asset" >"$TMP_ROOT/app.css"

curl -fsS "$ROOT_URL/api/local/v1/capabilities" >"$TMP_ROOT/reader-capabilities.json"
curl -fsS "$ROOT_URL/api/local/v1/source" >"$TMP_ROOT/source.json"
curl -fsS "$ROOT_URL/api/local/v1/views" >"$TMP_ROOT/views.json"
curl -fsS "$ROOT_URL/api/local/v1/view?id=support" >"$TMP_ROOT/view.json"
curl -fsS "$ROOT_URL/api/local/v1/reader/list?path=%2F&brief=true" >"$TMP_ROOT/list-root.json"
curl -fsS "$ROOT_URL/api/local/v1/reader/list?path=%2Frunbooks&view=support" >"$TMP_ROOT/list-branch.json"
curl -fsS "$ROOT_URL/api/local/v1/reader/read?path=%2Fguides%2Fonboarding" >"$TMP_ROOT/reader-read.json"
curl -fsS -H 'Content-Type: application/json' --data '{"path":"/","query":"invoice"}' "$ROOT_URL/api/local/v1/reader/search" >"$TMP_ROOT/search.json"
curl -fsS -H 'Content-Type: application/json' --data '{"path":"/","query":"invoice","depth":1}' "$ROOT_URL/api/local/v1/reader/context" >"$TMP_ROOT/context.json"
curl -fsS "$ROOT_URL/api/local/v1/reader/graph?path=%2Fguides%2Fonboarding&depth=1" >"$TMP_ROOT/graph.json"
curl -fsS "$ROOT_URL/api/local/v1/reader/validate?path=%2Fguides%2Fonboarding" >"$TMP_ROOT/validate.json"

grep -q '"mode":"reader"' "$TMP_ROOT/reader-capabilities.json"
grep -q '"patch":false' "$TMP_ROOT/reader-capabilities.json"
grep -q '"title":"Local Factile workspace"' "$TMP_ROOT/source.json"
grep -Fq "$FIXTURE_ROOT" "$TMP_ROOT/source.json"
grep -q '"id":"support"' "$TMP_ROOT/views.json"
grep -q '"id":"support"' "$TMP_ROOT/view.json"
grep -q '"path":"/"' "$TMP_ROOT/list-root.json"
grep -q '"path":"/runbooks"' "$TMP_ROOT/list-branch.json"
grep -q '"path":"/guides/onboarding"' "$TMP_ROOT/reader-read.json"
grep -q '"results"' "$TMP_ROOT/search.json"
grep -q '"concepts"' "$TMP_ROOT/context.json"
grep -q '"edges"' "$TMP_ROOT/graph.json"
grep -q '"issues"' "$TMP_ROOT/validate.json"

writer_status="$(curl -sS -o "$TMP_ROOT/reader-writer.json" -w '%{http_code}' -H 'Content-Type: application/json' --data '{"path":"/guides/onboarding"}' "$ROOT_URL/api/local/v1/writer/patch")"
test "$writer_status" = "501"
grep -q '"code":"unsupported_operation"' "$TMP_ROOT/reader-writer.json"
stop_server

start_server "$CURATOR_PORT" curator
CURATOR_URL="http://${HOST}:${CURATOR_PORT}"
curl -fsS "$CURATOR_URL/api/local/v1/capabilities" >"$TMP_ROOT/curator-capabilities.json"
curl -fsS "$CURATOR_URL/api/local/v1/reader/read?path=%2Fguides%2Fonboarding" >"$TMP_ROOT/curator-read.json"
grep -q '"mode":"curator"' "$TMP_ROOT/curator-capabilities.json"
grep -q '"patch":true' "$TMP_ROOT/curator-capabilities.json"
cmp "$TMP_ROOT/reader-read.json" "$TMP_ROOT/curator-read.json"
stop_server

printf 'factile embedded UI smoke ok: %s (reader) and %s (curator)\n' "$ROOT_URL" "$CURATOR_URL"
