#!/usr/bin/env bash
#
# The engine suite, or one shard of it.
#
# internal/engine is 800-odd top-level tests and the slowest package in the
# tree: 380 to 500 seconds on the CI runner, and past Go's 10 minute default
# under contention. CI runs it as SHARDS parallel jobs, each of which runs
# this script with its own SHARD. `go test -list` names every top-level test
# in the package and .mise/shardselect.sh hands back this shard's slice, so
# the same tree always cuts the same way and a failure reproduces by rerunning
# its shard number.
#
# The selection is a `-run` regex anchored at both ends, so a name that is a
# prefix of another cannot pull the other in. Subtests follow their parent:
# `-run` matches the first slash-separated element against the top-level name.
#
# Without SHARD and SHARDS this is the whole package, which is the run
# AGENTS.md recommends when working in the engine, so the flags match
# `test:db`: -count=1 (no cache), -timeout 30m (the whole package on a slow
# machine), -skip '^TestLive' (the live set buys completions). A shard runs
# under -timeout 12m instead: its job has 15 minutes, and a hang must die by
# Go's timeout, which prints every goroutine, not by the runner's, which
# prints nothing.
#
#   mise run test:db:engine                       # the whole package
#   SHARD=3 SHARDS=8 mise run test:db:engine      # one eighth of it
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

shards="${SHARDS:-1}"
shard="${SHARD:-1}"
flags=(-count=1 -skip '^TestLive')

# This script runs ./internal/engine/ while `test:db` runs ./internal/engine/...
# so a subpackage that grew a test suite would run on main's coverage job and
# never on a PR. Refuse it here, where the cut is, rather than let it happen.
subpackages="$(go list -f '{{if or .TestGoFiles .XTestGoFiles}}{{.ImportPath}}{{end}}' ./internal/engine/... | grep -v '/internal/engine$' || true)"
if [ -n "$subpackages" ]; then
  echo "test:db:engine: a package under internal/engine/ has tests of its own and the shards run only the root package:" >&2
  printf '  %s\n' "$subpackages" >&2
  echo "test:db:engine: add it to test:db:rest, or cut it here" >&2
  exit 1
fi

# -list prints one name per line and then the package's `ok` line; only the
# names are wanted. Test, Example and Fuzz: a fuzz target's seed corpus runs
# as an ordinary test under -run, so it has a shard like any other, and a
# Benchmark does not run without -bench. The list is built even for a single
# shard, so a bad SHARD is refused the same way whatever SHARDS says; the
# binary it compiles is the one the run below reuses.
names="$(go test -list '.*' ./internal/engine/ | grep -E '^(Test|Example|Fuzz)')"
mine="$(printf '%s\n' "$names" | SHARD="$shard" SHARDS="$shards" .mise/shardselect.sh)"
total="$(printf '%s\n' "$names" | grep -c .)"
count="$(printf '%s\n' "$mine" | grep -c .)"

if [ "$shards" -eq 1 ]; then
  exec go test "${flags[@]}" -timeout 30m ./internal/engine/
fi

run="^($(printf '%s\n' "$mine" | paste -sd '|'))\$"
echo "test:db:engine: shard ${shard}/${shards}, ${count} of ${total} tests"
exec go test "${flags[@]}" -timeout 12m -run "$run" ./internal/engine/
