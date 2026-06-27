#!/bin/sh
# Install the claude-benchmark CLI for the npx / agent-agnostic channel: download the
# checksum-verified prebuilt release into a PATH dir. (The Claude-plugin channel instead
# ships bin/claude-benchmark, which bootstraps the same way on first exec.)
#
# Exit 0 = installed (or already present). Exit 2 = unsupported platform (caller should
# fall back to `go install`). Exit 1 = transient failure (download / checksum).
set -eu

REPO="nikalosa/claude-god"
BIN="claude-benchmark"
VERSION="${CLAUDE_BENCHMARK_VERSION:-latest}"
DEST="${CLAUDE_BENCHMARK_BINDIR:-$HOME/.local/bin}"

if command -v "$BIN" >/dev/null 2>&1; then
  echo "already installed: $(command -v "$BIN")"; exit 0
fi

os=$(uname -s)
case "$os" in
  Darwin) os=darwin ;;
  Linux)  os=linux ;;
  *) echo "$BIN: unsupported OS '$os'" >&2; exit 2 ;;
esac
arch=$(uname -m)
case "$arch" in
  x86_64|amd64) arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) echo "$BIN: unsupported arch '$arch'" >&2; exit 2 ;;
esac

asset="${BIN}_${os}_${arch}"
if [ "$VERSION" = latest ]; then
  base="https://github.com/$REPO/releases/latest/download"
else
  base="https://github.com/$REPO/releases/download/$VERSION"
fi

echo "$BIN: downloading $asset ($VERSION)…" >&2
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

if ! curl -fsSL "$base/$asset" -o "$tmp/$BIN"; then
  echo "$BIN: download failed" >&2; exit 1
fi
if ! curl -fsSL "$base/checksums.txt" -o "$tmp/checksums.txt"; then
  echo "$BIN: checksum list download failed" >&2; exit 1
fi

want=$(grep " $asset\$" "$tmp/checksums.txt" | awk '{print $1}')
if [ -z "$want" ]; then
  echo "$BIN: $asset missing from checksums.txt" >&2; exit 1
fi
if command -v sha256sum >/dev/null 2>&1; then
  got=$(sha256sum "$tmp/$BIN" | awk '{print $1}')
else
  got=$(shasum -a 256 "$tmp/$BIN" | awk '{print $1}')
fi
if [ "$want" != "$got" ]; then
  echo "$BIN: checksum mismatch (want $want, got $got)" >&2; exit 1
fi

mkdir -p "$DEST"
chmod 0755 "$tmp/$BIN"
mv "$tmp/$BIN" "$DEST/$BIN"
rm -rf "$tmp"; trap - EXIT

echo "installed: $DEST/$BIN"
case ":$PATH:" in
  *":$DEST:"*) ;;
  *) echo "PATH: $DEST is not on PATH — add it with  export PATH=\"$DEST:\$PATH\"" >&2 ;;
esac
