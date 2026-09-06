#!/usr/bin/env bash
#
# validate-release-metadata.sh
# Validates version, schema, and release consistency across the entire repository.
# Exits 0 on success, exits 1 on any consistency violation.
#
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

TARGET_VERSION=""
TARGET_SCHEMA=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    -v|--version)
      TARGET_VERSION="$2"
      shift 2
      ;;
    -s|--schema|--target-schema)
      TARGET_SCHEMA="$2"
      shift 2
      ;;
    -r|--repo-root)
      REPO_ROOT="$2"
      shift 2
      ;;
    -h|--help)
      echo "Usage: $0 [options]"
      echo "Options:"
      echo "  -v, --version <ver>         Expected version (default: read from .release-version)"
      echo "  -s, --schema <num>          Expected schema (default: derived from latest migration)"
      echo "  -r, --repo-root <path>      Repository root path (default: parent of script dir)"
      exit 0
      ;;
    *)
      echo "Unknown option: $1" >&2
      exit 1
      ;;
  esac
done

ERRORS=0

log_error() {
  echo "::error::[RELEASE CONSISTENCY ERROR] $1" >&2
  ERRORS=$((ERRORS + 1))
}

log_pass() {
  echo "[PASS] $1"
}

# ==============================================================================
# 1. CANONICAL VERSION VALIDATION
# ==============================================================================
RELEASE_VERSION_FILE="${REPO_ROOT}/.release-version"
if [ ! -f "${RELEASE_VERSION_FILE}" ]; then
  log_error "Missing .release-version file at ${RELEASE_VERSION_FILE}"
  CANONICAL_VERSION=""
else
  CANONICAL_VERSION="$(tr -d '[:space:]' < "${RELEASE_VERSION_FILE}" | sed 's/^v//')"
fi

if [ -n "${TARGET_VERSION}" ]; then
  CLEAN_TARGET="$(echo "${TARGET_VERSION}" | tr -d '[:space:]' | sed 's/^v//')"
  if [ -n "${CANONICAL_VERSION}" ] && [ "${CANONICAL_VERSION}" != "${CLEAN_TARGET}" ]; then
    log_error "Target version (${CLEAN_TARGET}) does not match .release-version (${CANONICAL_VERSION})"
  fi
  CANONICAL_VERSION="${CLEAN_TARGET}"
fi

if [ -z "${CANONICAL_VERSION}" ]; then
  log_error "Unable to determine canonical release version"
