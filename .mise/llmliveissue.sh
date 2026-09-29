#!/usr/bin/env bash
#
# The `llm live` job's tracking issue: ONE issue, found by its fixed title,
# that every failing run comments on.
#
#   .mise/llmliveissue.sh failure   comment on the issue, reopening it if it
#                                   is closed, or open it if there is none
#   .mise/llmliveissue.sh success   close the issue if it is open
#
# The issue is found by exact title among every issue, open and closed, never
# through search: the search index lags, and a lagging index is how a second
# issue gets opened. The oldest match is the one written to, so a duplicate
# opened by hand never takes over.
#
# Reads the runner's GITHUB_SERVER_URL, GITHUB_REPOSITORY and GITHUB_RUN_ID,
# and GH_TOKEN through gh. The workflow calls it after `mise run ci:llm`; it
# has no laptop meaning. .mise/cicheck.sh runs its scenarios against a fake gh.
set -euo pipefail

verdict="${1:-}"
case "$verdict" in
failure | success) ;;
*)
  echo "usage: .mise/llmliveissue.sh failure|success" >&2
  exit 2
  ;;
esac
repo="${GITHUB_REPOSITORY:?GITHUB_REPOSITORY names the repository}"
run_url="${GITHUB_SERVER_URL:?}/${repo}/actions/runs/${GITHUB_RUN_ID:?}"

export LLM_LIVE_TITLE="Weekly \`llm live\` job fails: \`mise run test:llm\` is red on main"

# "<number> <OPEN|CLOSED>" of the oldest issue with exactly the title, or
# nothing.
found="$(gh issue list --repo "$repo" --state all --limit 10000 --json number,state,title \
  --jq '[.[] | select(.title == env.LLM_LIVE_TITLE)] | sort_by(.number) | .[0] // empty | "\(.number) \(.state)"')"
number="${found%% *}"
state="${found#* }"

case "$verdict" in
failure)
  if [ -z "$found" ]; then
    body="$(
      cat <<EOF
The weekly \`llm live\` job (.github/workflows/llm-live.yml) failed on main: ${run_url}

The job runs \`mise run test:llm\` against the real OpenAI and Anthropic APIs. The failing case in the run's log names the cause: a provider refusing a request an adapter sends, a changed response or stream shape, a missing or revoked key, or the suite's request or token ceiling. The run summary lists what each provider was sent.

Every failing run comments here, and the next passing run closes this issue. docs/testing.md describes the job.
EOF
    )"
    gh issue create --repo "$repo" --title "$LLM_LIVE_TITLE" --label area/ci --label area/agent --body "$body"
  else
    if [ "$state" = CLOSED ]; then
      gh issue reopen --repo "$repo" "$number"
    fi
    gh issue comment --repo "$repo" "$number" --body "The \`llm live\` job failed again: ${run_url}"
  fi
  ;;
success)
  if [ -n "$found" ] && [ "$state" = OPEN ]; then
    gh issue close --repo "$repo" "$number" --comment "The \`llm live\` job passed: ${run_url}"
  fi
  ;;
esac
