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
version="${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || printf dev)}"
printf 'Building native Fyne desktop app for %s/%s (%s)\n' "$os" "$arch" "$version"
GOTOOLCHAIN="${GOTOOLCHAIN:-local}" go build -trimpath -ldflags="-X=git.mpx.sk/elvis/poker_clock/internal/ui.BuildVersion=${version}" -o "dist/poker-clock-${os}-${arch}${suffix}" ./cmd/poker-clock
