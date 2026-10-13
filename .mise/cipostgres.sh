#!/usr/bin/env bash
#
# The Postgres a CI database job runs its suite against, started by a step.
#
# Why it exists: a job's `services:` container is pulled and started by the
# runner before the job's first step, and the checkout waits for it to report
# healthy. That `Initialize containers` step took a median 15 s (12 to 37 s
# over 944 jobs, 2026-10-10 to 2026-10-12) on every database job's critical
# path, and a pull request the path gate found inert paid it too, then
# skipped every step. `start` returns at once and leaves `docker run` pulling
# while mise and the caches install; `wait` runs right before the suite.
#
# The container is what the jobs' SUBSTRATE_TEST_DATABASE_URL names:
# postgres:postgres on localhost:5432, database substrate, its data
# directory on a tmpfs. It keeps the image's stock settings,
# max_connections 100 included; testdb turns the durability off with ALTER
# SYSTEM because the jobs set SUBSTRATE_TEST_DATABASE_DISPOSABLE.
#
# `wait` probes over TCP (-h 127.0.0.1). The image's entrypoint first runs a
# temporary server for its init scripts that listens on the unix socket only,
# so a socket probe answers ready, the temporary server stops, and the first
# connection meets a server that is not there (#869). The real server is the
# first one to listen on TCP.
#
# Refused outside CI: it publishes a superuser with a known password on every
# interface, which a runner discards and a laptop keeps.
#
#   .mise/cipostgres.sh start
#   .mise/cipostgres.sh wait
#
# CI_POSTGRES_TIMEOUT bounds `wait` in seconds (default 120: the pull, the
# init scripts and the restart); CI_POSTGRES_DIR holds the `docker run`
# output and its exit status between the two steps (default under
# RUNNER_TEMP).
set -euo pipefail

readonly name="substrate-ci-postgres"
readonly image="pgvector/pgvector:pg16"
dir="${CI_POSTGRES_DIR:-${RUNNER_TEMP:-${TMPDIR:-/tmp}}/substrate-ci-postgres}"
timeout="${CI_POSTGRES_TIMEOUT:-120}"

usage() {
  echo "usage: $0 start|wait" >&2
  exit 2
}

[ "$#" -eq 1 ] || usage
if [ "${CI:-}" != "true" ]; then
  echo "cipostgres: CI is not true; on a laptop testdb starts a container of its own" >&2
  exit 2
fi
case "$timeout" in '' | *[!0-9]* | 0)
  echo "cipostgres: CI_POSTGRES_TIMEOUT must be a positive integer, got '${timeout}'" >&2
  exit 2
  ;;
esac

start() {
  mkdir -p "$dir"
  rm -f "$dir/status"
  # Every descriptor is redirected: a background child holding the step's
  # stdout would keep the step open until the pull finished. The status file
  # is written by rename, so `wait` never reads a half-written one. The inner
  # script's `$1` to `$3` are its own arguments, hence the single quotes.
  # shellcheck disable=SC2016
  nohup bash -c '
    status=0
    docker run -d --name "$1" -p 5432:5432 \
      -e POSTGRES_PASSWORD=postgres -e POSTGRES_DB=substrate \
      --tmpfs /var/lib/postgresql/data:rw,size=3g "$2" || status=$?
    echo "$status" >"$3/status.tmp" && mv "$3/status.tmp" "$3/status"
  ' cipostgres "$name" "$image" "$dir" >"$dir/run.log" 2>&1 </dev/null &
  echo "cipostgres: pulling and starting ${image} as ${name} in the background"
}

# show prints what a failed start leaves behind: the pull and the server.
show() {
  echo "--- docker run" >&2
  cat "$dir/run.log" >&2 2>/dev/null || true
  echo "--- docker logs ${name}" >&2
  docker logs "$name" >&2 2>&1 || true
}

wait_ready() {
  if [ ! -f "$dir/run.log" ]; then
    echo "cipostgres: no start in ${dir}; run \`.mise/cipostgres.sh start\` first" >&2
    exit 1
  fi
  local began=$SECONDS
  while [ $((SECONDS - began)) -lt "$timeout" ]; do
    if [ -f "$dir/status" ] && [ "$(cat "$dir/status")" != "0" ]; then
      echo "cipostgres: docker run exited $(cat "$dir/status")" >&2
      show
      exit 1
    fi
    if docker exec "$name" pg_isready -q -h 127.0.0.1 -U postgres -d substrate >/dev/null 2>&1; then
      echo "cipostgres: ${name} accepts TCP connections after $((SECONDS - began))s of waiting"
      return 0
    fi
    sleep 1
  done
  echo "cipostgres: ${name} did not accept a TCP connection within ${timeout}s" >&2
  show
  exit 1
}

case "$1" in
start) start ;;
wait) wait_ready ;;
*) usage ;;
esac
