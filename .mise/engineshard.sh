#!/usr/bin/env bash
#
# The engine suite, or one shard of it.
#
# internal/engine is 800-odd top-level tests and the slowest package in the
# tree: 380 to 500 seconds on the CI runner, and past Go's 10 minute default
# under contention. CI runs it as SHARDS parallel jobs, each of which runs
# this script with its own SHARD, and each shard gets a fixed slice of the
# tests: `go test -list` names every top-level test in the package, the names
# are sorted, and shard k takes every SHARDS-th name starting from the k-th.
# Every test lands in exactly one shard by construction, a new test lands in
# one without anybody editing a list, and the same tree always cuts the same
# way, so a failure reproduces by rerunning its shard number.
#
# Round robin over the sorted names rather than a hash of each: it puts the
# same count of tests in every shard, and on the recorded per-test timings it
# spreads the total more evenly than a hash does (the slowest tests are a few
# 30 second TOTP waits, and a hash was free to stack them).
#
# The selection is a `-run` regex anchored at both ends, so a name that is a
# prefix of another cannot pull the other in. Subtests follow their parent:
# `-run` matches the first slash-separated element against the top-level name.
#
# Without SHARD and SHARDS this is the whole package, which is the run
# AGENTS.md recommends when working in the engine, so the flags match
# `test:db`: -count=1 (no cache), -timeout 30m (the whole package on a slow
# machine), -skip '^TestLive' (the live set buys completions).
#
#   mise run test:db:engine                       # the whole package
#   SHARD=3 SHARDS=8 mise run test:db:engine      # one eighth of it
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

shards="${SHARDS:-1}"
shard="${SHARD:-1}"
case "$shards$shard" in
*[!0-9]* | "") echo "test:db:engine: SHARD and SHARDS must be positive integers (got SHARD=${shard} SHARDS=${shards})" >&2; exit 2 ;;
esac
if [ "$shards" -lt 1 ] || [ "$shard" -lt 1 ] || [ "$shard" -gt "$shards" ]; then
  echo "test:db:engine: SHARD must be in 1..SHARDS (got SHARD=${shard} SHARDS=${shards})" >&2
  exit 2
fi

flags=(-count=1 -timeout 30m -skip '^TestLive')

if [ "$shards" -eq 1 ]; then
  exec go test "${flags[@]}" ./internal/engine/
fi

# -list prints one name per line and then the package's `ok` line; only the
# names are wanted. LC_ALL=C so the sort is the same on every machine, which
# is what makes shard k the same set everywhere.
names="$(go test -list '.*' ./internal/engine/ | grep -E '^(Test|Example)' | LC_ALL=C sort)"
total="$(printf '%s\n' "$names" | grep -c .)"
mine="$(printf '%s\n' "$names" | awk -v n="$shards" -v k="$shard" 'NR % n == k % n')"
count="$(printf '%s\n' "$mine" | grep -c . || true)"
if [ "$count" -eq 0 ]; then
  echo "test:db:engine: shard ${shard}/${shards} selects no test out of ${total}; SHARDS is larger than the suite" >&2
  exit 1
fi

run="^($(printf '%s\n' "$mine" | paste -sd '|'))\$"
echo "test:db:engine: shard ${shard}/${shards}, ${count} of ${total} tests"
exec go test "${flags[@]}" -run "$run" ./internal/engine/
