#!/usr/bin/env bash
# Fails when content/ changed since BASE but content/site.yml still carries the same release label.
# Every content change must start a new Content Release so stats can compare releases.
# Usage: scripts/check-content-release.sh [base-ref]   (default: $BASE_REF or origin/main)
set -euo pipefail

base="${1:-${BASE_REF:-origin/main}}"
if ! git rev-parse --verify -q "${base}^{commit}" >/dev/null; then
  echo "release gate: base ref '${base}' not found; nothing to compare" >&2
  exit 0
fi
merge_base="$(git merge-base "${base}" HEAD)"
changed="$(git diff --name-only "${merge_base}" HEAD -- content/)"
if [ -z "${changed}" ]; then
  echo "release gate: content/ unchanged since ${base}"
  exit 0
fi

tmp="$(mktemp -d)"
trap 'rm -rf "${tmp}"' EXIT
new_label="$(go run ./cmd/release-label content/site.yml)"
if git cat-file -e "${merge_base}:content/site.yml" 2>/dev/null; then
  git show "${merge_base}:content/site.yml" > "${tmp}/old.yml"
  old_label="$(go run ./cmd/release-label "${tmp}/old.yml")"
else
  old_label=""
fi

if [ -n "${old_label}" ] && [ "${old_label}" = "${new_label}" ]; then
  echo "release gate: content/ changed but release.label is still '${new_label}'." >&2
  echo "Give content/site.yml a new release label and note (see README, 'Editing content')." >&2
  echo "Changed files:" >&2
  echo "${changed}" | sed 's/^/  /' >&2
  exit 1
fi
echo "release gate: content/ changed and release moved '${old_label:-<none>}' -> '${new_label}'"
