#!/usr/bin/env bash
#
# The dialect guard over the declaration format: a key added to, removed from
# or moved between the closed key sets of the declaration documents raises
# vocabulary.Dialect, and the number never goes down (decision 0268). A
# package states the dialect it needs and a server publishes the one it reads,
# so a key that changed under an unchanged number answers both wrongly.
#
# NOT in `lint`, for the same reason `features:check` is not: it reads the
# DIFF. The plain test (TestDialectKeysMatchTheGolden) holds the key sets to
# the golden in the tree, and `-update-dialect` holds a rewrite to the golden
# it replaces, but neither sees the base: two branches that each add a key and
# each raise the number from 3 to 4 merge cleanly into two keys under one
# number. This compares the tree with the merge base's golden, through the
# test's own rule (dialectProblems).
#
# DIALECT_CHECK_BASE overrides the base commit, for trying the check by hand:
#   DIALECT_CHECK_BASE=HEAD~1 mise run dialect:check
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

golden=internal/vocabulary/testdata/dialect.golden.json

base_commit="${DIALECT_CHECK_BASE:-}"
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
    # In CI this is the merge gate: a base it cannot resolve must not read as
    # a pass.
    echo "dialect:check: cannot resolve base branch ${base_branch}; refusing to pass without checking" >&2
    exit 1
  else
    echo "dialect:check: no base branch to diff against; skipping" >&2
    exit 0
  fi
fi

# The golden did not move (committed or not): nothing to check.
if git diff --quiet "$base_commit" -- "$golden"; then
  exit 0
fi

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

# A base from before the golden holds nothing to compare with.
if ! git show "${base_commit}:${golden}" >"$tmp/base.json" 2>/dev/null; then
  exit 0
fi

DIALECT_BASE_GOLDEN="$tmp/base.json" go test -count=1 -run '^TestDialectAgainstTheBase$' ./internal/vocabulary/
