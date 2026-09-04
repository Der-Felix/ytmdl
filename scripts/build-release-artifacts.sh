#!/usr/bin/env bash
#
# build-release-artifacts.sh
# Deterministically compiles ytmdlctl release binaries for all supported platforms
# and generates SHA256SUMS.
#
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"

VERSION="${VERSION:-}"
if [ -z "${VERSION}" ]; then
  if [ -f "${ROOT_DIR}/.release-version" ]; then
    VERSION="$(tr -d '[:space:]' < "${ROOT_DIR}/.release-version" | sed 's/^v//')"
  else
    VERSION="dev"
  fi
fi

COMMIT="${COMMIT:-${GITHUB_SHA:-$(git -C "${ROOT_DIR}" rev-parse --short HEAD 2>/dev/null || echo "none")}}"
BUILD_DATE="${BUILD_DATE:-$(date -u +"%Y-%m-%dT%H:%M:%SZ")}"
OUTPUT_DIR="${OUTPUT_DIR:-${ROOT_DIR}/dist}"

mkdir -p "${OUTPUT_DIR}"

TARGETS=(
  "linux/amd64"
  "linux/arm64"
  "darwin/amd64"
  "darwin/arm64"
)

echo "==> Building ytmdlctl release binaries"
echo "    Version:    ${VERSION}"
echo "    Commit:     ${COMMIT}"
echo "    Build Date: ${BUILD_DATE}"
echo "    Output:     ${OUTPUT_DIR}"
echo ""

LDFLAGS="-s -w -X 'main.version=${VERSION}' -X 'main.commit=${COMMIT}' -X 'main.date=${BUILD_DATE}'"

for target in "${TARGETS[@]}"; do
  GOOS="${target%%/*}"
  GOARCH="${target##*/}"
  BINARY_NAME="ytmdlctl-${GOOS}-${GOARCH}"
  OUT_PATH="${OUTPUT_DIR}/${BINARY_NAME}"

  echo "  --> Compiling ${BINARY_NAME} (${GOOS}/${GOARCH})..."
  CGO_ENABLED=0 GOOS="${GOOS}" GOARCH="${GOARCH}" go -C "${ROOT_DIR}/backend" build \
    -trimpath \
    -ldflags "${LDFLAGS}" \
    -o "${OUT_PATH}" \
    ./cmd/ytmdlctl
done

echo ""
echo "==> Generating SHA256SUMS..."
(
  cd "${OUTPUT_DIR}"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum ytmdlctl-* > SHA256SUMS
    sha256sum -c SHA256SUMS
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 ytmdlctl-* > SHA256SUMS
    shasum -a 256 -c SHA256SUMS
  else
    echo "Error: neither sha256sum nor shasum found" >&2
    exit 1
  fi
)

echo ""
echo "==> Release artifacts ready in ${OUTPUT_DIR}:"
ls -lh "${OUTPUT_DIR}"
