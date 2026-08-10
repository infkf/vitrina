#!/bin/sh
set -eu

REPO="infkf/vitrina"
BIN="vitrina"

echo "vitrina: installing latest release..."

OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)

case "$ARCH" in
  x86_64)  ARCH="amd64" ;;
  aarch64) ARCH="arm64" ;;
  arm64)   ;;
  *)       echo "unsupported architecture: $ARCH"; exit 1 ;;
esac

case "$OS" in
  linux)   ;;
  darwin)  ;;
  *)       echo "unsupported OS: $OS"; exit 1 ;;
esac

LATEST_URL=$(curl -fsSL "https://github.com/${REPO}/releases/latest" | grep -o "https://github.com/${REPO}/releases/download/[^\"]*" | head -1)
VERSION=$(echo "$LATEST_URL" | grep -o 'v[0-9.]*' | head -1)

DOWNLOAD_URL="https://github.com/${REPO}/releases/download/${VERSION}/vitrina_${VERSION}_${OS}_${ARCH}.tar.gz"

TMPDIR=$(mktemp -d)
trap 'rm -rf "$TMPDIR"' EXIT

curl -fsSL "$DOWNLOAD_URL" -o "$TMPDIR/vitrina.tar.gz"
tar -xzf "$TMPDIR/vitrina.tar.gz" -C "$TMPDIR"

DEST="/usr/local/bin"
if [ "$(id -u)" -ne 0 ]; then
  echo "Installing to /usr/local/bin requires sudo:"
  sudo install -m 0755 "$TMPDIR/vitrina" "$DEST/vitrina"
else
  install -m 0755 "$TMPDIR/vitrina" "$DEST/vitrina"
fi

echo "vitrina $VERSION installed to /usr/local/bin/vitrina"
echo "Next: vitrina remote set <name> --host <ip>  (configure your VPS)"
echo "      vitrina bootstrap <name> --domain <domain> --email <email>"
