#!/usr/bin/env bash
#
# scripts/generate-release-notes.sh
# Canonical release note generator & validator for YTMDL releases.
#
# Usage:
#   scripts/generate-release-notes.sh [flags]
#   scripts/generate-release-notes.sh --validate <file>
#
# Flags:
#   -v, --version <ver>         Target version (e.g. 0.18.1, default: .release-version)
#   -c, --changelog <path>      Path to CHANGELOG.md (default: CHANGELOG.md)
#   -p, --prev-tag <tag>        Previous release git tag (e.g. v0.18.0, default: auto-detected)
#   -r, --repo <owner/repo>     GitHub repository (default: Der-Felix/ytmdl)
#   -m, --migration <text>      Custom migration note under Update section
#   -o, --output <path>         Output file path (default: stdout)
#   --validate <path>           Validate existing release notes file and exit
#
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"

validate_notes() {
  local file="$1"
  if [ ! -f "$file" ]; then
    echo "::error::Release notes file not found: $file" >&2
    return 1
  fi

  local content
  content="$(cat "$file")"

  if [ -z "$(echo "$content" | tr -d '[:space:]')" ]; then
    echo "::error::Release notes file is empty" >&2
    return 1
  fi

  # Must contain Highlights or Changes
  if ! echo "$content" | grep -Eq "^## (Highlights|Changes|Queue improvements|Added|Fixed)"; then
    echo "::error::Release notes must contain a ## Highlights or ## Changes section" >&2
    return 1
  fi

  # Must contain Full Changelog link
  if ! echo "$content" | grep -Eq "(\*\*Full Changelog:\*\*|Full Changelog:)"; then
    echo "::error::Release notes must contain a Full Changelog compare link" >&2
    return 1
  fi

  # Must not collapse to ONLY Full Changelog / compare link
  # Filter out title, headers, empty lines, and the compare link
  local body_lines
  body_lines="$(echo "$content" | grep -vE "^(#.*|\s*|\*\*Full Changelog.*|Full Changelog.*)$" | tr -d '[:space:]')"

  if [ ${#body_lines} -lt 40 ]; then
    echo "::error::Release notes collapsed to only compare link or insufficient content (${#body_lines} chars)" >&2
    return 1
  fi

  # Must contain at least one bullet point item
  if ! echo "$content" | grep -Eq "^- "; then
    echo "::error::Release notes must contain descriptive bullet points (- ...)" >&2
    return 1
  fi

  echo "Release notes validation PASSED for $file"
  return 0
}

VERSION=""
CHANGELOG="${ROOT_DIR}/CHANGELOG.md"
PREV_TAG=""
REPO="Der-Felix/ytmdl"
MIGRATION=""
OUTPUT=""

DO_VALIDATE=0

while [ $# -gt 0 ]; do
  case "$1" in
    --validate)
      if [ $# -ge 2 ] && [[ "$2" != -* ]] && [ -f "$2" ]; then
        validate_notes "$2"
        exit $?
      else
        DO_VALIDATE=1
        shift 1
      fi
      ;;
    -v|--version)
      VERSION="$2"
      shift 2
      ;;
    -c|--changelog)
      CHANGELOG="$2"
      shift 2
      ;;
    -p|--prev-tag)
      PREV_TAG="$2"
      shift 2
      ;;
    -r|--repo)
      REPO="$2"
      shift 2
      ;;
    -m|--migration)
      MIGRATION="$2"
      shift 2
      ;;
    --manifest)
      MANIFEST="$2"
      shift 2
      ;;
    -o|--output)
      OUTPUT="$2"
      shift 2
      ;;
    -h|--help)
      echo "Usage: scripts/generate-release-notes.sh [--version <ver>] [--changelog <path>] [--prev-tag <tag>] [--manifest <path>] [--migration <text>] [--output <file>] [--validate <file>]"
      exit 0
      ;;
    *)
      echo "Unknown argument: $1" >&2
      exit 1
      ;;
  esac
done

if [ -z "$VERSION" ]; then
  if [ -f "${ROOT_DIR}/.release-version" ]; then
    VERSION="$(tr -d '[:space:]' < "${ROOT_DIR}/.release-version" | sed 's/^v//')"
  else
    echo "Error: Version not specified and .release-version not found" >&2
    exit 1
  fi
fi

