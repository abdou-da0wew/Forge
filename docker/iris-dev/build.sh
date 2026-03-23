#!/bin/bash
# Build the iris-dev Docker image
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
IMAGE_NAME="${IMAGE_NAME:-iris-dev:latest}"

echo "Building $IMAGE_NAME..."

docker build \
    -t "$IMAGE_NAME" \
    -f "$SCRIPT_DIR/Dockerfile" \
    "$SCRIPT_DIR"

echo ""
echo "Build complete!"
echo "Image: $IMAGE_NAME"
echo ""
echo "Test with:"
echo "  docker run --rm $IMAGE_NAME cargo --version"
echo "  docker run --rm $IMAGE_NAME pkg-config --exists gtk4 && echo 'GTK4 OK'"
