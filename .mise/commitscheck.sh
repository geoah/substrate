#!/usr/bin/env bash
#
# The pull request's titles, held to what the release reads: the PR title and
# every commit subject on the branch are conventional commits, and a break
# carries its upgrade steps.
#
# Both halves are needed because main takes both merge methods. A squash
# lands the PR title as the one commit, a rebase lands every commit as it is,
# and release-please reads whichever arrives to write the next CHANGELOG.md
# section and pick the next version. A subject nothing parses is a change
# the changelog never lists, and a break with no steps is a release nobody
# can upgrade to without reading the diff.
#
# A break is a `!` before the colon, in the title or in any subject, or a
# `BREAKING CHANGE:` footer at the start of a line in any commit body
# (release-please's rule, and the conventional commits one). It needs that
# footer WITH TEXT in some commit body on the branch: release-please prints
# the text as the entry under "BREAKING CHANGES", so the text is the upgrade
# steps, written for the person or agent who follows them literally.
#
# A release pull request (`chore(release): vX.Y.Z`, opened by release-please)
# may touch CHANGELOG.md and the release-please manifest and nothing else: it
# is the one pull request whose diff nobody reviews line by line.
#
# PR_TITLE is the title, from the workflow's env and never interpolated into
# the script. Unset on a laptop, where only the commits are checked.
# COMMITS_CHECK_BASE overrides the base commit, as FROZEN_CHECK_BASE does:
#   COMMITS_CHECK_BASE=HEAD~3 mise run commits:check
#
# No `-e`: every rule runs and reports, so one pass names everything wrong.
set -uo pipefail

cd "$(git rev-parse --show-toplevel)" || exit 2

# The prefixes in use, and the only ones: AGENTS.md lists the same seven. A
# scope is a comma-separated list of lowercase names (`cli,api`).
types='feat|fix|docs|refactor|test|chore|ci'
pattern="^(${types})(\\([a-z0-9._/,-]+\\))?!?: [^ ]"
breaking="^(${types})(\\([a-z0-9._/,-]+\\))?!: "
release_title='^chore\(release\): v[0-9]+\.[0-9]+\.[0-9]+$'

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
has_steps=0
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
  # The footer is a LINE that starts with the token, as the conventional
  # commits parser release-please uses reads it. The phrase quoted mid-line
  # in prose is prose. A here-string, not a pipe: under pipefail
  # `git log | grep -q` reports 141 when grep exits on an early match.
  body="$(git log -1 --format=%b "$sha")"
  grep -qE '^BREAKING[ -]CHANGE:' <<<"$body" && is_break=1
  grep -qE '^BREAKING[ -]CHANGE: *[^ ]' <<<"$body" && has_steps=1
done < <(git rev-list --no-merges "${base_commit}..HEAD")

if [ "$is_break" -eq 1 ] && [ "$has_steps" -eq 0 ]; then
  flag "this branch breaks something (a '!', or a 'BREAKING CHANGE:' footer) and no commit body carries 'BREAKING CHANGE: <the upgrade steps>'; add the footer with the steps a client or a deployment follows, or drop the '!'"
fi

if [ -n "${PR_TITLE:-}" ] && [[ "$PR_TITLE" =~ $release_title ]]; then
  extra="$(git diff --name-only "${base_commit}" HEAD | grep -vxF -e CHANGELOG.md -e .release-please-manifest.json || true)"
  [ -z "$extra" ] ||
    flag "a release pull request may change CHANGELOG.md and .release-please-manifest.json only; this one also changes: $(tr '\n' ' ' <<<"$extra")"
fi

exit "$fail"
