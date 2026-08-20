#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BACKEND="$ROOT/backend"
OUT="$ROOT/apps/desktop/src-tauri/binaries"
GOCACHE="${GOCACHE:-/tmp/nextx-go-build-cache}"
mkdir -p "$OUT"

build() {
  local goos="$1"
  local goarch="$2"
  local suffix="$3"
  local ext=""
  local ldflags=""
  if [[ "$goos" == "windows" ]]; then
    ext=".exe"
    ldflags="-H=windowsgui"
  fi
  (
    cd "$BACKEND"
    GOCACHE="$GOCACHE" GOOS="$goos" GOARCH="$goarch" CGO_ENABLED=0 \
      go build -ldflags "$ldflags" -o "$OUT/nextx-agent-${suffix}${ext}" ./cmd/nextx-agent
  )
}

build darwin amd64 x86_64-apple-darwin
build darwin arm64 aarch64-apple-darwin
build linux amd64 x86_64-unknown-linux-gnu
build windows amd64 x86_64-pc-windows-msvc

echo "sidecars written to $OUT"