if [ -z "$PREV_TAG" ] && [ -f "$CHANGELOG" ]; then
  PREV_TAG="$(awk -v ver="${VERSION}" '
    /^## [0-9]/ {
      if (found && !prev) {
        prev = $2
        exit
      }
      if ($2 == ver) { found=1 }
    }
    END { if (prev) print "v" prev }
  ' "${CHANGELOG}" 2>/dev/null || true)"
fi

if [ -z "$PREV_TAG" ]; then
  PREV_TAG="$(git -C "${ROOT_DIR}" tag -l "v*" --sort=-v:refname 2>/dev/null | grep -v "^v${VERSION}$" | head -n 1 || true)"
fi

if [ ! -f "$CHANGELOG" ]; then
  echo "Error: Changelog file not found: $CHANGELOG" >&2
  exit 1
fi

# Extract version section from CHANGELOG
RAW_BODY="$(awk -v ver="${VERSION}" '
  /^## [0-9]/ {
    if (found) exit
    if ($2 == ver) { found=1; next }
  }
  found { print }
' "${CHANGELOG}")"

if [ -z "$(echo "${RAW_BODY}" | tr -d '[:space:]')" ]; then
  echo "Error: Could not find changelog section for version ${VERSION} in ${CHANGELOG}" >&2
  exit 1
fi

# Determine migration wording if not explicitly provided
if [ -z "$MIGRATION" ] && [ -n "${MANIFEST:-}" ] && [ -f "$MANIFEST" ]; then
  target_schema="$(grep -o '"target_schema":[[:space:]]*[0-9]*' "$MANIFEST" | head -n 1 | tr -dc '0-9' || true)"
  if [ "$target_schema" = "9" ]; then
    MIGRATION="No database migration is required."
  fi
fi

if [ -z "$MIGRATION" ]; then
  if echo "$RAW_BODY" | grep -qiE "(no database migration|keine datenbankmigration|remains at schema)"; then
    MIGRATION="No database migration is required."
  elif echo "$RAW_BODY" | grep -qiE "migration"; then
    MIGRATION="$(echo "$RAW_BODY" | grep -iE "migration" | head -n 1 | sed 's/^[-*] //; s/^\*\*Database Schema:\*\*[[:space:]]*//' | tr -d '\r')"
  else
    echo "Error: Unable to determine migration status for version ${VERSION} from manifest or changelog" >&2
    exit 1
  fi
fi

# Clean raw body:
# 1. Promote ### to ##
# 2. Filter out raw Database Schema line because it will be placed cleanly under ## Update
# 3. Trim leading and trailing blank lines
CLEAN_BODY="$(echo "$RAW_BODY" | grep -vE "^[-*] \*\*Database Schema:\*\*" | sed 's/^### /## /' | awk 'NF {p=1} p')"

UPDATE_HEADING="Update"
if ! echo "$MIGRATION" | grep -qiE "(no database migration|keine datenbankmigration|remains at schema)"; then
  if echo "$MIGRATION" | grep -qiE "migration"; then
    UPDATE_HEADING="Upgrade Notes"
    MIGRATION="$(echo "$MIGRATION" | sed -E 's/([.!?]) ([A-Z])/\1\n\n\2/g')"
  fi
fi

# Build final notes
NOTES="$(printf "%s\n\n%s\n\n## %s\n\nExisting installations can update using:\n\n\`ytmdlctl update\`\n\n%s\n" \
  "# YTMDL v${VERSION}" \
  "${CLEAN_BODY}" \
  "${UPDATE_HEADING}" \
  "${MIGRATION}")"

if [ -n "$PREV_TAG" ]; then
  COMPARE_URL="https://github.com/${REPO}/compare/${PREV_TAG}...v${VERSION}"
  NOTES="$(printf "%s\n\n**Full Changelog:** [%s...v%s](%s)\n" \
    "${NOTES}" \
    "${PREV_TAG}" \
    "${VERSION}" \
    "${COMPARE_URL}")"
fi

if [ "$DO_VALIDATE" -eq 1 ]; then
  TMP_VAL="$(mktemp)"
  printf "%s\n" "$NOTES" > "$TMP_VAL"
  validate_notes "$TMP_VAL"
  VAL_RET=$?
  rm -f "$TMP_VAL"
  exit $VAL_RET
fi

if [ -n "$OUTPUT" ]; then
  mkdir -p "$(dirname "$OUTPUT")"
  printf "%s\n" "$NOTES" > "$OUTPUT"
  echo "Generated release notes written to $OUTPUT"
else
  printf "%s\n" "$NOTES"
fi
