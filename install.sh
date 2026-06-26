#!/usr/bin/env sh
set -eu

repo="factile/factile"
version="${FACTILE_VERSION:-latest}"
install_dir="${FACTILE_INSTALL_DIR:-}"

die() {
  echo "factile install: $*" >&2
  exit 1
}

need() {
  command -v "$1" >/dev/null 2>&1 || die "missing required command: $1"
}

need curl
need grep
need sed
need uname

os="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "$os" in
  linux|darwin) ;;
  *) die "unsupported OS: $os" ;;
esac

arch="$(uname -m)"
case "$arch" in
  x86_64|amd64) arch="amd64" ;;
  arm64|aarch64) arch="arm64" ;;
  *) die "unsupported architecture: $arch" ;;
esac

if [ -z "$install_dir" ]; then
  if [ -d /usr/local/bin ] && [ -w /usr/local/bin ]; then
    install_dir="/usr/local/bin"
  else
    install_dir="${HOME:-}/.local/bin"
  fi
fi
[ -n "$install_dir" ] || die "set FACTILE_INSTALL_DIR or HOME"

tmpdir="$(mktemp -d)"
cleanup() {
  rm -rf "$tmpdir"
}
trap cleanup EXIT INT TERM

release_api="${FACTILE_RELEASE_API:-}"
if [ -n "$release_api" ]; then
  case "$release_api" in
    file://*) ;;
    *) die "FACTILE_RELEASE_API is only supported for local file:// installer tests" ;;
  esac
else
  if [ "$version" = "latest" ]; then
    release_api="https://api.github.com/repos/$repo/releases/latest"
  else
    release_api="https://api.github.com/repos/$repo/releases/tags/$version"
  fi
fi

release_json="$tmpdir/release.json"
curl -fsSL "$release_api" -o "$release_json"

tag="$(sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' "$release_json" | head -n 1)"
[ -n "$tag" ] || die "could not read release tag from GitHub response"

download_urls() {
  sed 's/"browser_download_url"/\
"browser_download_url"/g' "$release_json" |
    sed -n 's/.*"browser_download_url": *"\([^"]*\)".*/\1/p'
}

asset_url="$(
  download_urls |
    grep "_${os}_${arch}" |
    grep -E '\.(tar\.gz|zip)$' |
    head -n 1
)"
[ -n "$asset_url" ] || die "no release archive found for ${os}_${arch} in $tag"

checksum_url="$(
  download_urls |
    grep '/checksums.txt$' |
    head -n 1
)"

archive_name="${asset_url##*/}"
archive="$tmpdir/$archive_name"
curl -fsSL "$asset_url" -o "$archive"

if [ -n "$checksum_url" ]; then
  checksums="$tmpdir/checksums.txt"
  curl -fsSL "$checksum_url" -o "$checksums"
  checksum_line="$tmpdir/checksum-line.txt"
  grep "  $archive_name\$" "$checksums" > "$checksum_line" || die "checksums.txt does not include $archive_name"
  if command -v sha256sum >/dev/null 2>&1; then
    (cd "$tmpdir" && sha256sum -c "$checksum_line")
  elif command -v shasum >/dev/null 2>&1; then
    (cd "$tmpdir" && shasum -a 256 -c "$checksum_line")
  else
    die "checksums.txt is available, but sha256sum or shasum is missing"
  fi
fi

case "$archive_name" in
  *.tar.gz)
    need tar
    tar -xzf "$archive" -C "$tmpdir"
    ;;
  *.zip)
    need unzip
    unzip -q "$archive" -d "$tmpdir"
    ;;
  *)
    die "unsupported archive format: $archive_name"
    ;;
esac

binary="$(find "$tmpdir" -type f -name factile | head -n 1)"
[ -n "$binary" ] || die "archive did not contain factile binary"

mkdir -p "$install_dir"
cp "$binary" "$install_dir/factile"
chmod 0755 "$install_dir/factile"

"$install_dir/factile" version
echo "installed $tag to $install_dir/factile"
