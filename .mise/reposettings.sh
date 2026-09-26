#!/usr/bin/env bash
#
# The repository's merge settings, from the tree: .github/rulesets/main.json
# is the ruleset on main, and the merge buttons are set below. Run by a
# person with admin on the repository (`gh auth login`); CI never runs it,
# because a workflow token that can rewrite the ruleset can lift it.
#
# What it holds: main moves only through a pull request, merged by squash or
# rebase, with a linear history, after the named checks passed. Squash lands
# the PR title as the commit; rebase lands each commit, which is why
# `commits:check` (the `conventional commits` check) holds both.
#
# A required check must exist on the head of every open pull request, so
# apply this after the workflow that runs it has merged, and rebase the open
# pull requests onto main.
#
#   mise run repo:settings           apply
#   mise run repo:settings --dry-run print what would be sent
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

repo="${GITHUB_REPOSITORY:-$(gh repo view --json nameWithOwner --jq .nameWithOwner)}"
ruleset=.github/rulesets/main.json
name="$(jq -r .name "$ruleset")"

# The merge buttons. squash_merge_commit_title is what makes the squash
# commit carry the PR title; merge commits stay off because the ruleset
# requires a linear history anyway.
settings='{
  "allow_squash_merge": true,
  "allow_rebase_merge": true,
  "allow_merge_commit": false,
  "squash_merge_commit_title": "PR_TITLE",
  "squash_merge_commit_message": "COMMIT_MESSAGES",
  "delete_branch_on_merge": true
}'

id="$(gh api "repos/${repo}/rulesets" --jq ".[] | select(.name == \"${name}\") | .id")"

if [ "${1:-}" = "--dry-run" ]; then
  echo "PATCH repos/${repo}"
  jq . <<<"$settings"
  if [ -n "$id" ]; then echo "PUT repos/${repo}/rulesets/${id}"; else echo "POST repos/${repo}/rulesets"; fi
  jq . "$ruleset"
  exit 0
fi

gh api --method PATCH "repos/${repo}" --input - <<<"$settings" >/dev/null
echo "repo:settings: merge buttons set on ${repo}" >&2

if [ -n "$id" ]; then
  gh api --method PUT "repos/${repo}/rulesets/${id}" --input "$ruleset" >/dev/null
  echo "repo:settings: ruleset '${name}' (${id}) replaced from ${ruleset}" >&2
else
  gh api --method POST "repos/${repo}/rulesets" --input "$ruleset" >/dev/null
  echo "repo:settings: ruleset '${name}' created from ${ruleset}" >&2
fi
