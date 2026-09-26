#!/usr/bin/env bash
#
# Remove this machine's test containers whose owner process has exited.
#
# Every Postgres container a test starts carries the labels internal/testdb
# sets (substrate.test=1, the host, the pid of the test binary). Ryuk
# removes a session's containers when its process goes away, but a killed
# run can still leave some: ryuk exits while container creates are in flight
# in dockerd, and those land in `Created` with nothing to reap them. The Go
# side sweeps the same way at the start of every run (testdb.SweepOrphans);
# this is the same sweep from the shell, for dblock.sh to run after a suite
# and for `mise run test:clean`. A container whose owner is alive is another
# run's and is left alone.
set -uo pipefail

command -v docker >/dev/null 2>&1 || exit 0
host="$(hostname)"
ids="$(docker ps -aq --filter label=substrate.test=1 --filter "label=substrate.test.host=${host}" 2>/dev/null)" || exit 0
[ -z "$ids" ] && exit 0
n=0
for id in $ids; do
  pid="$(docker inspect -f '{{index .Config.Labels "substrate.test.pid"}}' "$id" 2>/dev/null)"
  case "$pid" in '' | *[!0-9]*) continue ;; esac
  kill -0 "$pid" 2>/dev/null && continue
  # EPERM: alive, and somebody else's.
  [ -d "/proc/$pid" ] && continue
  if docker rm -fv "$id" >/dev/null 2>&1; then n=$((n + 1)); fi
done
[ "$n" -gt 0 ] && echo "testclean: removed ${n} orphaned test container(s)" >&2
exit 0
