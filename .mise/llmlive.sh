#!/usr/bin/env bash
#
# CI job `llm live` (`mise run ci:llm`): the live suite with both keys
# required, and the spend it booked written to the run summary.
#
# `test:llm` skips every case whose key is absent and exits 0, which is right
# on a laptop and wrong here: a scheduled run whose environment lost a secret
# would report green having tested nothing. So a missing key fails the job.
#
# The ledgers (internal/llm/livespend) print one table per test binary between
# `<!-- live-spend -->` markers; every such block in the output is appended to
# $GITHUB_STEP_SUMMARY. The suite's exit status is kept and returned, because
# the summary is most wanted when the suite failed. The output goes to a
# temporary file that is removed on exit and never uploaded.
set -uo pipefail

summary="${GITHUB_STEP_SUMMARY:-/dev/null}"

missing=""
for key in OPENAI_API_KEY ANTHROPIC_API_KEY; do
  if [ -z "${!key:-}" ]; then
    missing="${missing} ${key}"
  fi
done
if [ -n "$missing" ]; then
  printf 'ci:llm: not set:%s. The job reads both keys from the llm-live environment (docs/testing.md).\n' "$missing" >&2
  printf '### Live LLM suite not run\n\nNot set:%s. The job reads both keys from the llm-live environment.\n' "$missing" >>"$summary"
  exit 1
fi

log="$(mktemp)"
trap 'rm -f "$log"' EXIT

mise run test:llm 2>&1 | tee "$log"
status=${PIPESTATUS[0]}

# A line may carry a `[task] ` prefix where mise prefixes task output; it is
# dropped before the markers are matched.
blocks="$(awk '{ sub(/^\[[^]]*\] /, "") } /^<!-- live-spend -->$/, /^<!-- \/live-spend -->$/' "$log")"
if [ -n "$blocks" ]; then
  printf '%s\n' "$blocks" >>"$summary"
else
  printf '### Live LLM spend\n\nNo live request was booked.\n' >>"$summary"
fi

exit "$status"
