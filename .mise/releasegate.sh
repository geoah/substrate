#!/usr/bin/env bash
#
# Whether this release-please run may create a tag and a GitHub release.
# Writes `hold=true` or `hold=false` to $GITHUB_OUTPUT;
# .github/workflows/release-please.yml hands it to the action as
# `skip-github-release`.
#
# The tag and the release are public the moment release-please creates them,
# before release.yml refuses a commit ci never passed. So a run may tag only
# when every merged release pull request still waiting for its tag (label
# `autorelease: pending`) has a green push run of ci on main for its merge
# commit. That run going green is a ci completion of its own, and the run it
# starts here cuts the release.
#
# Two cases hold whatever ci says, because this list is read before
# release-please reads its own, and a release pull request merged between the
# two reads would be released with no ci run behind it:
#
#   - A push run. The merge that started it may be that release pull request,
#     whose ci run has only just begun. A push run refreshes the pull request
#     and nothing else; the ci completion that follows decides.
#   - No release pull request waiting. There is nothing to release, so holding
#     costs nothing, and one merged just after this read is not released
#     unchecked.
#
# Usage: EVENT_NAME=<github.event_name> .mise/releasegate.sh
#   GITHUB_REPOSITORY, GITHUB_OUTPUT and GH_TOKEN as Actions sets them.
set -euo pipefail

event="${EVENT_NAME:?EVENT_NAME must be github.event_name of the run}"
repo="${GITHUB_REPOSITORY:?GITHUB_REPOSITORY must name the repository}"
output="${GITHUB_OUTPUT:?GITHUB_OUTPUT must name the step output file}"

case "$event" in
push)
  echo "a push run refreshes the release pull request and never tags" >&2
  echo "hold=true" >>"$output"
  exit 0
  ;;
workflow_run | workflow_dispatch) ;;
*)
  echo "release-please does not run on '$event'; refusing to decide" >&2
  exit 2
  ;;
esac

shas="$(gh pr list --repo "$repo" --state merged --label 'autorelease: pending' \
  --json mergeCommit --jq '.[].mergeCommit.oid')"
if [ -z "$shas" ]; then
  echo "no merged release pull request is waiting for its tag; holding" >&2
  echo "hold=true" >>"$output"
  exit 0
fi

hold=false
while read -r sha; do
  # `event == push` and `head_branch == main`: a pull request run can carry a
  # main commit's sha (a fork's pull request from its own main), and that run
  # tested the fork.
  green="$(gh api \
    "repos/$repo/actions/workflows/ci.yml/runs?head_sha=$sha&status=success&per_page=100" \
    --jq '[.workflow_runs[] | select(.event == "push" and .head_branch == "main")] | length')"
  if [ "$green" -eq 0 ]; then
    echo "release commit $sha has no successful ci run on main yet; holding the release" >&2
    hold=true
  else
    echo "release commit $sha passed ci" >&2
  fi
done <<<"$shas"
echo "hold=$hold" >>"$output"
