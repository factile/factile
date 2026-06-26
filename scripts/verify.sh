#!/usr/bin/env bash
set -euo pipefail

export GOCACHE="${GOCACHE:-/tmp/factile-go-build-cache}"
mkdir -p "$GOCACHE"

repo_root="$(pwd)"
tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT

unformatted="$(gofmt -l .)"
if [ -n "$unformatted" ]; then
  echo "gofmt required for:" >&2
  echo "$unformatted" >&2
  exit 1
fi

go test ./...
go vet ./...
GOOS=linux GOARCH=amd64 go build -o "$tmpdir/factile-linux-amd64" ./cmd/factile
GOOS=darwin GOARCH=amd64 go build -o "$tmpdir/factile-darwin-amd64" ./cmd/factile
GOOS=windows GOARCH=amd64 go build -o "$tmpdir/factile-windows-amd64.exe" ./cmd/factile
go build -o "$tmpdir/factile" ./cmd/factile

factile_bin="$tmpdir/factile"

mkdir -p "$tmpdir/bundles"
cp -R ./testdata/bundles/product-docs "$tmpdir/bundles/product-docs"
cp -R ./testdata/bundles/broken-docs "$tmpdir/bundles/broken-docs"

cat > "$tmpdir/mounts.toml" <<EOF2
[mounts."/product-docs"]
source = "$tmpdir/bundles/product-docs"
kind = "local"
writable = true

[mounts."/broken-docs"]
source = "$tmpdir/bundles/broken-docs"
kind = "local"
writable = true
EOF2

"$factile_bin" --help >/dev/null
"$factile_bin" version >/dev/null
"$factile_bin" --version >/dev/null
"$factile_bin" skill list --json >/dev/null
"$factile_bin" skill inspect codex --json >/dev/null

skill_workspace="$tmpdir/skill-workspace"
mkdir -p "$skill_workspace"
(
  cd "$skill_workspace"
  PATH="$tmpdir:$PATH" "$factile_bin" skill install codex --scope repo --json >/dev/null
)

curator_skill_workspace="$tmpdir/curator-skill-workspace"
mkdir -p "$curator_skill_workspace"
(
  cd "$curator_skill_workspace"
  PATH="$tmpdir:$PATH" "$factile_bin" skill install codex --scope repo --mode curator --profile software --json >/dev/null
)

init_workspace="$tmpdir/init-workspace"
mkdir -p "$init_workspace/.codex"
(
  cd "$init_workspace"
  PATH="$tmpdir:$PATH" "$factile_bin" init --json >/dev/null
  "$factile_bin" list / --json >/dev/null
  "$factile_bin" list / --brief --json >/dev/null
  "$factile_bin" stat /project --json >/dev/null
  "$factile_bin" kb list --json >/dev/null
  "$factile_bin" kb inspect /project --json >/dev/null
  "$factile_bin" read /project/overview --json >/dev/null
)

catalog_workspace="$tmpdir/catalog-workspace"
mkdir -p "$catalog_workspace"
(
  cd "$catalog_workspace"
  "$factile_bin" kb create /engineering --title "Engineering" --json >/dev/null
  "$factile_bin" kb link /engineering "$tmpdir/bundles/product-docs" /engineering/docs --title "Product Docs" --description "Fixture product documentation" --read-only --json >/dev/null
  "$factile_bin" kb inspect /engineering --json >/dev/null
  "$factile_bin" kb view set /engineering reader --bundle /engineering/docs --title "Reader" --json >/dev/null
  "$factile_bin" list /engineering --json >/dev/null
  "$factile_bin" list /engineering --view reader --json >/dev/null
  "$factile_bin" list /engineering --brief --json >/dev/null
  "$factile_bin" stat /engineering/docs --json >/dev/null
  "$factile_bin" validate /engineering/docs --json >/dev/null
  "$factile_bin" kb view delete /engineering reader --json >/dev/null
  "$factile_bin" kb unlink /engineering/docs --json >/dev/null
)

"$factile_bin" --mount-file "$tmpdir/mounts.toml" bundle list --json >/dev/null
"$factile_bin" --mount-file "$tmpdir/mounts.toml" bundle inspect "$tmpdir/bundles/product-docs" --json >/dev/null

"$factile_bin" --mount-file "$tmpdir/mounts.toml" list / --json >/dev/null
"$factile_bin" --mount-file "$tmpdir/mounts.toml" list / --brief --json >/dev/null
"$factile_bin" --mount-file "$tmpdir/mounts.toml" list /product-docs --json >/dev/null
"$factile_bin" --mount-file "$tmpdir/mounts.toml" stat /product-docs --json >/dev/null
"$factile_bin" --mount-file "$tmpdir/mounts.toml" read /product-docs/workflows/invoice-import --json >/dev/null
"$factile_bin" --mount-file "$tmpdir/mounts.toml" read /product-docs/workflows/invoice-import.md --json >/dev/null
"$factile_bin" --mount-file "$tmpdir/mounts.toml" search /product-docs 'invoice' --json >/dev/null
"$factile_bin" --mount-file "$tmpdir/mounts.toml" context /product-docs 'invoice import workflow' --json >/dev/null
"$factile_bin" --mount-file "$tmpdir/mounts.toml" graph /product-docs/workflows/invoice-import --json >/dev/null
"$factile_bin" --mount-file "$tmpdir/mounts.toml" validate /product-docs --json >/dev/null

if "$factile_bin" --mount-file "$tmpdir/mounts.toml" validate /broken-docs --json >/dev/null; then
  echo 'expected validation failure for /broken-docs' >&2
  exit 1
fi

cat > "$tmpdir/new-workflow.md" <<'EOF3'
# Payment Import Workflow

Payment imports are loaded, validated, and reconciled.
EOF3

"$factile_bin" --mount-file "$tmpdir/mounts.toml" create /product-docs/workflows/payment-import --type Workflow --title 'Payment Import Workflow' --body "$tmpdir/new-workflow.md" --json >/dev/null
"$factile_bin" --mount-file "$tmpdir/mounts.toml" read /product-docs/workflows/payment-import --json >/dev/null
