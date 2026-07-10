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
echo "synced Factile UI assets from $ui_dist to $target"
