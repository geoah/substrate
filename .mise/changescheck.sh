#!/usr/bin/env bash
#
# The path gate for the database suite: does this change touch anything a Go
# test result can depend on?
#
# Why it exists: the database suite is the slowest thing CI runs (the engine
# package alone is 380 to 500 seconds on the runner, before it was sharded),
# and a PR that edits only docs/, the console or a linter config paid for it
# every time. The workflow asks this script once, in its first job, and the
# `go test` job and the engine shards run only when the answer is `go=true`.
#
# The list below is the INERT set, not the relevant one. A file is relevant
# unless it matches a pattern known to be read by nothing a Go test runs,
# because the two mistakes are not symmetric: a missed inert pattern costs one
# needless test run, a missed relevant pattern is a red test that merged
# green. kinds/ and samples/ are checked first because they are embedded into
# the binary whole, so a README under them IS an input.
#
# A push (to main, the only branch the workflow watches) has no base to diff
# against and is where the coverage profile comes from, so every job runs.
#
# Output: `go=true` or `go=false` on stdout, and the same line appended to
# $GITHUB_OUTPUT when the runner provides one, which is how the workflow reads
# it. CHANGES_CHECK_BASE overrides the base commit, for trying it by hand:
#   CHANGES_CHECK_BASE=HEAD~1 mise run ci:changes
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

emit() {
  echo "ci:changes: go=$1 ($2)"
  if [ -n "${GITHUB_OUTPUT:-}" ]; then
    echo "go=$1" >>"$GITHUB_OUTPUT"
  fi
}

if [ "${GITHUB_EVENT_NAME:-}" = "push" ]; then
  emit true "a push runs every job"
  exit 0
fi

base_commit="${CHANGES_CHECK_BASE:-}"
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
    # In CI this decides whether the suite runs at all. A base it cannot
    # resolve means the fetch above failed, and answering `false` would skip
    # the suite on a change nobody looked at.
    echo "ci:changes: cannot resolve base branch ${base_branch}; refusing to skip the suite" >&2
    exit 1
  else
    emit true "no base branch to diff against"
    exit 0
  fi
fi

# Inert: read by the lint, console and image jobs, which run regardless, and
# by no Go test. Everything else is relevant, including .mise/ (the scripts
# the test tasks call) and .mise.toml (the tasks themselves).
inert() {
  case "$1" in
  kinds/* | samples/* | .github/workflows/ci.yml) return 1 ;;
  docs/* | web/console/* | .github/* | *.md) return 0 ;;
  LICENSE | .gitignore | .gitattributes | .editorconfig | .dockerignore) return 0 ;;
  .yamlfmt | .yamllint | .lychee.toml | .ruff.toml | .golangci.yml | .goreleaser.yaml) return 0 ;;
  Dockerfile | Dockerfile.release | compose.yaml) return 0 ;;
  *) return 1 ;;
  esac
}

# The diff against the working tree, plus the untracked files a laptop has
# and a CI checkout never does, so a hand run sees the file just created.
while IFS= read -r path; do
  if ! inert "$path"; then
    emit true "${path} can change a Go result"
    exit 0
  fi
done < <(
  git diff --name-only "$base_commit"
  git ls-files --others --exclude-standard
)

emit false "no changed file since ${base_commit} is read by a Go test"
