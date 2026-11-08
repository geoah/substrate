#!/usr/bin/env bash
#
# The verdict of the `apps gate` job, the one check the ruleset requires for
# the apps' jobs (today `apps linux` alone). Theirs cannot be required: each
# runs only when the `changes` job answers apps=true, and GitHub passes a
# required check whose job was skipped, so a `changes` that failed to answer
# (the jobs then skipped) would merge an app change that nothing built. So
# the gate reads the answer itself:
#
#   apps=true   every job must have succeeded;
#   apps=false  every job must have been skipped, or have succeeded;
#   no answer   red, since nothing says whether the jobs had to run.
#
# A failure and a cancellation are red whatever the answer, and so is a
# skip apps=true did not allow.
#
# Usage: APPS=<needs.changes.outputs.apps> .mise/appsgate.sh '<job>=<result>'…
#   one argument for each job the answer gates, with its needs.<job>.result.
set -euo pipefail

if [ "$#" -eq 0 ]; then
  echo "apps gate: no job to hold; name every job the apps answer gates" >&2
  exit 2
fi

answer="${APPS:-}"
case "$answer" in
true) allowed="success" ;;
false) allowed="success skipped" ;;
*)
  echo "apps gate: the changes job gave no apps answer ('${answer}'), so nothing says whether the apps jobs had to run" >&2
  exit 1
  ;;
esac

status=0
for pair in "$@"; do
  job="${pair%=*}"
  result="${pair##*=}"
  case " ${allowed} " in
  *" ${result} "*) echo "apps gate: ${job} ${result} (apps=${answer})" ;;
  *)
    echo "apps gate: ${job} ended ${result:-with no result}, and apps=${answer} allows ${allowed// / or }" >&2
    status=1
    ;;
  esac
done
exit "$status"
