#!/usr/bin/env bash
#
# The revision guard over the discovery feature table: a feature's revision
# only goes up, and a feature whose routes changed carries a higher one than
# the base branch's. A client compares the number ("at least 3") before it
# uses what a revision added, so a number that stood still under a new route,
# or went down, answers that client wrongly. The same rule refuses a route or
# a feature that left the table without being declared as removed, a
# `stable` label that went down, and a route or a feature a `stable` one gives
# up even when it is declared: a stable feature only gains things within 1.x
# (docs/stability.md).
#
# NOT in `lint`, for the same reason `kinds:check` is not: it reads the DIFF.
# The plain test (TestFeaturesMatchTheGolden) holds the table to the golden in
# the tree, and `-update` holds a rewrite to the golden it replaces, but
# neither sees the base: two branches that each add a route to one feature and
# each raise it from 2 to 3 merge cleanly into a feature that gained two
# routes under one number. This compares the tree with the golden of what the
# change merges into, through the test's own rule (revisionProblems).
#
# FEATURES_CHECK_BASE overrides the base commit, for trying the check by hand:
#   FEATURES_CHECK_BASE=HEAD~1 mise run features:check
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

golden=internal/api/testdata/features.golden.json

base_commit="${FEATURES_CHECK_BASE:-}"
# A pull request's checkout is its merge commit (refs/pull/N/merge), whose
# first parent is what it merges into. For a pull request in a stack that
# parent is the merge of the stack below it, while GITHUB_BASE_REF names the
# trunk: the merge base with the trunk is from before every revision the
# stack below raised, and each of them would read as a new feature that did
# not start at 1. HEAD equal to GITHUB_SHA is the sign the checkout is that
# merge commit, as in .mise/changescheck.sh.
if [ -z "$base_commit" ] && [ "${GITHUB_EVENT_NAME:-}" = "pull_request" ] &&
  [ "$(git rev-parse HEAD)" = "${GITHUB_SHA:-}" ] &&
  git rev-parse --verify --quiet 'HEAD^2' >/dev/null; then
  base_commit="$(git rev-parse 'HEAD^1')"
fi
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
