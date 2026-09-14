#!/usr/bin/env sh
# Cross-compile gibbon for every supported platform into dist/.
# Usage: scripts/build-release.sh [VERSION]   (default: git describe, or "dev")
set -eu

version="${1:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}"
ldflags="-s -w -X github.com/kumibrr/gibbon/internal/cli.version=${version}"
targets="windows/amd64 windows/arm64 linux/amd64 linux/arm64 darwin/amd64 darwin/arm64"

rm -rf dist && mkdir -p dist
for target in $targets; do
    os="${target%/*}"
    arch="${target#*/}"
    name="gibbon_${version}_${os}_${arch}"
    bin="gibbon"
    [ "$os" = windows ] && bin="gibbon.exe"
    stage="dist/${name}"
    mkdir -p "$stage"
    CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build -trimpath -ldflags "$ldflags" -o "${stage}/${bin}" ./cmd/gibbon
    cp README.md "$stage/"
    if [ "$os" = windows ]; then
        (cd dist && zip -qr "${name}.zip" "$name")
    else
        tar -czf "${stage}.tar.gz" -C dist "$name"
    fi
    rm -rf "$stage"
    echo "built ${name}"
done
(cd dist && sha256sum -- *.zip *.tar.gz > checksums.txt)
