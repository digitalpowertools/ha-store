#!/usr/bin/env bash
# Build and push the add-on images to ghcr.io from this machine
# (alternative to the GitHub Action in .github/workflows/docker.yml).
#
#   ./build-images.sh [version]        # version defaults to config.yaml
#
# Requires: docker with buildx, and `docker login ghcr.io`.
set -euo pipefail

cd "$(dirname "$0")/littleprinter"

VERSION="${1:-$(sed -n 's/^version: "\(.*\)"/\1/p' config.yaml | tr -d '"')}"
REPO_BASE="ghcr.io/digitalpowertools/littleprinter"

# HA arch name -> docker platform
declare -A ARCHS=( [amd64]=linux/amd64 [aarch64]=linux/arm64 [armv7]=linux/arm/v7 )

for ha_arch in amd64 aarch64 armv7; do
    platform="${ARCHS[$ha_arch]}"
    echo "=== Building $ha_arch ($platform) -> $REPO_BASE-$ha_arch:$VERSION"
    docker buildx build \
        --platform "$platform" \
        -t "$REPO_BASE-$ha_arch:$VERSION" \
        -t "$REPO_BASE-$ha_arch:latest" \
        --push \
        .
done

echo "All architectures pushed."
