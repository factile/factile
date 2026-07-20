#!/usr/bin/env bash
set -euo pipefail

export GOCACHE="${GOCACHE:-/tmp/factile-go-build-cache}"
export GOMODCACHE="${GOMODCACHE:-/tmp/factile-go-mod-cache}"
mkdir -p "$GOCACHE" "$GOMODCACHE"

repo_root="$(pwd)"
version="$(tr -d '[:space:]' < "$repo_root/VERSION")"
if ! [[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "VERSION must contain SemVer X.Y.Z, got: $version" >&2
  exit 1
fi

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

"$factile_bin" --help >/dev/null
test "$("$factile_bin" version)" = "factile v$version"
"$factile_bin" --version >/dev/null
"$factile_bin" skill list --json >/dev/null
"$factile_bin" skill inspect codex --json >/dev/null

skill_workspace="$tmpdir/skill-workspace"
mkdir -p "$skill_workspace"
(
  cd "$skill_workspace"
  "$factile_bin" init --json >/dev/null
  PATH="$tmpdir:$PATH" "$factile_bin" skill install codex --scope repo --json >/dev/null
  PATH="$tmpdir:$PATH" "$factile_bin" skill doctor codex --json >/dev/null
)

if grep -R -E -- '\.factile/(config|views)\.toml|no_active_root|`--root' \
  "$skill_workspace/.agents/skills/factile" "$skill_workspace/AGENTS.md" >/dev/null; then
  echo 'generated Factile guidance contains legacy root-layout instructions' >&2
  exit 1
fi

curator_skill_workspace="$tmpdir/curator-skill-workspace"
mkdir -p "$curator_skill_workspace"
(
  cd "$curator_skill_workspace"
  "$factile_bin" init --json >/dev/null
  PATH="$tmpdir:$PATH" "$factile_bin" skill install codex --scope repo --mode curator --profile software --json >/dev/null
)

init_workspace="$tmpdir/init-workspace"
mkdir -p "$init_workspace/.codex"
(
  cd "$init_workspace"
  PATH="$tmpdir:$PATH" "$factile_bin" init --json >/dev/null
  "$factile_bin" list / --json >/dev/null
  "$factile_bin" list / --brief --json >/dev/null
  "$factile_bin" stat /overview --json >/dev/null
  "$factile_bin" read /overview --json >/dev/null
)

descriptor_workspace="$tmpdir/descriptor-workspace"
mkdir -p "$descriptor_workspace"
(
  cd "$descriptor_workspace"
  "$factile_bin" init --here --json >/dev/null
  "$factile_bin" mount "$tmpdir/bundles/product-docs" /engineering/docs --title "Product Docs" --description "Fixture product documentation" --read-only --json >/dev/null
  "$factile_bin" mount "$tmpdir/bundles/product-docs" /product-docs --writable --json >/dev/null
  "$factile_bin" mount "$tmpdir/bundles/broken-docs" /broken-docs --writable --json >/dev/null
  "$factile_bin" mounts --json >/dev/null
  "$factile_bin" list /engineering --json >/dev/null
  "$factile_bin" list /engineering --brief --json >/dev/null
  "$factile_bin" search /engineering 'invoice' --json >/dev/null
  "$factile_bin" context /engineering 'invoice import workflow' --json >/dev/null
  "$factile_bin" graph /engineering --json >/dev/null
  "$factile_bin" stat /engineering/docs --json >/dev/null
  "$factile_bin" validate /engineering/docs --json >/dev/null
  "$factile_bin" view set invoice --title "Invoice" --path /engineering/docs/workflows/invoice-import --json >/dev/null
  "$factile_bin" view list --json >/dev/null
  "$factile_bin" context /engineering 'invoice import workflow' --view invoice --json >/dev/null
  "$factile_bin" view delete invoice --json >/dev/null
  "$factile_bin" unmount /engineering/docs --json >/dev/null
)

"$factile_bin" bundle inspect "$tmpdir/bundles/product-docs" --json >/dev/null
"$factile_bin" bundle find "$tmpdir/bundles" --json >/dev/null

"$factile_bin" --workspace "$descriptor_workspace" list / --json >/dev/null
"$factile_bin" --workspace "$descriptor_workspace" list / --brief --json >/dev/null
"$factile_bin" --workspace "$descriptor_workspace" list /product-docs --json >/dev/null
"$factile_bin" --workspace "$descriptor_workspace" stat /product-docs --json >/dev/null
"$factile_bin" --workspace "$descriptor_workspace" read /product-docs/workflows/invoice-import --json >/dev/null
"$factile_bin" --workspace "$descriptor_workspace" read /product-docs/workflows/invoice-import.md --json >/dev/null
"$factile_bin" --workspace "$descriptor_workspace" search /product-docs 'invoice' --json >/dev/null
"$factile_bin" --workspace "$descriptor_workspace" context /product-docs 'invoice import workflow' --json >/dev/null
"$factile_bin" --workspace "$descriptor_workspace" graph /product-docs/workflows/invoice-import --json >/dev/null
"$factile_bin" --workspace "$descriptor_workspace" validate /product-docs --json >/dev/null

if "$factile_bin" --workspace "$descriptor_workspace" validate /broken-docs --json >/dev/null; then
  echo 'expected validation failure for /broken-docs' >&2
  exit 1
fi

cat > "$tmpdir/new-workflow.md" <<'EOF3'
# Payment Import Workflow

Payment imports are loaded, validated, and reconciled.
EOF3

"$factile_bin" --workspace "$descriptor_workspace" create /product-docs/workflows/payment-import --type Workflow --title 'Payment Import Workflow' --body "$tmpdir/new-workflow.md" --json >/dev/null
"$factile_bin" --workspace "$descriptor_workspace" read /product-docs/workflows/payment-import --json >/dev/null

npm_stage="$tmpdir/npm"
node packaging/npm/scripts/prepare-packages.mjs --build --out "$npm_stage" --version "$version" >/dev/null
node packaging/npm/scripts/smoke-test.mjs --packages-dir "$npm_stage" >/dev/null

docs_workspace="$tmpdir/docs-workspace"
mkdir -p "$docs_workspace"
cp -R "$repo_root/docs" "$docs_workspace/docs"
cp "$repo_root/factile.toml" "$docs_workspace/factile.toml"
rm -f "$docs_workspace/docs/coding.mount.toml"
(
  cd "$docs_workspace"
  "$factile_bin" validate / --json >/dev/null
)
