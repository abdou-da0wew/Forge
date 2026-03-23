#!/bin/bash
set -euo pipefail

# Build script for forge-proxy static binary
# Creates a fully static binary with no libc dependencies

VERSION=${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo "dev")}
OUTPUT="forge-proxy"

echo "Building forge-proxy $VERSION..."

# Static build flags
export CGO_ENABLED=0
export GOOS=linux
export GOARCH=amd64

go build \
    -tags netgo \
    -ldflags "-w -s -extldflags '-static' -X main.Version=$VERSION" \
    -o "$OUTPUT" \
    ./cmd/forge-proxy/

echo "Verifying static binary..."
if file "$OUTPUT" | grep -q "statically linked"; then
    echo "✅ Binary is fully static"
else
    echo "⚠️  Binary may have dynamic dependencies"
    ldd "$OUTPUT" 2>/dev/null || true
fi

SIZE=$(du -sh "$OUTPUT" | cut -f1)
SHA256=$(sha256sum "$OUTPUT" | cut -d' ' -f1)

echo ""
echo "Built: $OUTPUT ($SIZE)"
echo "SHA256: $SHA256"
echo "$SHA256" > "$OUTPUT.sha256"

echo ""
echo "Done!"
