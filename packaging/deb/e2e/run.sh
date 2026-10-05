#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)
PKG_DIR="$ROOT_DIR/packaging/deb"
IMAGE_TAG="${IMAGE_TAG:-orion-deb-e2e:local}"

docker_build() {
  if docker buildx version >/dev/null 2>&1; then
    docker buildx build --load \
      -f "$PKG_DIR/e2e/Dockerfile" \
      -t "$IMAGE_TAG" \
      "$PKG_DIR"
    return
  fi

  DOCKER_BUILDKIT=0 docker build \
    -f "$PKG_DIR/e2e/Dockerfile" \
    -t "$IMAGE_TAG" \
    "$PKG_DIR"
}

make -C "$ROOT_DIR" build
"$PKG_DIR/build.sh"

docker_build

docker run --rm "$IMAGE_TAG"
