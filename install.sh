#!/bin/sh
# Install dbx on macOS from the latest GitHub release.
#
#   curl -fsSL https://raw.githubusercontent.com/G2H/dbx/main/install.sh | sh
#
# Environment overrides:
#   DBX_VERSION      release tag to install, e.g. v0.1.0 (default: latest)
#   DBX_INSTALL_DIR  where the binary goes (default: ~/.local/bin)
#   DBX_BASE_URL     download location of the release assets (used by tests)
set -eu

repo="G2H/dbx"
version="${DBX_VERSION:-latest}"
install_dir="${DBX_INSTALL_DIR:-$HOME/.local/bin}"

fail() {
	echo "dbx install: $*" >&2
	exit 1
}

[ "$(uname -s)" = "Darwin" ] || fail "only macOS is supported by prebuilt releases; build from source with 'make build'"

case "$(uname -m)" in
arm64) arch="arm64" ;;
x86_64) arch="amd64" ;;
*) fail "unsupported architecture $(uname -m)" ;;
esac

if [ -n "${DBX_BASE_URL:-}" ]; then
	base_url="$DBX_BASE_URL"
elif [ "$version" = "latest" ]; then
	base_url="https://github.com/$repo/releases/latest/download"
else
	base_url="https://github.com/$repo/releases/download/$version"
fi

asset="dbx-darwin-$arch.tar.gz"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

echo "Downloading $asset ($version)..."
curl -fsSL "$base_url/$asset" -o "$tmp/$asset" || fail "download failed: $base_url/$asset"
curl -fsSL "$base_url/checksums.txt" -o "$tmp/checksums.txt" || fail "download failed: $base_url/checksums.txt"

expected="$(awk -v f="$asset" '$2 == f { print $1 }' "$tmp/checksums.txt")"
[ -n "$expected" ] || fail "no checksum listed for $asset"
actual="$(shasum -a 256 "$tmp/$asset" | awk '{ print $1 }')"
[ "$expected" = "$actual" ] || fail "checksum mismatch for $asset (expected $expected, got $actual)"

tar -xzf "$tmp/$asset" -C "$tmp"
mkdir -p "$install_dir"
install -m 0755 "$tmp/dbx" "$install_dir/dbx"

echo "Installed $("$install_dir/dbx" --version) to $install_dir/dbx"
case ":$PATH:" in
*":$install_dir:"*) ;;
*) echo "Note: $install_dir is not on your PATH. Add this to your shell profile:"
   echo "  export PATH=\"$install_dir:\$PATH\"" ;;
esac
