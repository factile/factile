#!/usr/bin/env bash
set -euo pipefail

GO="${GO:-go}"
BINARY="${BINARY:-bin/factile}"
HOST="${HOST:-127.0.0.1}"
PORT="${PORT:-4327}"
ROOT_URL="http://${HOST}:${PORT}"
LOG_FILE="${TMPDIR:-/tmp}/factile-cli-ui-smoke-${PORT}.log"

if [ ! -f "pkg/uibridge/static/index.html" ]; then
  printf 'embedded UI assets are missing; run make ui-assets first\n' >&2
  exit 1
fi

install -d "$(dirname "$BINARY")"
"$GO" build -o "$BINARY" ./cmd/factile

rm -f "$LOG_FILE"

"$BINARY" --mount-file testdata/mounts.toml ui --curator --no-open --port "$PORT" >"$LOG_FILE" 2>&1 &
server_pid="$!"

cleanup() {
  kill "$server_pid" 2>/dev/null || true
  wait "$server_pid" 2>/dev/null || true
}
trap cleanup EXIT

ready=0
for _ in $(seq 1 80); do
  if curl -fsS "$ROOT_URL/" >/tmp/factile-cli-ui-smoke-root.html 2>/dev/null; then
    ready=1
    break
  fi
  sleep 0.25
done

if [ "$ready" -ne 1 ]; then
  cat "$LOG_FILE"
  exit 1
fi

curl -fsS "$ROOT_URL/product-docs/workflows/invoice-import" >/tmp/factile-cli-ui-smoke-document.html
curl -fsS "$ROOT_URL/api/local/v1/capabilities" >/tmp/factile-cli-ui-smoke-capabilities.json
curl -fsS "$ROOT_URL/api/local/v1/reader/read?path=%2Fproduct-docs%2Fworkflows%2Finvoice-import" >/tmp/factile-cli-ui-smoke-read.json
grep -q '<div id="root">' /tmp/factile-cli-ui-smoke-root.html
grep -q '"mode":"curator"' /tmp/factile-cli-ui-smoke-capabilities.json
grep -q '"patch":true' /tmp/factile-cli-ui-smoke-capabilities.json
grep -q '"path":"/product-docs/workflows/invoice-import"' /tmp/factile-cli-ui-smoke-read.json

printf 'factile embedded UI smoke ok: %s\n' "$ROOT_URL"
