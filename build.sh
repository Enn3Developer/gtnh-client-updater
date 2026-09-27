#!/usr/bin/env bash
# Cross-compile static gtnh-update binaries into dist/, plus dist/checksums.txt (the
# file the self-updater verifies downloads against). CI's release job runs this too.
# Usage: ./build.sh [version]   (default: `git describe`, leading "v" stripped; else "dev")
set -euo pipefail
cd "$(dirname "$0")"

ver="${1:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}"
ver="${ver#v}"
targets=(linux/amd64 linux/arm64 windows/amd64 windows/arm64 darwin/amd64 darwin/arm64)

rm -rf dist
mkdir -p dist
for t in "${targets[@]}"; do
    os=${t%/*} arch=${t#*/}
    out="dist/gtnh-update-$os-$arch"
    [[ $os == windows ]] && out+=".exe"
    CGO_ENABLED=0 GOOS=$os GOARCH=$arch \
        go build -trimpath -ldflags "-s -w -X main.version=$ver" -o "$out" ./cmd/gtnh-update
    echo "built $out"
done
(cd dist && sha256sum gtnh-update-* > checksums.txt)
echo "wrote dist/checksums.txt (version $ver)"
