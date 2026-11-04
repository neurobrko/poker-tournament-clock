#!/bin/sh
set -eu

ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
cd "$ROOT"

GOTOOLCHAIN="${GOTOOLCHAIN:-local}" go test -tags ci ./...
GOTOOLCHAIN="${GOTOOLCHAIN:-local}" go vet -tags ci ./...
mkdir -p dist

os="$(go env GOOS)"
arch="$(go env GOARCH)"
suffix=""
if [ "$os" = "windows" ]; then suffix=".exe"; fi
printf 'Building native Fyne desktop app for %s/%s\n' "$os" "$arch"
GOTOOLCHAIN="${GOTOOLCHAIN:-local}" go build -trimpath -o "dist/poker-clock-${os}-${arch}${suffix}" ./cmd/poker-clock
