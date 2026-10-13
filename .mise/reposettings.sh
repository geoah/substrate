#!/usr/bin/env bash
#
# The repository's merge and security settings, from the tree:
# .github/rulesets/main.json is the ruleset on main, and the rest is set
# below. Run by a person with admin on the repository (`gh auth login`); CI
# never runs it, because a workflow token that can rewrite the ruleset can
# lift it.
#
# What it holds: main moves only through a pull request, merged by squash or
# rebase, with a linear history, after the named checks passed. Squash lands
# the PR title as the commit; rebase lands each commit, which is why
# `commits:check` (the `conventional commits` check) holds both. Dependabot
# reports and patches vulnerable dependencies, the workflow token reads
# unless a workflow asks for more, every action is pinned to a commit, and
# the `release` and `llm-live` environments deploy from main alone.
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

# Read-only unless a workflow's `permissions:` asks for more, which every
# workflow here does; a workflow without the block gets a token that can
# write nothing. No workflow approves a pull request, so none may.
workflow='{
  "default_workflow_permissions": "read",
  "can_approve_pull_request_reviews": false
}'

# Every `uses:` names its action by a full commit SHA, as every workflow
# here already does; a tag (mutable) fails the run. Local actions and
# reusable workflows, `./.github/workflows/release.yml` among them, are
# exempt.
actions='{
  "enabled": true,
  "allowed_actions": "all",
  "sha_pinning_required": true
}'

# The environments whose secrets only main's workflows may read: `release`
# holds RELEASE_PLEASE_TOKEN (docs/releasing.md has the move from a
# repository secret), and `llm-live` the key the weekly live suite spends.
environments=(release llm-live)
environment='{
  "deployment_branch_policy": {
    "protected_branches": false,
    "custom_branch_policies": true
  }
}'
branch='{"name": "main", "type": "branch"}'

id="$(gh api "repos/${repo}/rulesets" --jq ".[] | select(.name == \"${name}\") | .id")"

if [ "${1:-}" = "--dry-run" ]; then
  echo "PATCH repos/${repo}"
  jq . <<<"$settings"
  if [ -n "$id" ]; then echo "PUT repos/${repo}/rulesets/${id}"; else echo "POST repos/${repo}/rulesets"; fi
  jq . "$ruleset"
  echo "PUT repos/${repo}/vulnerability-alerts"
  echo "PUT repos/${repo}/automated-security-fixes"
  echo "PUT repos/${repo}/actions/permissions/workflow"
  jq . <<<"$workflow"
  echo "PUT repos/${repo}/actions/permissions"
  jq . <<<"$actions"
  for env in "${environments[@]}"; do
    echo "PUT repos/${repo}/environments/${env}"
    jq . <<<"$environment"
    echo "DELETE repos/${repo}/environments/${env}/deployment-branch-policies/<id>, for every policy but main's"
    echo "POST repos/${repo}/environments/${env}/deployment-branch-policies, unless the environment lists main"
    jq . <<<"$branch"
  done
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

# Alerts first: GitHub refuses security updates on a repository without
# them. The updates are what .github/dependabot.yml's `applies-to:
# security-updates` groups wait for.
gh api --method PUT "repos/${repo}/vulnerability-alerts" >/dev/null
gh api --method PUT "repos/${repo}/automated-security-fixes" >/dev/null
echo "repo:settings: Dependabot alerts and security updates on for ${repo}" >&2

gh api --method PUT "repos/${repo}/actions/permissions/workflow" --input - <<<"$workflow" >/dev/null
gh api --method PUT "repos/${repo}/actions/permissions" --input - <<<"$actions" >/dev/null
echo "repo:settings: read-only workflow token and SHA-pinned actions on ${repo}" >&2

# The PUT keeps the environment's secrets. Its policies are reconciled to
# main's alone: one added by hand (another branch, a pattern, a tag) would
# let that ref read the secrets, and a second POST of main's is refused.
main_only() {
  local env="$1" policies have="" pid ptype pname
  gh api --method PUT "repos/${repo}/environments/${env}" --input - <<<"$environment" >/dev/null
  policies="$(gh api --paginate "repos/${repo}/environments/${env}/deployment-branch-policies" \
    --jq '.branch_policies[] | "\(.id) \(.type) \(.name)"')"
  while read -r pid ptype pname; do
    [ -n "$pid" ] || continue
    if [ "$ptype" = branch ] && [ "$pname" = main ]; then
      have=1
      continue
    fi
    gh api --method DELETE "repos/${repo}/environments/${env}/deployment-branch-policies/${pid}" >/dev/null
    echo "repo:settings: removed the ${env} environment's ${ptype} policy '${pname}'" >&2
  done <<<"$policies"
  if [ -z "$have" ]; then
    gh api --method POST "repos/${repo}/environments/${env}/deployment-branch-policies" --input - <<<"$branch" >/dev/null
  fi
  echo "repo:settings: environment '${env}' deploys from main alone" >&2
}
for env in "${environments[@]}"; do
  main_only "$env"
done
