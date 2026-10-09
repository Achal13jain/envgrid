#!/bin/sh
# Installs the latest envgrid release for Linux or macOS.
#   curl -fsSL https://raw.githubusercontent.com/Achal13jain/envgrid/main/install.sh | sh
# Set ENVGRID_INSTALL_DIR to choose where the program goes.
set -eu

repo="Achal13jain/envgrid"
case "$(uname -s)" in
  Linux) os=linux ;;
  Darwin) os=darwin ;;
  *) echo "envgrid: unsupported system $(uname -s); see https://github.com/$repo/releases" >&2; exit 1 ;;
esac
case "$(uname -m)" in
  x86_64 | amd64) arch=amd64 ;;
  arm64 | aarch64) arch=arm64 ;;
  *) echo "envgrid: unsupported processor $(uname -m)" >&2; exit 1 ;;
esac

name="envgrid_${os}_${arch}"
base="https://github.com/$repo/releases/latest/download"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

echo "Downloading $name..."
curl -fsSL "$base/$name.tar.gz" -o "$tmp/$name.tar.gz"
curl -fsSL "$base/checksums.txt" -o "$tmp/checksums.txt"
expected="$(grep " ./$name.tar.gz\$" "$tmp/checksums.txt" | cut -d ' ' -f 1)"
if command -v sha256sum >/dev/null 2>&1; then
  actual="$(sha256sum "$tmp/$name.tar.gz" | cut -d ' ' -f 1)"
else
  actual="$(shasum -a 256 "$tmp/$name.tar.gz" | cut -d ' ' -f 1)"
fi
if [ -z "$expected" ] || [ "$expected" != "$actual" ]; then
  echo "envgrid: checksum mismatch, not installing" >&2
  exit 1
fi
tar -xzf "$tmp/$name.tar.gz" -C "$tmp"

dir="${ENVGRID_INSTALL_DIR:-/usr/local/bin}"
if [ -w "$dir" ]; then
  install -m 0755 "$tmp/$name/envgrid" "$dir/envgrid"
elif [ -z "${ENVGRID_INSTALL_DIR:-}" ] && command -v sudo >/dev/null 2>&1; then
  sudo install -m 0755 "$tmp/$name/envgrid" "$dir/envgrid"
else
  echo "envgrid: cannot write to $dir; set ENVGRID_INSTALL_DIR to a folder you own" >&2
  exit 1
fi
echo "Installed $("$dir/envgrid" version) to $dir/envgrid"
echo "Next: run 'envgrid genkey', then see https://github.com/$repo#quick-start"
