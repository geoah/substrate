#!/usr/bin/env bash
#
# The revision guard over the discovery feature table: a feature's revision
# only goes up, and a feature whose routes changed carries a higher one than
# the base branch's. A client compares the number ("at least 3") before it
# uses what a revision added, so a number that stood still under a new route,
# or went down, answers that client wrongly.
#
# NOT in `lint`, for the same reason `kinds:check` is not: it reads the DIFF.
# The plain test (TestFeaturesMatchTheGolden) holds the table to the golden in
# the tree, and `-update` holds a rewrite to the golden it replaces, but
# neither sees the base: two branches that each add a route to one feature and
# each raise it from 2 to 3 merge cleanly into a feature that gained two
# routes under one number. This compares the tree with the merge base's
# golden, through the test's own rule (revisionProblems).
#
# FEATURES_CHECK_BASE overrides the base commit, for trying the check by hand:
#   FEATURES_CHECK_BASE=HEAD~1 mise run features:check
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

golden=internal/api/testdata/features.golden.json

base_commit="${FEATURES_CHECK_BASE:-}"
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
    echo "features:check: cannot resolve base branch ${base_branch}; refusing to pass without checking" >&2
    exit 1
  else
    echo "features:check: no base branch to diff against; skipping" >&2
    exit 0
  fi
fi

# The table and its golden did not move (committed or not): nothing to check.
if git diff --quiet "$base_commit" -- internal/api/features.go "$golden"; then
  exit 0
fi

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

# A base from before the table has no golden, and every feature is then new.
if ! git show "${base_commit}:${golden}" >"$tmp/base.json" 2>/dev/null; then
  echo '[]' >"$tmp/base.json"
fi

FEATURES_BASE_GOLDEN="$tmp/base.json" go test -count=1 -run '^TestFeatureRevisionsAgainstTheBase$' ./internal/api/
