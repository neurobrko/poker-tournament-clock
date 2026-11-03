#!/bin/sh
set -eu

ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
cd "$ROOT"

GOTOOLCHAIN="${GOTOOLCHAIN:-local}" go test ./...
go vet ./...
mkdir -p dist

build() {
  os="$1"
  arch="$2"
  suffix=""
  if [ "$os" = "windows" ]; then suffix=".exe"; fi
  printf 'Building %s/%s\n' "$os" "$arch"
  GOOS="$os" GOARCH="$arch" GOTOOLCHAIN="${GOTOOLCHAIN:-local}" go build -trimpath -o "dist/poker-clock-${os}-${arch}${suffix}" ./cmd/poker-clock
}

build linux amd64
build linux arm64
build darwin amd64
build darwin arm64
build windows amd64
