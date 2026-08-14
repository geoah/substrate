#!/usr/bin/env bash
#
# The version-bump guard over kinds/: a changed declaration ships a changed
# version, or the merge does not happen. The working tree's kinds/ is diffed
# against the merge base with the base branch (the PR's base in CI, origin/main
# or main locally) by cmd/vocabularydiff, which compares documents minus their
# `version` key through the one comparator (vocabulary.CompareVersions).
#
# Why it exists: the boot upgrade, the bundle upgrade preview and the console's
# upgrade offer all key on `version`. A definition that changes under an
# unmoved version is an upgrade no repository will ever receive, silently.
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

# Nothing under kinds/ moved (committed or not): nothing to check.
if git diff --quiet "$base_commit" -- kinds/; then
  exit 0
fi

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
git archive "$base_commit" kinds | tar -x -C "$tmp"

exec go run ./cmd/vocabularydiff "$tmp/kinds" kinds
