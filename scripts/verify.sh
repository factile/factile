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
verify_codex_home="$tmpdir/codex-home"
mkdir -p "$verify_codex_home"
export CODEX_HOME="$verify_codex_home"

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

require_contains() {
  local file="$1"
  local text="$2"
  if ! grep -F -- "$text" "$file" >/dev/null; then
    echo "expected $file to contain: $text" >&2
    exit 1
  fi
}

require_no_change_actions() {
  local file="$1"
  if grep -E -- '"action": "(created|updated|removed)"' "$file" >/dev/null; then
    echo "expected repeated init to reuse every managed file: $file" >&2
    exit 1
  fi
}

wait_for_text() {
  local file="$1"
  local text="$2"
  local attempt
  for attempt in {1..200}; do
    if [ -f "$file" ] && grep -F -- "$text" "$file" >/dev/null; then
      return 0
    fi
    sleep 0.05
  done
  return 1
}

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
  "$factile_bin" init --root knowledge --name curator-guide --title "Curator Guide" --description "Curated project guidance." --agent none --yes --json >/dev/null
  PATH="$tmpdir:$PATH" "$factile_bin" skill install codex --scope repo --mode curator --profile software --json >/dev/null
  sed -i 's/# Factile local knowledge workflow/# Stale Factile workflow/' .agents/skills/factile/SKILL.md
  "$factile_bin" init --json > "$tmpdir/curator-reconcile.json"
  PATH="$tmpdir:$PATH" "$factile_bin" skill doctor codex --json >/dev/null
)
require_contains "$tmpdir/curator-reconcile.json" '"root_bundle_path": "knowledge"'
require_contains "$tmpdir/curator-reconcile.json" '"mode": "curator"'
require_contains "$tmpdir/curator-reconcile.json" '"profile": "software"'
require_contains "$tmpdir/curator-reconcile.json" '"action": "updated"'
require_contains "$tmpdir/curator-reconcile.json" '"ok": true'
if grep -F -- '"--read-only"' "$curator_skill_workspace/.codex/config.toml" >/dev/null; then
  echo 'curator init reconciliation downgraded MCP to reader mode' >&2
  exit 1
fi

init_workspace="$tmpdir/init-workspace"
mkdir -p "$init_workspace/.codex"
(
  cd "$init_workspace"
  PATH="$tmpdir:$PATH" "$factile_bin" init --json > "$tmpdir/init-first.json"
  printf '\nAuthored verification content.\n' >> docs/overview.md
  cp docs/overview.md "$tmpdir/init-overview.before"
  PATH="$tmpdir:$PATH" "$factile_bin" init --json > "$tmpdir/init-repeated.json"
  cmp docs/overview.md "$tmpdir/init-overview.before"
  "$factile_bin" list / --json >/dev/null
  "$factile_bin" list / --brief --json >/dev/null
  "$factile_bin" stat /overview --json >/dev/null
  "$factile_bin" read /overview --json >/dev/null
)
require_contains "$tmpdir/init-first.json" '"root_bundle_path": "docs"'
require_contains "$tmpdir/init-first.json" '"mode": "reader"'
require_contains "$tmpdir/init-first.json" '"ok": true'
require_contains "$tmpdir/init-repeated.json" '"action": "unchanged"'
require_contains "$tmpdir/init-repeated.json" '"ok": true'
require_no_change_actions "$tmpdir/init-repeated.json"