else
  if [[ ! "${CANONICAL_VERSION}" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    log_error "Canonical version '${CANONICAL_VERSION}' is not valid SemVer (MAJOR.MINOR.PATCH)"
  else
    log_pass "Canonical release version: ${CANONICAL_VERSION}"
  fi
fi

# Check CHANGELOG.md top release heading
CHANGELOG_FILE="${REPO_ROOT}/CHANGELOG.md"
if [ -f "${CHANGELOG_FILE}" ] && [ -n "${CANONICAL_VERSION}" ]; then
  # Match top release header e.g. "## 0.19.0 — 2026-09-05" or "## 0.19.0"
  CHANGELOG_TOP_VER="$(grep -E '^## [0-9]+\.[0-9]+\.[0-9]+' "${CHANGELOG_FILE}" | head -n 1 | sed -E 's/^## ([0-9]+\.[0-9]+\.[0-9]+).*/\1/' || true)"
  if [ -z "${CHANGELOG_TOP_VER}" ]; then
    log_error "No valid release heading found in CHANGELOG.md"
  elif [ "${CHANGELOG_TOP_VER}" != "${CANONICAL_VERSION}" ]; then
    log_error "CHANGELOG.md top release heading (${CHANGELOG_TOP_VER}) does not match canonical version (${CANONICAL_VERSION})"
  else
    log_pass "CHANGELOG.md top release: ${CHANGELOG_TOP_VER}"
  fi
fi

# Check docs/package.json
DOCS_PKG="${REPO_ROOT}/docs/package.json"
if [ -f "${DOCS_PKG}" ] && [ -n "${CANONICAL_VERSION}" ]; then
  DOCS_VER="$(python3 -c "import json; print(json.load(open('${DOCS_PKG}')).get('version', ''))" 2>/dev/null || true)"
  if [ "${DOCS_VER}" != "${CANONICAL_VERSION}" ]; then
    log_error "docs/package.json version (${DOCS_VER}) does not match canonical version (${CANONICAL_VERSION})"
  else
    log_pass "docs/package.json version: ${DOCS_VER}"
  fi
fi

# Check .env.example
ENV_EXAMPLE="${REPO_ROOT}/.env.example"
if [ -f "${ENV_EXAMPLE}" ] && [ -n "${CANONICAL_VERSION}" ]; then
  ENV_VER="$(grep -E '^YTMDL_VERSION=' "${ENV_EXAMPLE}" | head -n 1 | cut -d'=' -f2 | tr -d '[:space:]' || true)"
  if [ "${ENV_VER}" != "${CANONICAL_VERSION}" ]; then
    log_error ".env.example YTMDL_VERSION (${ENV_VER}) does not match canonical version (${CANONICAL_VERSION})"
  else
    log_pass ".env.example YTMDL_VERSION: ${ENV_VER}"
  fi
fi

# Check backend/cmd/ytmdlctl/main.go maintenance default fallback
MAIN_GO="${REPO_ROOT}/backend/cmd/ytmdlctl/main.go"
if [ -f "${MAIN_GO}" ] && [ -n "${CANONICAL_VERSION}" ]; then
  # runReconcileArtists and runMergeArtists define fallback versions for maintenance backup tagging
  STALE_FALLBACKS=$(grep -n -E 'currentVersion = "0\.(1[6-9]|[2-9][0-9])\.' "${MAIN_GO}" | grep -v "${CANONICAL_VERSION}" || true)
  if [ -n "${STALE_FALLBACKS}" ]; then
    log_error "backend/cmd/ytmdlctl/main.go contains stale maintenance currentVersion fallback:\n${STALE_FALLBACKS}"
  else
    log_pass "backend/cmd/ytmdlctl/main.go maintenance fallbacks match ${CANONICAL_VERSION}"
  fi
fi

# ==============================================================================
# 2. CANONICAL SCHEMA VALIDATION
# ==============================================================================
MIGRATIONS_DIR="${REPO_ROOT}/backend/internal/database/migrations"
if [ ! -d "${MIGRATIONS_DIR}" ]; then
  log_error "Migrations directory not found at ${MIGRATIONS_DIR}"
  LATEST_SCHEMA=""
else
  LATEST_MIGRATION_FILE="$(ls -1 "${MIGRATIONS_DIR}/"*.sql 2>/dev/null | sort -V | tail -n 1 || true)"
  if [ -z "${LATEST_MIGRATION_FILE}" ]; then
    log_error "No .sql migration files found in ${MIGRATIONS_DIR}"
    LATEST_SCHEMA=""
  else
    LATEST_SCHEMA="$(basename "${LATEST_MIGRATION_FILE}" | sed -E 's/^([0-9]+).*/\1/' | sed 's/^0*//')"
    log_pass "Latest DB migration: $(basename "${LATEST_MIGRATION_FILE}") (Schema ${LATEST_SCHEMA})"
  fi
fi

if [ -n "${TARGET_SCHEMA}" ]; then
  if [ -n "${LATEST_SCHEMA}" ] && [ "${TARGET_SCHEMA}" != "${LATEST_SCHEMA}" ]; then
    log_error "Specified target schema (${TARGET_SCHEMA}) does not match latest DB migration schema (${LATEST_SCHEMA})"
  fi
  LATEST_SCHEMA="${TARGET_SCHEMA}"
fi

# Check .github/workflows/release.yml manifest-gen schema parameter
WORKFLOW_FILE="${REPO_ROOT}/.github/workflows/release.yml"
if [ -f "${WORKFLOW_FILE}" ] && [ -n "${LATEST_SCHEMA}" ]; then
  WORKFLOW_SCHEMA="$(grep -E '\-\-schema [0-9]+' "${WORKFLOW_FILE}" | head -n 1 | sed -E 's/.*--schema ([0-9]+).*/\1/' || true)"
  if [ -n "${WORKFLOW_SCHEMA}" ] && [ "${WORKFLOW_SCHEMA}" != "${LATEST_SCHEMA}" ]; then
    log_error ".github/workflows/release.yml manifest-gen specifies stale --schema ${WORKFLOW_SCHEMA} (expected Schema ${LATEST_SCHEMA})"
  elif [ -n "${WORKFLOW_SCHEMA}" ]; then
    log_pass ".github/workflows/release.yml manifest-gen matches Schema ${LATEST_SCHEMA}"
  fi
fi

# Check scripts/build-release-artifacts.sh schema parameter if explicitly hardcoded
BUILD_SCRIPT="${REPO_ROOT}/scripts/build-release-artifacts.sh"
if [ -f "${BUILD_SCRIPT}" ] && [ -n "${LATEST_SCHEMA}" ]; then
  BUILD_SCHEMA="$(grep -E '\-\-schema [0-9]+' "${BUILD_SCRIPT}" | head -n 1 | sed -E 's/.*--schema ([0-9]+).*/\1/' || true)"
  if [ -n "${BUILD_SCHEMA}" ] && [ "${BUILD_SCHEMA}" != "${LATEST_SCHEMA}" ]; then
    log_error "scripts/build-release-artifacts.sh specifies stale --schema ${BUILD_SCHEMA} (expected Schema ${LATEST_SCHEMA})"
  fi
fi

# ==============================================================================
# 3. RELEASE NOTES GENERATION & VALIDATION
# ==============================================================================
GEN_NOTES_SCRIPT="${REPO_ROOT}/scripts/generate-release-notes.sh"
if [ -f "${GEN_NOTES_SCRIPT}" ] && [ -n "${CANONICAL_VERSION}" ]; then
  if ! "${GEN_NOTES_SCRIPT}" --version "${CANONICAL_VERSION}" --validate >/dev/null 2>&1; then
    log_error "scripts/generate-release-notes.sh --version ${CANONICAL_VERSION} --validate failed"
  else
    log_pass "Release notes validated successfully for v${CANONICAL_VERSION}"
  fi
fi

# ==============================================================================
# SUMMARY & EXIT
# ==============================================================================
if [ "${ERRORS}" -gt 0 ]; then
  echo "" >&2
  echo "Release metadata validation FAILED with ${ERRORS} consistency error(s)." >&2
  exit 1
fi

echo ""
echo "Release metadata validation PASSED. All versions, schemas, and contracts consistent."
exit 0
