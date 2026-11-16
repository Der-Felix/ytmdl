#!/usr/bin/env bash
# Delete the GHCR package versions that belong to one candidate tag of a
# release run, and nothing else.
#
# release.yml calls this for a run that failed before its candidates got the
# version tag, and at the end of a rehearsal. Deleting a package version
# deletes the manifest itself, with every tag on it, so the rules are strict:
#
#   * the index is deleted only if every tag on it is a throwaway tag
#     (candidate-* or rehearsal-*); an index that also carries a version tag
#     or latest is published and stays;
#   * a platform or attestation manifest is deleted only if no other tagged
#     index in the package refers to it;
#   * any answer that is not understood stops the script before a deletion.
#
# usage: delete-candidate-images.sh [--dry-run] OWNER CANDIDATE_TAG PACKAGE...
# Needs gh (GH_TOKEN with packages write), docker buildx and jq.
set -euo pipefail

dry_run=false
if [ "${1:-}" = "--dry-run" ]; then dry_run=true; shift; fi
if [ "$#" -lt 3 ]; then
  echo "usage: $0 [--dry-run] OWNER CANDIDATE_TAG PACKAGE..." >&2
  exit 2
fi
owner="$1"; candidate="$2"; shift 2
if [[ ! "${candidate}" =~ ^candidate-[0-9A-Za-z._-]+$ ]]; then
  echo "::error::'${candidate}' is not a candidate tag." >&2
  exit 2
fi
owner_lc="$(echo "${owner}" | tr '[:upper:]' '[:lower:]')"
throwaway='^(candidate|rehearsal)-'

for package in "$@"; do
  repository="ghcr.io/${owner_lc}/${package}"
  versions="$(gh api --paginate "/users/${owner}/packages/container/${package}/versions?per_page=100" | jq -s 'add')"

  index="$(echo "${versions}" | jq -c --arg t "${candidate}" '[.[] | select(.metadata.container.tags | index($t))]')"
  count="$(echo "${index}" | jq 'length')"
  if [ "${count}" -eq 0 ]; then
    echo "${package}: no version carries ${candidate}; nothing to delete."
    continue
  fi
  if [ "${count}" -ne 1 ]; then
    echo "::error::${package}: ${count} versions carry ${candidate}; refusing." >&2
    exit 1
  fi
  index_id="$(echo "${index}" | jq -r '.[0].id')"
  index_digest="$(echo "${index}" | jq -r '.[0].name')"
  kept_tags="$(echo "${index}" | jq -r --arg re "${throwaway}" '.[0].metadata.container.tags[] | select(test($re) | not)')"
  if [ -n "${kept_tags}" ]; then
    echo "${package}: ${index_digest} also carries $(echo "${kept_tags}" | tr '\n' ' '); it is published and stays."
    continue
  fi
  [[ "${index_digest}" =~ ^sha256:[0-9a-f]{64}$ ]] || { echo "::error::${package}: unexpected version name '${index_digest}'." >&2; exit 1; }

  children="$(docker buildx imagetools inspect --raw "${repository}@${index_digest}" | jq -r '.manifests[].digest')"
  [ -n "${children}" ] || { echo "::error::${package}: ${index_digest} lists no manifests." >&2; exit 1; }

  # Every manifest that another tagged index refers to is protected.
  protected=""
  while read -r other; do
    [ -n "${other}" ] || continue
    refs="$(docker buildx imagetools inspect --raw "${repository}@${other}" | jq -r '(.manifests // [])[].digest')"
    protected="${protected}"$'\n'"${other}"$'\n'"${refs}"
  done < <(echo "${versions}" | jq -r --argjson id "${index_id}" '.[] | select(.id != $id and (.metadata.container.tags | length > 0)) | .name')

  ids=()
  while read -r child; do
    if echo "${protected}" | grep -qxF "${child}"; then
      echo "${package}: ${child} is used by another tagged image; kept."
      continue
    fi
    child_id="$(echo "${versions}" | jq -r --arg d "${child}" '.[] | select(.name == $d) | select(.metadata.container.tags | length == 0) | .id')"
    if [ -z "${child_id}" ]; then
      echo "${package}: ${child} has no untagged package version; kept."
      continue
    fi
    ids+=("${child_id}")
  done <<< "${children}"
  ids+=("${index_id}")

  for id in "${ids[@]}"; do
    if [ "${dry_run}" = "true" ]; then
      echo "${package}: would delete version ${id}"
    else
      gh api -X DELETE "/users/${owner}/packages/container/${package}/versions/${id}"
      echo "${package}: deleted version ${id}"
    fi
  done
done
