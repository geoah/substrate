#!/usr/bin/env bash
#
# One database package's suite, or one shard of it.
#
# internal/engine is 800-odd top-level tests and the slowest package in the
# tree, and internal/catalog is the next. CI runs each as SHARDS parallel
# jobs, each of which runs this script with its own SHARD. The package's test
# files name every top-level test and .mise/shardselect.sh hands back this shard's
# slice, so the same tree always cuts the same way and a failure reproduces
# by rerunning its shard number.
#
# The selection is a `-run` regex anchored at both ends, so a name that is a
# prefix of another cannot pull the other in. Subtests follow their parent:
# `-run` matches the first slash-separated element against the top-level name.
#
# `go test` runs the binary both times, never a copy built with `-c`: the
# sandbox re-executes the test binary, and one run from outside go's build
# directory fails every body with `uv sync: exit status 126`.
#
# PKG is the package directory (default internal/engine). Without SHARD and
# SHARDS this is the whole package, which is the run AGENTS.md recommends when
# working in the engine, so the flags match `test:db`: -count=1 (no cache),
# -timeout 30m, -skip '^TestLive' (the live set buys completions). A shard
# runs under -timeout 12m instead: its job has 15 minutes, and a hang must die
# by Go's timeout, which prints every goroutine, not by the runner's, which
# prints nothing. SKIP adds names to the skip pattern.
#
#   mise run test:db:engine                          # the whole engine package
#   SHARD=3 SHARDS=12 mise run test:db:engine        # one twelfth of it
#   PKG=internal/catalog SHARD=1 SHARDS=2 .mise/dbshard.sh
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

pkg="${PKG:-internal/engine}"
pkg="${pkg#./}"
pkg="${pkg%/}"
shards="${SHARDS:-1}"
shard="${SHARD:-1}"
skip='^TestLive'
[ -n "${SKIP:-}" ] && skip="${skip}|${SKIP}"
label="test:db:$(basename "$pkg")"

# This script runs ./$pkg/ while `test:db` runs ./$pkg/..., so a subpackage
# that grew a test suite would run on main's coverage job and never on a PR.
# Refuse it here, where the cut is, rather than let it happen.
subpackages="$(go list -f '{{if or .TestGoFiles .XTestGoFiles}}{{.ImportPath}}{{end}}' "./${pkg}/..." | grep -v "/${pkg}\$" || true)"
if [ -n "$subpackages" ]; then
  echo "${label}: a package under ${pkg}/ has tests of its own and the shards run only the root package:" >&2
  printf '  %s\n' "$subpackages" >&2
  echo "${label}: add it to test:db:rest, or cut it here" >&2
  exit 1
fi

if [ "$shards" -eq 1 ]; then
  exec go test -count=1 -skip "$skip" -timeout 30m "./${pkg}/"
fi

# The names come from the source, not from `go test -list`: listing compiles
# and links the test binary, which was 11 of a shard's 50 seconds, and the run
# below compiles it anyway. TestMain is the one `func Test` that is not a test.
# A helper whose signature `go test` would not run lands in a shard's regex and
# matches nothing, so the two lists differ only harmlessly.
names="$(grep -hoE '^func (Test|Example|Fuzz)[A-Za-z0-9_]*\(' "$pkg"/*_test.go |
  sed -e 's/^func //' -e 's/($//' | grep -vx 'TestMain' | LC_ALL=C sort -u)"
mine="$(printf '%s\n' "$names" | SHARD="$shard" SHARDS="$shards" .mise/shardselect.sh)"
total="$(printf '%s\n' "$names" | grep -c .)"
count="$(printf '%s\n' "$mine" | grep -c .)"
run="^($(printf '%s\n' "$mine" | paste -sd '|'))\$"
echo "${label}: shard ${shard}/${shards}, ${count} of ${total} tests"
exec go test -count=1 -skip "$skip" -timeout 12m -run "$run" "./${pkg}/"
