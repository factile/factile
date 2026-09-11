#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ui_dist="${FACTILE_UI_DIST:-"$repo_root/../factile-ui/apps/local/dist"}"
target="$repo_root/pkg/uibridge/static"

if [ ! -f "$ui_dist/index.html" ]; then
  echo "missing Factile UI build at $ui_dist" >&2
  echo "run npm run build in the factile-ui repository first" >&2
  exit 1
fi

rm -rf "$target"
mkdir -p "$target"
cp -R "$ui_dist"/. "$target"/
ui_root="$(cd "$ui_dist/../../.." && pwd)"
if git -C "$ui_root" rev-parse --verify HEAD >/dev/null 2>&1; then
  ui_revision="$(git -C "$ui_root" rev-parse HEAD)"
  ui_dirty=false
  if [ -n "$(git -C "$ui_root" status --porcelain)" ]; then ui_dirty=true; fi
  printf '{"revision":"%s","worktree":%s}\n' "$ui_revision" "$ui_dirty" > "$target/factile-ui-source.json"
fi
echo "synced Factile UI assets from $ui_dist to $target"
