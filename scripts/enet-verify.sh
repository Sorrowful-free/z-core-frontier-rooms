#!/usr/bin/env bash
# Локальная верификация ENet-кода (build-теги `enet && cgo`) через Docker-образ
# из .devcontainer/Dockerfile — хосту libenet-dev не нужен.
# Использование: ./scripts/enet-verify.sh [build|test|vet|all]  (default: all)
set -euo pipefail

IMAGE="zcore-rooms-dev"
REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"

if ! docker image inspect "$IMAGE" >/dev/null 2>&1; then
  echo "building $IMAGE (host network — bridge DNS на этой машине мёртв)..."
  docker build --network=host -t "$IMAGE" -f "$REPO_ROOT/.devcontainer/Dockerfile" "$REPO_ROOT/.devcontainer/"
fi

run() {
  docker run --rm --network=host \
    -v "$REPO_ROOT":/work -w /work \
    -v "$(go env GOMODCACHE)":/go/pkg/mod \
    -e CGO_ENABLED=1 -e GOCACHE=/tmp/gocache -e GOFLAGS=-mod=mod \
    "$IMAGE" "$@"
}

mode="${1:-all}"
case "$mode" in
  build) run go build -tags enet ./... ;;
  vet)   run go vet -tags enet ./internal/... ./cmd/... ./tests/... ;;
  test)  run go test -race -count=1 -tags enet ./tests/delivery/enet/... ;;
  all)
    run go build -tags enet ./...
    run go vet -tags enet ./internal/... ./cmd/... ./tests/...
    run go test -race -count=1 -tags enet ./tests/delivery/enet/...
    ;;
  *) echo "usage: $0 [build|test|vet|all]" >&2; exit 2 ;;
esac
