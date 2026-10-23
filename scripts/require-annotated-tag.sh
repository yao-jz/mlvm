#!/usr/bin/env bash
# Fail unless the current GitHub tag ref is an annotated tag (not a lightweight tag).
set -euo pipefail
tag="${1:-${GITHUB_REF_NAME:-}}"
if [[ -z "$tag" ]]; then
  echo "usage: $0 <tag>" >&2
  exit 1
fi
kind=$(git cat-file -t "$tag" 2>/dev/null || true)
if [[ "$kind" != "tag" ]]; then
  echo "error: $tag is '$kind'; releases require an annotated tag (git tag -a / git tag -s)" >&2
  exit 1
fi
echo "ok  annotated tag $tag"
