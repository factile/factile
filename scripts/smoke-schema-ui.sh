#!/usr/bin/env bash
set -euo pipefail
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ui_root="${FACTILE_UI_DIR:-$repo_root/../factile-ui}"
port="${PORT:-4387}"
curator_port="$((port + 1))"
task_root="$(mktemp -d)"
pids=()
cleanup() {
  for pid in "${pids[@]}"; do kill "$pid" 2>/dev/null || true; wait "$pid" 2>/dev/null || true; done
  rm -rf "$task_root"
}
trap cleanup EXIT
mkdir -p "$task_root/workspace" "$task_root/broken/concept-schemas" "$task_root/limited/concept-schemas"
cp -R "$repo_root/testdata/bundles/schema-profiles" "$task_root/profiles"
cat > "$task_root/workspace/factile.toml" <<'TOML'
version = 2
[workspace]
root = "."
[bundle]
name = "schema-ui"
title = "Schema UI checks"
TOML
printf '%s\n' '---' 'type: Memo' 'title: Plain note' '---' 'No profile.' > "$task_root/workspace/plain.md"
for source in profiles broken limited; do
  printf 'version = 2\n[bundle]\nname = "%s"\n' "$source" > "$task_root/$source/factile.toml"
  printf 'source = "../%s"\nwritable = true\n' "$source" > "$task_root/workspace/$source.mount.toml"
done
cp "$task_root/workspace/plain.md" "$task_root/broken/probe.md"
cp "$task_root/workspace/plain.md" "$task_root/limited/probe.md"
printf '{' > "$task_root/broken/concept-schemas/broken.schema.json"
head -c 1048577 /dev/zero | tr '\0' ' ' > "$task_root/limited/concept-schemas/large.schema.json"
cd "$repo_root"
go build -o "$task_root/factile" ./cmd/factile
PATH=/nonexistent "$task_root/factile" --workspace "$task_root/workspace" ui --no-open --port "$port" > "$task_root/reader.log" 2>&1 &
pids+=("$!")
PATH=/nonexistent "$task_root/factile" --workspace "$task_root/workspace" ui --no-open --curator --port "$curator_port" > "$task_root/curator.log" 2>&1 &
pids+=("$!")
for current_port in "$port" "$curator_port"; do
  ready=0
  for _ in $(seq 1 80); do
    if curl --max-time 1 -fsS "http://127.0.0.1:$current_port/api/local/v1/health" >/dev/null 2>&1; then ready=1; break; fi
    sleep 0.25
  done
  if [ "$ready" -ne 1 ]; then cat "$task_root/reader.log" "$task_root/curator.log"; exit 1; fi
done
cd "$ui_root"
FACTILE_EMBEDDED_URL="http://127.0.0.1:$port" FACTILE_SCHEMA_CURATOR_URL="http://127.0.0.1:$curator_port" FACTILE_SCHEMA_BRIDGE=1 npm run browser -- tests/browser/schema.pw.ts
