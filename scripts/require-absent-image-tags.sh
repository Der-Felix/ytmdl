#!/usr/bin/env bash
# Exit 0 only if none of the given image references exists in its registry.
#
# release.yml runs this before it pushes candidate images and again right
# before it gives qualified candidates their version tag. Published version
# tags are immutable: once someone may have pulled X.Y.Z, that name must never
# point at another image. Any answer other than a clear "not found" fails
# closed.
set -euo pipefail

if [ "$#" -eq 0 ]; then
  echo "usage: $0 IMAGE_REF..." >&2
  exit 2
fi

status=0
for ref in "$@"; do
  code=0
  out="$(docker buildx imagetools inspect "${ref}" 2>&1)" || code=$?
  if [ "${code}" -eq 0 ]; then
    echo "::error::${ref} already exists. Refusing to overwrite an immutable image tag." >&2
    status=1
  elif echo "${out}" | grep -qiE "not found|manifest unknown|no such manifest"; then
    echo "Absent: ${ref}"
  else
    echo "::error::Could not determine whether ${ref} exists; failing closed: ${out}" >&2
    status=1
  fi
done
exit "${status}"
