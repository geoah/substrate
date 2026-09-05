#!/usr/bin/env bash
#
# The version-bump guard over the shipped trees: a changed declaration ships a
# changed version, or the merge does not happen. The working tree's kinds/ and
# samples/ are diffed against the merge base with the base branch (the PR's
# base in CI, origin/main or main locally) by cmd/vocabularydiff, which compares
# documents minus their `version` key through the one comparator
# (vocabulary.CompareVersions).
#
# BOTH trees, because both are installed the same way: a sample package is
# served by the catalog and upgraded by the same version diff a provider is,
# so a sample whose declaration moved under an unmoved version is the same
# silent non-upgrade.
#
# Why it exists: the boot upgrade, the bundle upgrade preview and the console's
# upgrade offer all key on `version`. A definition that changes under an unmoved
# version is an upgrade no repository will ever receive, silently.
#
# KINDS_CHECK_BASE overrides the base commit, for trying the check by hand:
#   KINDS_CHECK_BASE=HEAD~1 mise run kinds:check
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

base_commit="${KINDS_CHECK_BASE:-}"
if [ -z "$base_commit" ]; then
  base_branch="${GITHUB_BASE_REF:-main}"
  # CI checkouts are shallow (fetch-depth 1); the merge base needs history.
  if [ -f "$(git rev-parse --git-dir)/shallow" ]; then
    git fetch --quiet --unshallow origin || git fetch --quiet origin
  fi
  if git rev-parse --verify --quiet "origin/${base_branch}" >/dev/null; then
    base_commit="$(git merge-base HEAD "origin/${base_branch}")"
  elif git rev-parse --verify --quiet "${base_branch}" >/dev/null; then
    base_commit="$(git merge-base HEAD "${base_branch}")"
  elif [ -n "${CI:-}" ]; then
    # In CI this is the merge gate. A base it cannot resolve means the fetch
    # above failed, and passing then would be a green job that checked
    # nothing — the one outcome worse than a red one.
    echo "kinds:check: cannot resolve base branch ${base_branch}; refusing to pass without checking" >&2
    exit 1
  else
    echo "kinds:check: no base branch to diff against; skipping" >&2
    exit 0
  fi
fi

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

status=0
for tree in kinds samples; do
  # Nothing under this tree moved (committed or not): nothing to check.
  if git diff --quiet "$base_commit" -- "$tree/"; then
    continue
  fi
  # A tree the base did not have yet diffs against an empty directory: every
  # declaration in it is an addition, which needs no bump.
  mkdir -p "$tmp/$tree"
  if git rev-parse --verify --quiet "$base_commit:$tree" >/dev/null; then
    git archive "$base_commit" "$tree" | tar -x -C "$tmp"
  fi
  go run ./cmd/vocabularydiff "$tmp/$tree" "$tree" || status=1
done
exit "$status"
