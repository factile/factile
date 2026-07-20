#!/usr/bin/env bash
set -euo pipefail

if ! command -v factile >/dev/null 2>&1; then
  echo 'factile is not on PATH' >&2
  exit 1
fi

factile status --json
factile list / --json
factile list / --brief --json
if [ "$#" -gt 0 ]; then
  factile context / "$*" --json
fi
