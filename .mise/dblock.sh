#!/usr/bin/env bash
#
# Run a database test suite under a machine-wide lock.
#
#   .mise/dblock.sh go test ./internal/engine/
#
# One engine suite keeps a 16-core machine busy on its own: sixteen parallel
# tests, 800 template clones, a pgvector container per test binary. On
# 2026-09-26 twelve worktrees on one VM ran their suites at once, load reached
# 50 on 16 vCPUs with a fifth of CPU time waiting on disk, and the VM's other
# processes (the editor server the agents run in) blocked on the disk journal.
# So the database suites take a slot first, and a second run on the machine
# waits for the first instead of piling on.
#
# SUBSTRATE_TEST_DB_SLOTS is the number of suites allowed at once (default 1).
# The slots are flock(1) locks on files under SUBSTRATE_TEST_DB_LOCK_DIR
# (default /tmp/substrate-testdb), so a suite killed by any signal, SIGKILL
# included, releases its slot: the kernel drops the lock with the last open
# descriptor. The suite inherits the descriptor, so the slot stays held for as
# long as the suite runs even if this script is killed first.
#
# Not locked: CI (CI=true; every job has a machine of its own), a nested call
# (a locked task that runs another locked task, SUBSTRATE_TEST_DB_LOCK_HELD),
# and a machine without flock(1) (macOS without util-linux), which says so.
#
# After the suite this sweeps the machine's orphaned test containers
# (.mise/testclean.sh), so a killed run's leftovers go with the next run.
set -uo pipefail

[ "$#" -gt 0 ] || { echo "usage: $0 <command> [args...]" >&2; exit 2; }

here="$(cd "$(dirname "$0")" && pwd)"

# The suite runs at a lower CPU priority on a developer's machine, so the
# processes sharing it (an editor, the agents' own server) stay responsive
# while sixteen tests and a compile compete for the cores.
# SUBSTRATE_TEST_DB_NICE=0 turns it off; CI does not use it.
nice_cmd=()
if [ "${CI:-}" != "true" ] && [ "${SUBSTRATE_TEST_DB_NICE:-10}" != "0" ] && command -v nice >/dev/null 2>&1; then
  nice_cmd=(nice -n "${SUBSTRATE_TEST_DB_NICE:-10}")
fi

run() {
  "${nice_cmd[@]}" "$@" &
  child=$!
  trap 'kill -TERM "$child" 2>/dev/null' TERM INT HUP
  while :; do
    wait "$child"
    rc=$?
    # wait returns early (>128) when a trapped signal arrives; the child is
    # still being told to stop, so wait for it again.
    kill -0 "$child" 2>/dev/null || break
  done
  trap - TERM INT HUP
  "$here/testclean.sh"
  return "$rc"
}

if [ "${CI:-}" = "true" ] || [ -n "${SUBSTRATE_TEST_DB_LOCK_HELD:-}" ]; then
  run "$@"
  exit $?
fi
if ! command -v flock >/dev/null 2>&1; then
  echo "dblock: flock(1) is not installed, running without the machine-wide test lock" >&2
  run "$@"
  exit $?
fi

slots="${SUBSTRATE_TEST_DB_SLOTS:-1}"
case "$slots" in '' | *[!0-9]* | 0) echo "dblock: SUBSTRATE_TEST_DB_SLOTS must be a positive integer, got '${slots}'" >&2; exit 2 ;; esac
dir="${SUBSTRATE_TEST_DB_LOCK_DIR:-/tmp/substrate-testdb}"
mkdir -p "$dir" 2>/dev/null
chmod 1777 "$dir" 2>/dev/null

holders() {
  local f out=""
  for f in "$dir"/slot-*.owner; do
    [ -f "$f" ] && out="${out}${out:+; }$(cat "$f" 2>/dev/null)"
  done
  echo "${out:-unknown}"
}

start=$(date +%s)
said=0
got=""
while [ -z "$got" ]; do
  for i in $(seq 1 "$slots"); do
    exec 9>>"$dir/slot-$i"
    if flock -n 9; then
      got=$i
      break
    fi
    exec 9>&-
  done
  [ -n "$got" ] && break
  now=$(date +%s)
  if [ "$said" -eq 0 ] || [ $((now - said)) -ge 60 ]; then
    echo "dblock: waiting for a database test slot (${slots} slot(s), held by: $(holders)); waited $((now - start))s" >&2
    said=$now
  fi
  sleep 2
done

[ "$said" -gt 0 ] && echo "dblock: got slot ${got} after $(($(date +%s) - start))s" >&2
echo "pid $$ in $(pwd) since $(date -u +%H:%M:%SZ): $*" >"$dir/slot-$got.owner" 2>/dev/null
export SUBSTRATE_TEST_DB_LOCK_HELD=1
run "$@"
rc=$?
rm -f "$dir/slot-$got.owner"
exit "$rc"
