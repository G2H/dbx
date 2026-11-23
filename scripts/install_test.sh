#!/bin/sh
# Tests for install.sh against a fake local release served over file://.
# A stub "dbx" shell script stands in for the real binary.
set -eu

if [ "$(uname -s)" != "Darwin" ]; then
	echo "install_test: skipped (macOS only)"
	exit 0
fi

root="$(cd "$(dirname "$0")/.." && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

case "$(uname -m)" in
arm64) arch="arm64" ;;
*) arch="amd64" ;;
esac

# Build the fake release: a tarball holding the stub plus checksums.txt.
mkdir -p "$work/stub" "$work/release"
printf '#!/bin/sh\necho "dbx 9.9.9"\n' > "$work/stub/dbx"
chmod +x "$work/stub/dbx"
tar -C "$work/stub" -czf "$work/release/dbx-darwin-$arch.tar.gz" dbx
(cd "$work/release" && shasum -a 256 *.tar.gz > checksums.txt)

# Happy path: installs into the requested directory and reports the version.
out="$(DBX_BASE_URL="file://$work/release" DBX_INSTALL_DIR="$work/bin" sh "$root/install.sh")"
[ -x "$work/bin/dbx" ] || { echo "FAIL: binary not installed"; exit 1; }
[ "$("$work/bin/dbx")" = "dbx 9.9.9" ] || { echo "FAIL: installed binary is wrong"; exit 1; }
echo "$out" | grep -q "Installed dbx 9.9.9" || { echo "FAIL: missing success line: $out"; exit 1; }
echo "$out" | grep -q "not on your PATH" || { echo "FAIL: missing PATH hint: $out"; exit 1; }

# Tampered asset: a checksum mismatch must abort without installing.
echo "0000000000000000000000000000000000000000000000000000000000000000  dbx-darwin-$arch.tar.gz" > "$work/release/checksums.txt"
if DBX_BASE_URL="file://$work/release" DBX_INSTALL_DIR="$work/bin2" sh "$root/install.sh" 2> "$work/err"; then
	echo "FAIL: install succeeded despite checksum mismatch"; exit 1
fi
grep -q "checksum mismatch" "$work/err" || { echo "FAIL: wrong error: $(cat "$work/err")"; exit 1; }
[ ! -e "$work/bin2/dbx" ] || { echo "FAIL: binary installed despite mismatch"; exit 1; }

# Missing asset: a clean download error.
rm "$work/release/dbx-darwin-$arch.tar.gz"
if DBX_BASE_URL="file://$work/release" DBX_INSTALL_DIR="$work/bin3" sh "$root/install.sh" 2> "$work/err"; then
	echo "FAIL: install succeeded without an asset"; exit 1
fi
grep -q "download failed" "$work/err" || { echo "FAIL: wrong error: $(cat "$work/err")"; exit 1; }

echo "install_test: ok"
