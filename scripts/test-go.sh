#!/usr/bin/env bash
set -euo pipefail

LAYER="${1:-unit}"
GOCACHE_DIR="${GOCACHE:-$PWD/.cache/go-build}"

mkdir -p "$GOCACHE_DIR"
export GOCACHE="$GOCACHE_DIR"

case "$LAYER" in
  unit)
    exec go test ./cmd/tabula
    ;;
  smoke)
    exec go test ./internal/kernel -run '^(TestSessionLifecycle|TestSessionRegistryGetOrCreate|TestSessionRegistryGet|TestSessionRegistryRemove|TestSessionRegistryAll|TestSessionEndEmittedOnLastClientLeave|TestConnectJoinHandshake|TestInitOnJoin|TestMessageRouting|TestExecBasic|TestSpawnListKill|TestShutdownGraceful)$'
    ;;
  all)
    go test ./cmd/tabula
    exec "$0" smoke
    ;;
  *)
    echo "usage: scripts/test-go.sh [unit|smoke|all]" >&2
    exit 1
    ;;
esac
