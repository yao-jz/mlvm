#!/bin/sh
# Install the latest MLVM release binary.
#
#   curl -fsSL https://raw.githubusercontent.com/yao-jz/mlvm/main/install.sh | sh
#
# Downloads the archive for this OS and CPU from GitHub Releases, checks it against the
# release's checksums.txt, and puts `mlvm` in $MLVM_INSTALL_DIR (default ~/.local/bin).
# Set MLVM_VERSION=v0.1.0 to pin a version. Windows: download the zip from the releases
# page. From source: git clone, then `make build` (Go 1.23+).
set -eu

REPO="yao-jz/mlvm"
DIR="${MLVM_INSTALL_DIR:-$HOME/.local/bin}"

fail() { echo "install.sh: $*" >&2; exit 1; }
need() { command -v "$1" >/dev/null 2>&1 || fail "needs $1"; }
need curl
need tar

case "$(uname -s)" in
  Linux) os=linux ;;
  Darwin) os=darwin ;;
  *) fail "unsupported OS $(uname -s); see https://github.com/$REPO/releases" ;;
esac
case "$(uname -m)" in
  x86_64 | amd64) arch=amd64 ;;
  arm64 | aarch64) arch=arm64 ;;
  *) fail "unsupported CPU $(uname -m)" ;;
esac

tag="${MLVM_VERSION:-}"
if [ -z "$tag" ]; then
  tag="$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -n 1)"
  [ -n "$tag" ] || fail "could not find the latest release"
fi
version="${tag#v}"
archive="mlvm_${version}_${os}_${arch}.tar.gz"
base="${MLVM_DOWNLOAD_BASE:-https://github.com/$REPO/releases/download/$tag}"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
echo "downloading $archive ($tag)"
curl -fsSL "$base/$archive" -o "$tmp/$archive"
curl -fsSL "$base/checksums.txt" -o "$tmp/checksums.txt"

want="$(grep " $archive\$" "$tmp/checksums.txt" | cut -d' ' -f1)"
[ -n "$want" ] || fail "$archive is not listed in checksums.txt"
if command -v sha256sum >/dev/null 2>&1; then
  got="$(sha256sum "$tmp/$archive" | cut -d' ' -f1)"
else
  got="$(shasum -a 256 "$tmp/$archive" | cut -d' ' -f1)"
fi
[ "$got" = "$want" ] || fail "checksum mismatch for $archive"

tar -xzf "$tmp/$archive" -C "$tmp" mlvm
mkdir -p "$DIR"
mv "$tmp/mlvm" "$DIR/mlvm"
chmod +x "$DIR/mlvm"
echo "installed $DIR/mlvm ($("$DIR/mlvm" --version 2>/dev/null || echo "$tag"))"
case ":$PATH:" in
  *":$DIR:"*) ;;
  *) echo "note: $DIR is not on your PATH" ;;
esac
echo "try: mlvm demo \"Once upon a time\""
