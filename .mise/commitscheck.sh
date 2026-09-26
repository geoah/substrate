#!/usr/bin/env bash
#
# The pull request's titles, held to what the release reads: the PR title and
# every commit subject on the branch are conventional commits, and a break
# ships an upgrade note.
#
# Both halves are needed because main takes both merge methods. A squash
# lands the PR title as the one commit, a rebase lands every commit as it is,
# and `svu` and goreleaser read whichever arrives. A subject nothing parses
# is a release that does not happen, and a `!` with no note is a release
# nobody can upgrade to without reading the diff.
#
# A break is a `!` before the colon, in the title or in any subject, or a
# `BREAKING CHANGE:` footer in any commit body. It needs a note under
# docs/changes/ ADDED by this branch whose `type:` is `breaking`; the note's
# shape is lint:docs's (.mise/docscheck.sh), which holds every note in the
# tree, not only the new ones.
#
# PR_TITLE is the title, from the workflow's env and never interpolated into
# the script. Unset on a laptop, where only the commits are checked.
# COMMITS_CHECK_BASE overrides the base commit, as FROZEN_CHECK_BASE does:
#   COMMITS_CHECK_BASE=HEAD~3 mise run commits:check
#
# No `-e`: every rule runs and reports, so one pass names everything wrong.
set -uo pipefail

cd "$(git rev-parse --show-toplevel)" || exit 2

# The types in use, and the only ones: AGENTS.md lists the same seven. A scope
# is a comma-separated list of lowercase names (`cli,api`).
types='feat|fix|docs|refactor|test|chore|ci'
pattern="^(${types})(\\([a-z0-9._/,-]+\\))?!?: [^ ]"
breaking="^(${types})(\\([a-z0-9._/,-]+\\))?!: "

fail=0
flag() {
  fail=1
  printf 'commits:check: %s\n' "$*" >&2
}

base_commit="${COMMITS_CHECK_BASE:-}"
if [ -z "$base_commit" ]; then
  base_branch="${GITHUB_BASE_REF:-main}"
  if git rev-parse --verify --quiet "origin/${base_branch}" >/dev/null; then
    base_commit="$(git merge-base HEAD "origin/${base_branch}")"
  elif git rev-parse --verify --quiet "${base_branch}" >/dev/null; then
    base_commit="$(git merge-base HEAD "${base_branch}")"
  elif [ "${CI:-}" = "true" ]; then
    echo "commits:check: cannot resolve base branch ${base_branch}; refusing to pass without checking" >&2
    exit 1
  else
    echo "commits:check: no base branch to diff against; skipping" >&2
    exit 0
  fi
fi

is_break=0

if [ -n "${PR_TITLE:-}" ]; then
  [[ "$PR_TITLE" =~ $pattern ]] ||
    flag "the PR title '${PR_TITLE}' is not type(scope): subject, with a type of ${types//|/, }"
  [[ "$PR_TITLE" =~ $breaking ]] && is_break=1
fi

# Merge commits are skipped: main keeps a linear history, so neither merge
# method lands one, and a branch that merged main in to catch up is fine.
while IFS= read -r sha; do
  subject="$(git log -1 --format=%s "$sha")"
  [[ "$subject" =~ $pattern ]] ||
    flag "commit ${sha:0:12} '${subject}' is not type(scope): subject; a rebase merge would land it as it is"
  [[ "$subject" =~ $breaking ]] && is_break=1
  # A here-string, not a pipe: under pipefail `git log | grep -q` reports 141
  # when grep exits on an early match, and the break would read as absent.
  body="$(git log -1 --format=%b "$sha")"
  grep -qE '^BREAKING[ -]CHANGE: ' <<<"$body" && is_break=1
done < <(git rev-list --no-merges "${base_commit}..HEAD")

if [ "$is_break" -eq 1 ]; then
  noted=0
  while IFS= read -r path; do
    [ "$(basename "$path")" = "README.md" ] && continue
    grep -qE '^type: breaking$' "$path" && noted=1
  done < <(git diff --name-only --diff-filter=A "$base_commit" -- 'docs/changes/*.md')
  [ "$noted" -eq 1 ] ||
    flag "this branch breaks something (a '!' or a BREAKING CHANGE footer) and adds no docs/changes/*.md with 'type: breaking'; docs/changes/README.md says what the note holds"
fi

exit "$fail"
