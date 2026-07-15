#!/usr/bin/env bash
# Build and publish a full Tabula GitHub release without GitHub Actions.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
VERSION="${VERSION:-$(tr -d '\n' < "$ROOT_DIR/VERSION")}" 
TAG="${TAG:-v${VERSION#v}}"
VERSION_BARE="${TAG#v}"
COMMIT="${COMMIT:-$(git -C "$ROOT_DIR" rev-parse --short HEAD)}"
DATE="${DATE:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"
OUT_DIR="${OUT_DIR:-$ROOT_DIR/dist/release-$TAG}"
DRY_RUN="${DRY_RUN:-0}"
NOTES="${NOTES:-Manual release for $TAG.}"
RELEASE_REPO="${RELEASE_REPO:-${GITHUB_REPOSITORY:-bamanoz/tabula}}"

info() { printf '\033[1;34m==>\033[0m %s\n' "$*"; }
ok() { printf '\033[1;32m  ✓\033[0m %s\n' "$*"; }
die() { printf '\033[1;31merror:\033[0m %s\n' "$*" >&2; exit 1; }

need() {
  command -v "$1" >/dev/null 2>&1 || die "required command not found: $1"
}

build_tar() {
  local goos="$1" goarch="$2"
  local work="$OUT_DIR/work/${goos}_${goarch}"
  local archive="$OUT_DIR/tabula_${VERSION_BARE}_${goos}_${goarch}.tar.gz"
  mkdir -p "$work"
  info "Building $goos/$goarch"
  GOOS="$goos" GOARCH="$goarch" CGO_ENABLED=0 go build -trimpath -ldflags "$LDFLAGS" -o "$work/tabula" ./cmd/tabula
  GOOS="$goos" GOARCH="$goarch" CGO_ENABLED=0 go build -trimpath -ldflags "$LDFLAGS" -o "$work/tabula-runtime" ./cmd/tabula-runtime
  tar -C "$work" -czf "$archive" tabula tabula-runtime
  ok "$(basename "$archive")"
}

build_zip() {
  local goos="$1" goarch="$2"
  local work="$OUT_DIR/work/${goos}_${goarch}"
  local archive="$OUT_DIR/tabula_${VERSION_BARE}_${goos}_${goarch}.zip"
  mkdir -p "$work"
  info "Building $goos/$goarch"
  GOOS="$goos" GOARCH="$goarch" CGO_ENABLED=0 go build -trimpath -ldflags "$LDFLAGS" -o "$work/tabula.exe" ./cmd/tabula
  GOOS="$goos" GOARCH="$goarch" CGO_ENABLED=0 go build -trimpath -ldflags "$LDFLAGS" -o "$work/tabula-runtime.exe" ./cmd/tabula-runtime
  (cd "$work" && zip -q "$archive" tabula.exe tabula-runtime.exe)
  ok "$(basename "$archive")"
}

main() {
  need go
  need git
  need tar
  need zip
  need shasum
  need gh

  cd "$ROOT_DIR"

  LDFLAGS="-s -w -X main.version=$VERSION_BARE -X main.commit=$COMMIT -X main.date=$DATE"
  rm -rf "$OUT_DIR"
  mkdir -p "$OUT_DIR/work"

  build_tar darwin amd64
  build_tar darwin arm64
  build_tar linux amd64
  build_tar linux arm64
  build_zip windows amd64

  info "Packaging runtime payload"
  bash scripts/package-skills.sh "$TAG"
  mv "extra/tabula-skills-$TAG.tar.gz" "$OUT_DIR/"
  ok "tabula-skills-$TAG.tar.gz"

  info "Writing checksums"
  (cd "$OUT_DIR" && shasum -a 256 tabula_${VERSION_BARE}_* "tabula-skills-$TAG.tar.gz" > checksums.txt)
  rm -rf "$OUT_DIR/work"

  if [ "$DRY_RUN" = "1" ]; then
    info "Dry run complete; assets are in $OUT_DIR"
    ls -lh "$OUT_DIR"
    exit 0
  fi

  local head_tag
  head_tag="$(git tag --points-at HEAD | grep -Fx "$TAG" || true)"
  [ -n "$head_tag" ] || die "HEAD is not tagged with $TAG"

  info "Publishing GitHub release $TAG to $RELEASE_REPO"
  if gh release view "$TAG" --repo "$RELEASE_REPO" >/dev/null 2>&1; then
    gh release upload "$TAG" "$OUT_DIR"/* --clobber --repo "$RELEASE_REPO"
    gh release edit "$TAG" --draft=false --latest --notes "$NOTES" --repo "$RELEASE_REPO"
  else
    gh release create "$TAG" "$OUT_DIR"/* --title "$TAG" --notes "$NOTES" --latest --repo "$RELEASE_REPO"
  fi
  ok "release published: $TAG"
}

main "$@"
