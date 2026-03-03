#!/usr/bin/env bash
set -euo pipefail

VERSION="${1:-dev-$(date +%Y%m%d)}"
COMMIT=$(git rev-parse --short HEAD)
DATE=$(date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS="-X main.version=${VERSION} -X main.commit=${COMMIT} -X main.date=${DATE}"
MAIN=./cmd/pv-migrate
OUTDIR=dist

echo "Building pv-migrate ${VERSION} (commit: ${COMMIT}, date: ${DATE})"

rm -rf "${OUTDIR}"
mkdir -p "${OUTDIR}"

platforms=(
  "darwin:arm64"
  "linux:amd64"
)

for platform in "${platforms[@]}"; do
  GOOS="${platform%%:*}"
  GOARCH="${platform##*:}"
  BINARY="pv-migrate"
  ARCHIVE_NAME="pv-migrate_${VERSION}_${GOOS}_${GOARCH}"
  BUILD_DIR="${OUTDIR}/${ARCHIVE_NAME}"

  echo "  -> ${GOOS}/${GOARCH}"
  mkdir -p "${BUILD_DIR}"

  CGO_ENABLED=0 GOOS="${GOOS}" GOARCH="${GOARCH}" go build \
    -ldflags "${LDFLAGS}" \
    -o "${BUILD_DIR}/${BINARY}" \
    "${MAIN}"

  # Create tar.xz archive
  tar -cJf "${OUTDIR}/${ARCHIVE_NAME}.tar.xz" -C "${OUTDIR}" "${ARCHIVE_NAME}"

  echo "     ${OUTDIR}/${ARCHIVE_NAME}.tar.xz"
done

echo ""
echo "Done! Archives:"
ls -lh "${OUTDIR}"/*.tar.xz