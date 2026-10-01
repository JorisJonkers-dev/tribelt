#!/usr/bin/env bash
# Content Release gate. Enforced on the release PR: when content/ changed since the latest vX.Y.Z tag,
# content/site.yml must carry a release label other than that tag's. Content PRs only get a notice,
# so several of them can share one label within a release.
#
# Usage: scripts/check-content-release.sh [release [head] | pr [base]]
#   release  enforce against the latest v*.*.* tag reachable from head (default HEAD)
#   pr       advisory for one change against base (default $BASE_REF, else origin/main)
#   (none)   release when the PR head ref ($HEAD_REF or $GITHUB_HEAD_REF) starts with release-please--,
#            pr otherwise. Deciding here, not in a workflow `if:`, keeps the choice under test.
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
tmp="$(mktemp -d)"
trap 'rm -rf "${tmp}"' EXIT

# label_at prints release.label of content/site.yml at a git ref, or nothing when the file is absent.
label_at() {
  local file
  file="${tmp}/site-$(git rev-parse --short "$1").yml"
  git show "$1:content/site.yml" > "${file}" 2>/dev/null || return 0
  go -C "${root}" run ./cmd/release-label "${file}"
}

latest_tag() {
  git describe --tags --abbrev=0 --match 'v[0-9]*.[0-9]*.[0-9]*' "$1" 2>/dev/null || true
}

annotate() {
  local level="$1" msg="$2"
  if [ "${GITHUB_ACTIONS:-}" = "true" ]; then
    echo "::${level} title=Content Release::${msg}"
  fi
  echo "release gate: ${msg}"
}

release() {
  local head="${1:-HEAD}" tag changed old new
  tag="$(latest_tag "${head}")"
  if [ -z "${tag}" ]; then
    echo "release gate: no previous v*.*.* tag; the first release passes"
    return 0
  fi
  changed="$(git diff --name-only "${tag}" "${head}" -- content/)"
  if [ -z "${changed}" ]; then
    echo "release gate: content/ unchanged since ${tag}"
    return 0
  fi
  old="$(label_at "${tag}")"
  new="$(label_at "${head}")"
  if [ -n "${old}" ] && [ "${old}" = "${new}" ]; then
    annotate error "content changed since ${tag} but release.label is still '${new}'; bump it in content/site.yml on main"
    echo "Changed since ${tag}:" >&2
    echo "${changed}" | sed 's/^/  /' >&2
    return 1
  fi
  echo "release gate: content/ changed since ${tag} and release.label moved '${old:-<none>}' -> '${new}'"
}

pr() {
  local base="${1:-${BASE_REF:-origin/main}}" merge_base changed ref tag old new
  if ! git rev-parse --verify -q "${base}^{commit}" >/dev/null; then
    echo "release gate: base ref '${base}' not found; nothing to compare"
    return 0
  fi
  merge_base="$(git merge-base "${base}" HEAD)"
  changed="$(git diff --name-only "${merge_base}" HEAD -- content/)"
  if [ -z "${changed}" ]; then
    echo "release gate: content/ unchanged since ${base}"
    return 0
  fi
  tag="$(latest_tag HEAD)"
  ref="${tag:-${merge_base}}"
  old="$(label_at "${ref}")"
  new="$(label_at HEAD)"
  if [ -n "${old}" ] && [ "${old}" = "${new}" ]; then
    annotate notice "content/ changed and release.label is still '${new}' (as at ${tag:-the base}). Content PRs may share one label, but the release PR fails until release.label is bumped in content/site.yml on main."
    return 0
  fi
  echo "release gate: content/ changed; release.label '${old:-<none>}' -> '${new}' since ${tag:-the base}"
}

mode="${1:-}"
case "${mode}" in
  release | pr) shift ;;
  "")
    head_ref="${HEAD_REF:-${GITHUB_HEAD_REF:-}}"
    if [[ "${head_ref}" == release-please--* ]]; then mode=release; else mode=pr; fi
    echo "release gate: ${mode} mode (head ref '${head_ref:-<none>}')"
    ;;
  *)
    echo "usage: $0 [release [head] | pr [base]]" >&2
    exit 2
    ;;
esac
"${mode}" "$@"