descriptor_workspace="$tmpdir/descriptor-workspace"
mkdir -p "$descriptor_workspace"
(
  cd "$descriptor_workspace"
  "$factile_bin" init --root . --agent none --yes --json > "$tmpdir/combined-first.json"
  "$factile_bin" init --agent none --json > "$tmpdir/combined-repeated.json"
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
require_contains "$tmpdir/combined-first.json" '"root_bundle_path": "."'
require_contains "$tmpdir/combined-first.json" '"ok": true'
require_contains "$tmpdir/combined-repeated.json" '"action": "unchanged"'
require_no_change_actions "$tmpdir/combined-repeated.json"

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

boundary_workspace="$tmpdir/boundary-workspace"
mkdir -p "$boundary_workspace/src/nested"
(
  cd "$boundary_workspace"
  "$factile_bin" init --agent none --yes --json >/dev/null
  cd src/nested
  "$factile_bin" init --agent none --yes --json > "$tmpdir/nested-reconcile.json"
  test ! -e factile.toml
  test ! -e .gitignore
  "$factile_bin" --workspace . init --agent none --yes --json > "$tmpdir/exact-workspace.json"
  test -f factile.toml
  test -f docs/factile.toml
)
require_contains "$tmpdir/nested-reconcile.json" '"workspace_path": "../.."'
require_contains "$tmpdir/nested-reconcile.json" '"root_bundle_path": "docs"'
require_contains "$tmpdir/exact-workspace.json" '"workspace_path": "."'
require_contains "$tmpdir/exact-workspace.json" '"root_bundle_path": "docs"'
require_contains "$boundary_workspace/factile.toml" 'root = "docs"'

root_change_workspace="$tmpdir/root-change-workspace"
mkdir -p "$root_change_workspace"
(
  cd "$root_change_workspace"
  "$factile_bin" init --agent none --yes --json >/dev/null
  printf '\nPreserve this old root.\n' >> docs/overview.md
  cp docs/overview.md "$tmpdir/old-root-overview.before"
  "$factile_bin" init --root knowledge --agent none --yes --json > "$tmpdir/root-change.json"
  cmp docs/overview.md "$tmpdir/old-root-overview.before"
  test -f docs/factile.toml
  test -f knowledge/factile.toml
  "$factile_bin" init --agent none --json > "$tmpdir/root-change-repeated.json"
)
require_contains "$tmpdir/root-change.json" '"root_bundle_path": "knowledge"'
require_contains "$tmpdir/root-change.json" '"action": "updated"'
require_contains "$root_change_workspace/factile.toml" 'root = "knowledge"'
require_no_change_actions "$tmpdir/root-change-repeated.json"

nested_root_workspace="$tmpdir/nested-root-workspace"
mkdir -p "$nested_root_workspace"
(
  cd "$nested_root_workspace"
  "$factile_bin" init --root knowledge/project --agent none --yes --json >/dev/null
  cd knowledge/project
  "$factile_bin" status --json > "$tmpdir/nested-root-status.json"
  "$factile_bin" list / --json >/dev/null
)
require_contains "$tmpdir/nested-root-status.json" '"workspace_dir": "'"$nested_root_workspace"'"'
require_contains "$tmpdir/nested-root-status.json" '"root_bundle_dir": "'"$nested_root_workspace"'/knowledge/project"'

interrupted_workspace="$tmpdir/interrupted-workspace"
mkdir -p "$interrupted_workspace/docs"
printf '/.factile/\n' > "$interrupted_workspace/.gitignore"
printf 'version = 2\n\n[workspace]\nroot = "docs"\n' > "$interrupted_workspace/factile.toml"
printf 'partial destination bytes\n' > "$interrupted_workspace/docs/.factile.toml.factile-tmp-interrupted"
(
  cd "$interrupted_workspace"
  "$factile_bin" init --agent none --yes --json > "$tmpdir/interrupted-recovered.json"
  "$factile_bin" read /overview --json >/dev/null
  "$factile_bin" init --agent none --json > "$tmpdir/interrupted-repeated.json"
)
if grep -F -- 'partial destination bytes' "$interrupted_workspace/docs/factile.toml" >/dev/null; then
  echo 'interrupted staging bytes were exposed as the destination' >&2
  exit 1
fi
require_contains "$interrupted_workspace/docs/factile.toml" '[bundle]'
require_contains "$tmpdir/interrupted-recovered.json" '"action": "created"'
require_no_change_actions "$tmpdir/interrupted-repeated.json"

collision_workspace="$tmpdir/collision-workspace"
mkdir -p "$collision_workspace"
(
  cd "$collision_workspace"
  "$factile_bin" init --agent none --yes --json >/dev/null
  mkdir -p .agents/skills/factile
  printf 'hand-authored Factile skill\n' > .agents/skills/factile/SKILL.md
  cp .agents/skills/factile/SKILL.md "$tmpdir/collision-skill.before"
  cp docs/factile.toml "$tmpdir/collision-bundle.before"
  if "$factile_bin" init --title 'Must Not Apply' --agent codex --yes --json >/dev/null 2>&1; then
    echo 'init accepted an unrecognized canonical skill collision' >&2
    exit 1
  fi
  if "$factile_bin" skill install codex --scope repo --json >/dev/null 2>&1; then
    echo 'skill install accepted an unrecognized canonical skill collision' >&2
    exit 1
  fi
  if "$factile_bin" skill uninstall codex --scope repo --json >/dev/null 2>&1; then
    echo 'skill uninstall accepted an unrecognized canonical skill collision' >&2
    exit 1
  fi
  cmp .agents/skills/factile/SKILL.md "$tmpdir/collision-skill.before"
  cmp docs/factile.toml "$tmpdir/collision-bundle.before"
  test ! -e AGENTS.md
  test ! -e .codex/config.toml
)

duplicate_workspace="$tmpdir/duplicate-markers-workspace"
mkdir -p "$duplicate_workspace"
(
  cd "$duplicate_workspace"
  "$factile_bin" init --agent codex --yes --json >/dev/null
  cp AGENTS.md "$tmpdir/duplicate-agents.block"
  cp .codex/config.toml "$tmpdir/duplicate-mcp.block"
  printf '\nauthored between managed AGENTS blocks\n' >> AGENTS.md
  cat "$tmpdir/duplicate-agents.block" >> AGENTS.md
  printf '\n# authored between managed MCP blocks\n' >> .codex/config.toml
  cat "$tmpdir/duplicate-mcp.block" >> .codex/config.toml
  if PATH="$tmpdir:$PATH" "$factile_bin" skill doctor codex --json > "$tmpdir/duplicate-doctor-before.json"; then
    echo 'skill doctor accepted duplicate managed blocks' >&2
    exit 1
  fi
  PATH="$tmpdir:$PATH" "$factile_bin" skill install codex --scope repo --json >/dev/null
  test "$(grep -c '^<!-- factile:codex:start -->$' AGENTS.md)" -eq 1
  test "$(grep -c '^# factile:codex-mcp:start$' .codex/config.toml)" -eq 1
  grep -F -- 'authored between managed AGENTS blocks' AGENTS.md >/dev/null
  grep -F -- 'authored between managed MCP blocks' .codex/config.toml >/dev/null
  PATH="$tmpdir:$PATH" "$factile_bin" skill doctor codex --json > "$tmpdir/duplicate-doctor-after.json"
)
require_contains "$tmpdir/duplicate-doctor-before.json" '"ok": false'
require_contains "$tmpdir/duplicate-doctor-after.json" '"ok": true'

malformed_markers_workspace="$tmpdir/malformed-markers-workspace"
mkdir -p "$malformed_markers_workspace"
(
  cd "$malformed_markers_workspace"
  "$factile_bin" init --agent codex --yes --json >/dev/null
  printf '<!-- factile:codex:start -->\nmalformed managed guidance\n' > AGENTS.md
  cp AGENTS.md "$tmpdir/malformed-agents.before"
  cp docs/factile.toml "$tmpdir/malformed-bundle.before"
  if PATH="$tmpdir:$PATH" "$factile_bin" skill doctor codex --json > "$tmpdir/malformed-doctor.json"; then
    echo 'skill doctor accepted malformed managed markers' >&2
    exit 1
  fi
  if "$factile_bin" skill install codex --scope repo --json >/dev/null 2>&1; then
    echo 'skill install accepted malformed managed markers' >&2
    exit 1
  fi
  if "$factile_bin" skill uninstall codex --scope repo --json >/dev/null 2>&1; then
    echo 'skill uninstall accepted malformed managed markers' >&2
    exit 1
  fi
  if "$factile_bin" init --title 'Must Not Apply' --agent codex --yes --json >/dev/null 2>&1; then
    echo 'init accepted malformed managed markers' >&2
    exit 1
  fi
  cmp AGENTS.md "$tmpdir/malformed-agents.before"
  cmp docs/factile.toml "$tmpdir/malformed-bundle.before"
)
require_contains "$tmpdir/malformed-doctor.json" '"ok": false'

user_scope_home="$tmpdir/user-symlink-codex-home"
user_scope_outside="$tmpdir/user-symlink-outside"
mkdir -p "$user_scope_home" "$user_scope_outside/factile"
printf 'outside user skill sentinel\n' > "$user_scope_outside/factile/SKILL.md"
cp "$user_scope_outside/factile/SKILL.md" "$tmpdir/user-symlink.before"
ln -s "$user_scope_outside" "$user_scope_home/skills"
if CODEX_HOME="$user_scope_home" "$factile_bin" skill install codex --scope user --json >/dev/null 2>&1; then
  echo 'user skill install followed a managed-path symlink' >&2
  exit 1
fi
if CODEX_HOME="$user_scope_home" "$factile_bin" skill uninstall codex --scope user --json >/dev/null 2>&1; then
  echo 'user skill uninstall followed a managed-path symlink' >&2
  exit 1
fi
cmp "$user_scope_outside/factile/SKILL.md" "$tmpdir/user-symlink.before"
test ! -e "$user_scope_outside/factile/scripts/factile-discover.sh"

for missing_option in workspace root name title description agent; do
  missing_workspace="$tmpdir/missing-$missing_option-workspace"
  mkdir -p "$missing_workspace"
  if (cd "$missing_workspace" && "$factile_bin" init "--$missing_option" --yes --json > "$tmpdir/missing-$missing_option.stdout" 2> "$tmpdir/missing-$missing_option.stderr"); then
    echo "init accepted a missing --$missing_option value" >&2
    exit 1
  fi
  test ! -s "$tmpdir/missing-$missing_option.stdout"
  require_contains "$tmpdir/missing-$missing_option.stderr" '"error"'
  if find "$missing_workspace" -mindepth 1 -print -quit | grep -q .; then
    echo "missing --$missing_option value mutated its workspace" >&2
    exit 1
  fi
done

devnull_workspace="$tmpdir/devnull-workspace"
mkdir -p "$devnull_workspace"
(
  cd "$devnull_workspace"
  "$factile_bin" init --agent none --color never </dev/null >/dev/null
)
test -f "$devnull_workspace/factile.toml"
test -f "$devnull_workspace/docs/factile.toml"

handoff_caller="$tmpdir/handoff-caller"
handoff_target="$tmpdir/Target Workspace"
mkdir -p "$handoff_caller" "$handoff_target"
(
  cd "$handoff_caller"
  "$factile_bin" init --title 'Caller Guide' --agent none --yes --json >/dev/null
  PATH="$tmpdir:$PATH" "$factile_bin" --workspace "$handoff_target" init --title 'Target Guide' --agent none --yes --color never > "$tmpdir/handoff.txt"
)
test "$(grep -Fc "factile --workspace '../Target Workspace'" "$tmpdir/handoff.txt")" -eq 3
sed -n 's/^  \(factile --workspace .*\)$/\1/p' "$tmpdir/handoff.txt" > "$tmpdir/handoff.commands"
test "$(wc -l < "$tmpdir/handoff.commands")" -eq 3
mv "$handoff_caller/docs/factile.toml" "$handoff_caller/docs/factile.toml.disabled"
while IFS= read -r next_command; do
  (
    cd "$handoff_caller"
    PATH="$tmpdir:$PATH" /bin/sh -c "$next_command" >/dev/null
  )
done < "$tmpdir/handoff.commands"

if ! command -v script >/dev/null; then
  echo 'source-built interactive init verification requires util-linux script' >&2
  exit 1
fi

interactive_workspace="$tmpdir/interactive-workspace"
mkdir -p "$interactive_workspace"
printf '../outside\nknowledge\nInteractive Guide\nInteractive project knowledge.\n\n' |
  script -qec "cd \"$interactive_workspace\" && \"$factile_bin\" init --agent none --color never" /dev/null > "$tmpdir/interactive-init.txt"
require_contains "$tmpdir/interactive-init.txt" 'Invalid value: Root bundle must be . or a normalized relative directory inside the workspace.'
require_contains "$tmpdir/interactive-init.txt" 'Initialized Factile workspace'
require_contains "$interactive_workspace/factile.toml" 'root = "knowledge"'
require_contains "$interactive_workspace/knowledge/factile.toml" 'title = "Interactive Guide"'
require_contains "$interactive_workspace/knowledge/factile.toml" 'description = "Interactive project knowledge."'

declined_workspace="$tmpdir/declined-workspace"
mkdir -p "$declined_workspace"
printf '\n\n\nno\n' |
  script -qec "cd \"$declined_workspace\" && \"$factile_bin\" init --agent none --color never" /dev/null > "$tmpdir/declined-init.txt"
require_contains "$tmpdir/declined-init.txt" 'Initialization cancelled; no changes made.'
test ! -e "$declined_workspace/factile.toml"
test ! -e "$declined_workspace/.gitignore"
test ! -e "$declined_workspace/docs"

path_swap_workspace="$tmpdir/path-swap-workspace"
path_swap_outside="$tmpdir/path-swap-outside"
mkdir -p "$path_swap_workspace" "$path_swap_outside"
(
  cd "$path_swap_workspace"
  "$factile_bin" init --agent none --yes --json >/dev/null
)
printf 'outside sentinel\n' > "$path_swap_outside/sentinel"
cp "$path_swap_workspace/factile.toml" "$tmpdir/path-swap-workspace.before"
cp "$path_swap_outside/sentinel" "$tmpdir/path-swap-sentinel.before"
path_swap_fifo="$tmpdir/path-swap.fifo"
mkfifo "$path_swap_fifo"
exec 3<> "$path_swap_fifo"
script -q -f -e -c "cd \"$path_swap_workspace\" && \"$factile_bin\" init --root knowledge/docs --agent none --color never" /dev/null < "$path_swap_fifo" > "$tmpdir/path-swap.txt" 2>&1 &
path_swap_pid=$!
if ! wait_for_text "$tmpdir/path-swap.txt" 'Change the workspace root bundle? [y/N]'; then
  kill "$path_swap_pid" 2>/dev/null || true
  wait "$path_swap_pid" 2>/dev/null || true
  exec 3>&-
  echo 'timed out waiting for root-change confirmation' >&2
  exit 1
fi
ln -s "$path_swap_outside" "$path_swap_workspace/knowledge"
printf 'yes\n' >&3
exec 3>&-
if wait "$path_swap_pid"; then
  echo 'init accepted a prompt-time root path substitution' >&2
  exit 1
fi
cmp "$path_swap_workspace/factile.toml" "$tmpdir/path-swap-workspace.before"
cmp "$path_swap_outside/sentinel" "$tmpdir/path-swap-sentinel.before"
test ! -e "$path_swap_outside/docs"
require_contains "$tmpdir/path-swap.txt" 'Root bundle path must contain only real directories.'

malformed_workspace="$tmpdir/malformed-workspace"
mkdir -p "$malformed_workspace"
printf 'not toml = [\n' > "$malformed_workspace/factile.toml"
cp "$malformed_workspace/factile.toml" "$tmpdir/malformed.before"
if (cd "$malformed_workspace" && "$factile_bin" init --json >/dev/null 2>&1); then
  echo 'expected malformed init plan to fail' >&2
  exit 1
fi
cmp "$malformed_workspace/factile.toml" "$tmpdir/malformed.before"
test ! -e "$malformed_workspace/.gitignore"
test ! -e "$malformed_workspace/docs"

invalid_workspace="$tmpdir/invalid-workspace"
mkdir -p "$invalid_workspace"
if (cd "$invalid_workspace" && "$factile_bin" init --root ../outside --json >/dev/null 2>&1); then
  echo 'expected escaping init root to fail' >&2
  exit 1
fi
test ! -e "$invalid_workspace/factile.toml"
test ! -e "$invalid_workspace/.gitignore"

retired_init_option="--he""re"
if grep -R -F -- "$retired_init_option" cmd internal pkg scripts testdata >/dev/null; then
  echo 'retired init option remains in executable code, help, fixtures, or tests' >&2
  exit 1
fi
if "$factile_bin" init --help | grep -F -- "$retired_init_option" >/dev/null; then
  echo 'retired init option remains in source-built help' >&2
  exit 1
fi
if (cd "$invalid_workspace" && "$factile_bin" init "$retired_init_option" --json >/dev/null 2>&1); then
  echo 'retired init option was accepted' >&2
  exit 1
fi

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
